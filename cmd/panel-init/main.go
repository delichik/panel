package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"panel/internal/modules/backups"
	"panel/internal/platform/panelsupervision"
	"panel/internal/platform/tailscale"
)

type restartRequest struct {
	Mode string `json:"mode"`
}

// panelLaunch 是一次 Panel 子进程启动所需的全部参数。
type panelLaunch struct {
	path   string
	socket string
	token  string
	mode   string
}

func main() {
	panelPath := flag.String("panel", defaultPanelPath(), "panel binary path")
	dataRoot := flag.String("data-root", defaultDataRoot(), "panel data root used for tailscale state")
	tailscalePath := flag.String("tailscale", defaultTailscalePath(), "tailscale CLI path (empty disables container tailscale)")
	tailscaledPath := flag.String("tailscaled", defaultTailscaledPath(), "tailscaled daemon path (empty disables container tailscale)")
	panelUser := flag.String("panel-user", defaultPanelUser(), "user the panel child process runs as")
	flag.Parse()

	restarts := make(chan string, 1)
	token, err := randomControlToken()
	if err != nil {
		log.Fatalf("generate control token failed: %v", err)
	}

	// 运行身份不允许降级：非 root 既不能把 Panel 子进程降权，也不能以内核 TUN
	// 模式启动容器内 tailscaled。这里直接拒绝启动，避免容器带着一个悄悄失效的
	// 能力运行。
	if err := ensurePrivileges(*panelUser, resolvePanelUser(*panelUser)); err != nil {
		log.Fatalf("%v", err)
	}

	supervisor := newTailscaleSupervisor(*dataRoot, *tailscalePath, *tailscaledPath, log.Default())
	supervisor.Start(context.Background())

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)

	socketPath := panelsupervision.ControlSocketPath(*dataRoot)
	server, err := startControlListener(socketPath, restarts, token, supervisor)
	if err != nil {
		supervisor.Stop()
		log.Fatalf("start control listener failed: %v", err)
	}
	log.Printf("control socket listening on %s", socketPath)
	defer func() {
		_ = server.Close()
		_ = os.Remove(socketPath)
	}()

	launch := panelLaunch{path: *panelPath, socket: socketPath, token: token}
	mode := backups.MaintenanceModeNormal
	for {
		next := launch
		next.mode = mode
		code, restartMode, err := runPanel(next, restarts, signals)
		if err != nil {
			log.Printf("panel failed: %v", err)
			shutdown(supervisor, socketPath, 1)
		}
		if restartMode == "" {
			shutdown(supervisor, socketPath, code)
		}
		mode = restartMode
	}
}

// shutdown 在结束进程前显式停止受管子进程并清理控制面 socket。os.Exit 不会执行
// defer，因此这里必须自行收敛 tailscaled，避免把子进程留给容器运行时强制清理；
// socket 文件也一并删除，避免在持久卷上留下无人监听的死 socket。
func shutdown(supervisor *tailscaleSupervisor, socketPath string, code int) {
	supervisor.Stop()
	_ = os.Remove(socketPath)
	os.Exit(code)
}

// startControlListener 在同一 Unix domain socket 上同时提供维护重启与 tailscale
// 控制面。两条路径共用同一个进程令牌；socket 固定位于持久卷的 <dataRoot>/run/
// 下，只允许 root 与降权后的 Panel 进程访问，进程不监听任何 TCP 端口。
func startControlListener(socketPath string, restarts chan<- string, token string, supervisor *tailscaleSupervisor) (*http.Server, error) {
	dir := filepath.Dir(socketPath)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("prepare control socket directory: %w", err)
	}
	// 上一次容器运行可能在持久卷上留下同路径的 socket 文件，残留会让 Listen
	// 直接失败：先清理再绑定。
	if err := os.Remove(socketPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("remove stale control socket: %w", err)
	}
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, err
	}
	// 控制面唯一的使用者是降权后的 Panel 子进程：目录对 panel 组开放 r-x，
	// socket 对 panel 组开放 rw，其他用户无权访问。
	_ = os.Chmod(dir, 0o750)
	_ = os.Chmod(socketPath, 0o660)
	if panelGID >= 0 {
		_ = os.Chown(dir, -1, panelGID)
		if err := os.Chown(socketPath, -1, panelGID); err != nil {
			log.Printf("chown control socket failed: %v", err)
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST "+backups.InitRestartPath, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(panelsupervision.ControlTokenHeader) != token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var req restartRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		mode := normalizeMode(req.Mode)
		select {
		case restarts <- mode:
		default:
		}
		w.WriteHeader(http.StatusAccepted)
	})
	mux.HandleFunc("POST /tailscale"+tailscale.InitApplyPath, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(panelsupervision.ControlTokenHeader) != token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		// 收敛在后台进行：控制面只确认“已接受请求”，真实状态由 /status 反映。
		supervisor.RequestReconcile()
		w.WriteHeader(http.StatusAccepted)
		writeTailscaleStatus(w, supervisor.Status())
	})
	mux.HandleFunc("GET /tailscale"+tailscale.InitStatusPath, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(panelsupervision.ControlTokenHeader) != token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		writeTailscaleStatus(w, supervisor.Status())
	})
	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("control listener failed: %v", err)
		}
	}()
	return server, nil
}

