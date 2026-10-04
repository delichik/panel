package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"panel/internal/platform/tailscale"
)

const (
	// tailscaleReconcileInterval 是无外部触发时的自愈周期。tailscaled 崩溃、
	// 首次启动尚未登录或管理员在远端改了 ACL 都能在一个周期内收敛。
	tailscaleReconcileInterval = 30 * time.Second
	// tailscaleSocketWait 限制等待 tailscaled 暴露 LocalAPI socket 的时间。
	tailscaleSocketWait = 15 * time.Second
	// tailscaleCommandTimeout 限制单条 tailscale 子命令的执行时间。
	tailscaleCommandTimeout = 30 * time.Second
	// tailscaleStopGrace 限制停止 tailscaled 时的优雅等待时间。
	tailscaleStopGrace = 5 * time.Second
)

// tailscaleSupervisor 独占管理容器内 tailscaled 进程与登录状态。
//
// 它是 panel-init 中唯一读写 tailscale 期望态与实际态的组件：Panel 只通过
// 控制面接口下发“重新收敛”并读取状态，从不直接接触 tailscaled socket。
// 任何失败都必须降级为“tailscale 不可用”并写入状态，绝不终止 Panel 进程。
type tailscaleSupervisor struct {
	dataRoot  string
	binTS     string
	binDaemon string

	trigger chan struct{}
	stop    chan struct{}
	once    sync.Once
	wg      sync.WaitGroup

	mu      sync.Mutex
	status  tailscale.Status
	desired tailscale.Config
	process *exec.Cmd
	waitCh  chan error
	lastLog string

	logger *log.Logger
}

func newTailscaleSupervisor(dataRoot, binTS, binDaemon string, logger *log.Logger) *tailscaleSupervisor {
	if logger == nil {
		logger = log.Default()
	}
	return &tailscaleSupervisor{
		dataRoot:  strings.TrimSpace(dataRoot),
		binTS:     strings.TrimSpace(binTS),
		binDaemon: strings.TrimSpace(binDaemon),
		trigger:   make(chan struct{}, 1),
		stop:      make(chan struct{}),
		logger:    logger,
		status: tailscale.Status{
			Available: true,
		},
	}
}

// Start 恢复上次状态快照并启动收敛循环。该方法不返回错误：容器内 tailscale
// 不可用不应阻止 Panel 启动。
func (s *tailscaleSupervisor) Start(ctx context.Context) {
	if s == nil {
		return
	}
	if previous, err := tailscale.ReadStatus(s.dataRoot); err == nil {
		s.mu.Lock()
		s.status = previous
		s.mu.Unlock()
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.loop(ctx)
	}()
	s.RequestReconcile()
}

// RequestReconcile 请求一次异步收敛。重复请求会被合并，调用方不被阻塞。
func (s *tailscaleSupervisor) RequestReconcile() {
	if s == nil {
		return
	}
	select {
	case s.trigger <- struct{}{}:
	default:
	}
}

// Status 返回最近一次收敛得到的实际态快照。
func (s *tailscaleSupervisor) Status() tailscale.Status {
	if s == nil {
		return tailscale.Status{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

// Stop 停止收敛循环并结束 tailscaled 进程。它可以被重复调用。停止后写入的
// 快照不再声称进程存活：容器重启后 tailscaled 已随容器结束，保留 running=true
// 会让下一次启动先对外报告一个不存在的进程。
func (s *tailscaleSupervisor) Stop() {
	if s == nil {
		return
	}
	s.once.Do(func() { close(s.stop) })
	s.wg.Wait()
	ctx, cancel := context.WithTimeout(context.Background(), tailscaleStopGrace+time.Second)
	defer cancel()
	s.stopProcess(ctx)
	s.publish(tailscale.Status{Available: s.binTS != "" && s.binDaemon != ""})
}

func (s *tailscaleSupervisor) loop(ctx context.Context) {
	ticker := time.NewTicker(tailscaleReconcileInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-s.trigger:
		}
		s.reconcile(ctx)
	}
}

func (s *tailscaleSupervisor) reconcile(ctx context.Context) {
	cfg, err := tailscale.ReadConfig(s.dataRoot)
	if err != nil {
		// Panel 写入的配置不可解析时不能猜测意图：停止进程并明确报错。
		s.fail(ctx, "invalid tailscale config: "+err.Error())
		return
	}
	s.mu.Lock()
	s.desired = cfg
	s.mu.Unlock()

	if !cfg.Enabled {
		s.stopProcess(ctx)
		s.publish(tailscale.Status{Available: true})
		return
	}
	if s.binTS == "" || s.binDaemon == "" {
		s.fail(ctx, "tailscale is not installed in this container")
		return
	}
	if err := s.ensureProcess(ctx); err != nil {
		s.fail(ctx, err.Error())
		return
	}
	status, err := s.probe(ctx)
	if err != nil {
		s.fail(ctx, err.Error())
		return
	}
	if status.BackendState != "Running" {
		if err := s.ensureLogin(ctx, cfg, status); err != nil {
			s.fail(ctx, err.Error())
			return
		}
		if status, err = s.probe(ctx); err != nil {
			s.fail(ctx, err.Error())
			return
		}
	}
	status.Available = true
	s.publish(status)
}

// ensureLogin 只在节点尚未进入 Running 时执行 tailscale up。认证密钥通过
// TS_AUTHKEY 环境变量传递，绝不进入命令行参数，避免出现在容器内进程列表里。
func (s *tailscaleSupervisor) ensureLogin(ctx context.Context, cfg tailscale.Config, status tailscale.Status) error {
	switch status.BackendState {
	case "NeedsMachineAuth":
		return errors.New("tailscale node is waiting for admin approval in the tailnet")
	case "NeedsLogin", "NoState", "", "Stopped", "Starting":
	default:
		return fmt.Errorf("tailscale backend state is %s", status.BackendState)
	}
	if cfg.AuthKey == "" && status.BackendState == "NeedsLogin" {
		return errors.New("tailscale auth key is required to join the tailnet")
	}
	args := []string{"--socket=" + tailscale.SocketPath(s.dataRoot), "up", "--accept-dns=false", "--hostname=" + cfg.Hostname}
	if len(cfg.Tags) > 0 {
		args = append(args, "--advertise-tags="+strings.Join(cfg.Tags, ","))
	}
	cmd := exec.CommandContext(ctx, s.binTS, args...)
	cmd.Env = os.Environ()
	if cfg.AuthKey != "" {
		cmd.Env = append(cmd.Env, "TS_AUTHKEY="+cfg.AuthKey)
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tailscale up failed: %s", s.redact(strings.TrimSpace(string(output)), cfg.AuthKey))
	}
	if text := strings.TrimSpace(string(output)); text != "" {
		s.logger.Printf("tailscale up: %s", s.redact(text, cfg.AuthKey))
	}
	return nil
}

// probe 通过 LocalAPI 读取节点状态，并只接受 tailnet 网段内的自身地址。
func (s *tailscaleSupervisor) probe(ctx context.Context) (tailscale.Status, error) {
	callCtx, cancel := context.WithTimeout(ctx, tailscaleCommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(callCtx, s.binTS, "--socket="+tailscale.SocketPath(s.dataRoot), "status", "--json")
	output, err := cmd.Output()
	if err != nil {
		return tailscale.Status{}, fmt.Errorf("tailscale status failed: %v", err)
	}
	state, err := tailscale.ParseStatusJSON(output)
	if err != nil {
		return tailscale.Status{}, err
	}
	return tailscale.Status{
		Available:    true,
		Running:      true,
		LoggedIn:     state.LoggedIn(),
		Hostname:     state.Hostname,
		IPv4:         state.IPv4,
		IPv6:         state.IPv6,
		Version:      state.Version,
		BackendState: state.BackendState,
		UpdatedAt:    time.Now().UTC(),
	}, nil
}

// ensureProcess 保证 tailscaled 存活并暴露 LocalAPI socket。
func (s *tailscaleSupervisor) ensureProcess(ctx context.Context) error {
	if s.processAlive() {
		return s.waitForSocket(ctx)
	}
	dir, err := tailscale.EnsureDir(s.dataRoot, panelUID, panelGID)
	if err != nil {
		return fmt.Errorf("prepare tailscale directory: %v", err)
	}
	cmd := exec.Command(s.binDaemon,
		"--state="+tailscale.StatePath(s.dataRoot),
		"--socket="+tailscale.SocketPath(s.dataRoot),
		"--tun=tailscale0",
	)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start tailscaled: %v", err)
	}
	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()
	s.mu.Lock()
	s.process = cmd
	s.waitCh = waitCh
	s.mu.Unlock()
	s.logger.Printf("tailscaled started (pid %d)", cmd.Process.Pid)
	return s.waitForSocket(ctx)
}

// stopProcess 结束 tailscaled。节点身份保存在状态文件中，因此停止进程不会
// 让节点退出 tailnet。
func (s *tailscaleSupervisor) stopProcess(ctx context.Context) {
	s.mu.Lock()
	cmd := s.process
	waitCh := s.waitCh
	s.process = nil
	s.waitCh = nil
	s.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Signal(os.Interrupt)
	timer := time.NewTimer(tailscaleStopGrace)
	defer timer.Stop()
	select {
	case <-waitCh:
	case <-timer.C:
		_ = cmd.Process.Kill()
		select {
		case <-waitCh:
		case <-time.After(tailscaleStopGrace):
		}
	case <-ctx.Done():
		_ = cmd.Process.Kill()
	}
	s.logger.Printf("tailscaled stopped")
}

func (s *tailscaleSupervisor) processAlive() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.process == nil || s.process.Process == nil {
		return false
	}
	select {
	case err := <-s.waitCh:
		s.logger.Printf("tailscaled exited: %v", err)
		s.process = nil
		s.waitCh = nil
		return false
	default:
		return true
	}
}

// waitForSocket 等待 tailscaled 暴露 LocalAPI socket。这里只确认 socket 路径
// 出现：真正的可用性由随后的状态读取判定，避免把“文件存在”当成服务就绪。
func (s *tailscaleSupervisor) waitForSocket(ctx context.Context) error {
	socket := tailscale.SocketPath(s.dataRoot)
	deadline := time.Now().Add(tailscaleSocketWait)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if _, err := os.Stat(socket); err == nil {
			return nil
		}
		if !s.processAlive() {
			return errors.New("tailscaled exited before its control socket was ready")
		}
		time.Sleep(200 * time.Millisecond)
	}
	return errors.New("tailscaled control socket did not become ready")
}