func writeTailscaleStatus(w http.ResponseWriter, status tailscale.Status) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(status)
}

// runPanel 启动 Panel 子进程，并在重启请求或终止信号到达时返回。restartMode
// 为空表示不应再启动子进程（正常退出或收到终止信号）。
func runPanel(launch panelLaunch, restarts <-chan string, signals <-chan os.Signal) (int, string, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	args := panelsupervision.Args(panelsupervision.Config{
		MaintenanceMode: launch.mode,
		ControlSocket:   launch.socket,
		ControlToken:    launch.token,
	})
	cmd := exec.CommandContext(ctx, launch.path, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Env = os.Environ()
	applyPanelCredential(cmd)
	if err := cmd.Start(); err != nil {
		return 1, "", err
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	// finish 把子进程退出结果翻译成 (exitCode, nextMode, error)。非零退出是
	// 失败，不能被排队中的重启请求掩盖。
	finish := func(err error, mode string) (int, string, error) {
		if err == nil {
			return 0, mode, nil
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode(), mode, nil
		}
		return 1, mode, err
	}

	for {
		select {
		case err := <-done:
			code, _, runErr := finish(err, "")
			if runErr != nil || code != 0 {
				return code, "", runErr
			}
			select {
			case nextMode := <-restarts:
				return 0, nextMode, nil
			default:
			}
			return 0, "", nil
		case nextMode := <-restarts:
			// The child may have already exited on its own before the restart
			// request won the select. Its exit status takes precedence: only a
			// clean exit or an active stop may adopt the queued restart.
			select {
			case err := <-done:
				return finish(err, nextMode)
			default:
			}
			if err := stopPanel(cmd, done); err != nil {
				return 1, "", err
			}
			return 0, nextMode, nil
		case <-signals:
			// SIGTERM 由 docker stop 发送。作为 PID 1 必须显式处理并转发给
			// Panel 子进程，否则容器只能在宽限期结束后被强制杀死。
			log.Printf("shutdown signal received; stopping panel")
			if err := stopPanel(cmd, done); err != nil {
				return 1, "", err
			}
			return 0, "", nil
		}
	}
}

func randomControlToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func stopPanel(cmd *exec.Cmd, done <-chan error) error {
	if cmd.Process == nil {
		return nil
	}
	_ = cmd.Process.Signal(os.Interrupt)
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	select {
	case err := <-done:
		var exitErr *exec.ExitError
		if err != nil && !errors.As(err, &exitErr) {
			return err
		}
	case <-timer.C:
		_ = cmd.Process.Kill()
		err := <-done
		var exitErr *exec.ExitError
		if err != nil && !errors.As(err, &exitErr) {
			return err
		}
	}
	return nil
}

func normalizeMode(mode string) string {
	switch mode {
	case backups.MaintenanceModeExport, backups.MaintenanceModeRestore, backups.MaintenanceModeNormal:
		return mode
	default:
		return backups.MaintenanceModeNormal
	}
}

func defaultPanelPath() string {
	if value := os.Getenv("PANEL_INIT_PANEL_PATH"); value != "" {
		return value
	}
	name := "panel"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if exe, err := os.Executable(); err == nil {
		return filepath.Join(filepath.Dir(exe), name)
	}
	return name
}

// defaultDataRoot 与镜像中的 PANEL_DATA_ROOT 默认值保持一致，使 panel-init 在
// 没有额外参数时也能找到 Panel 写入的 tailscale 期望态配置。
func defaultDataRoot() string {
	if value := strings.TrimSpace(os.Getenv("PANEL_DATA_ROOT")); value != "" {
		return value
	}
	return "/app/data"
}

func defaultPanelUser() string {
	if value := strings.TrimSpace(os.Getenv("PANEL_INIT_PANEL_USER")); value != "" {
		return value
	}
	return "panel"
}

func defaultTailscalePath() string {
	return lookupInitBinary("PANEL_INIT_TAILSCALE_PATH", "tailscale")
}

func defaultTailscaledPath() string {
	return lookupInitBinary("PANEL_INIT_TAILSCALED_PATH", "tailscaled")
}

// lookupInitBinary 解析容器内 tailscale 可执行文件。找不到时返回空串，
// panel-init 会把“不可用”写入状态，而不是让容器启动失败。
func lookupInitBinary(envVar, name string) string {
	if value := strings.TrimSpace(os.Getenv(envVar)); value != "" {
		return value
	}
	if path, err := exec.LookPath(name); err == nil {
		return path
	}
	return ""
}