func (s *tailscaleSupervisor) publish(status tailscale.Status) {
	if status.UpdatedAt.IsZero() {
		status.UpdatedAt = time.Now().UTC()
	}
	s.mu.Lock()
	previous := s.status
	s.status = status
	s.mu.Unlock()
	if previous.LastError != "" && status.LastError == "" {
		s.logger.Printf("tailscale recovered from: %s", previous.LastError)
	}
	if status.LastError != "" && status.LastError != s.lastLog {
		s.lastLog = status.LastError
		s.logger.Printf("tailscale error: %s", status.LastError)
	}
	if err := tailscale.WriteStatus(s.dataRoot, status); err != nil {
		s.logger.Printf("write tailscale status failed: %v", err)
	}
}

// fail 记录失败并收敛进程：running 为 false 时不保留半启动状态。
func (s *tailscaleSupervisor) fail(ctx context.Context, message string) {
	s.mu.Lock()
	available := s.binTS != "" && s.binDaemon != ""
	s.mu.Unlock()
	if !available {
		s.stopProcess(ctx)
	}
	s.publish(tailscale.Status{Available: available, Running: available && s.processAlive(), LastError: message})
}

// redact 从日志文本中移除认证密钥，保证 GOV-DOD-009 的秘密不外泄要求。
func (s *tailscaleSupervisor) redact(text, secret string) string {
	if secret == "" {
		return text
	}
	return strings.ReplaceAll(text, secret, "[redacted]")
}
