package server

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"panel/internal/config"
	"panel/internal/credential"
	"panel/internal/sshx"
	"panel/internal/storage"
	"panel/internal/tasks"
)

func TestServerValidation(t *testing.T) {
	if err := validateSave(SaveRequest{Port: 22}); err == nil {
		t.Fatal("expected validation error")
	}
	if err := validateSave(SaveRequest{Name: "s", Host: "127.0.0.1", Port: 22, CredentialID: "cred"}); err != nil {
		t.Fatalf("server username should be optional: %v", err)
	}
	if err := validateSave(SaveRequest{Name: "s", Host: "127.0.0.1", Port: 70000, CredentialID: "cred"}); err == nil {
		t.Fatal("expected port validation error")
	}
}

func TestCreateListServer(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	credSvc := credential.NewService(store.AppDB(), cfg)
	cred, err := credSvc.Create(context.Background(), credential.CreateRequest{Name: "c", Type: credential.TypePassword, Username: "du", Password: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	taskSvc := tasks.NewService(store.AppDB())
	svc := NewService(store.AppDB(), nil, taskSvc)
	_, err = svc.Create(context.Background(), SaveRequest{Name: "s", Host: "127.0.0.1", Port: 22, SSHUsername: "du", CredentialID: cred.ID, Traits: map[string]string{"custom.env": "prod"}})
	if err != nil {
		t.Fatal(err)
	}
	servers, err := svc.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 1 || servers[0].Traits["custom.env"] != "prod" {
		t.Fatalf("unexpected servers: %#v", servers)
	}
}

func TestListServersLoadsMetricsDBLoadAverage(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.AppDB().Exec(`INSERT INTO credentials(id,name,type,username,created_at,updated_at) VALUES('cred_1','c','password','du','now','now')`); err != nil {
		t.Fatal(err)
	}
	taskSvc := tasks.NewService(store.AppDB())
	svc := NewService(store.AppDB(), nil, taskSvc)
	svc.SetMetricsDB(store.MetricsDB())
	srv, err := svc.Create(context.Background(), SaveRequest{Name: "s", Host: "127.0.0.1", Port: 22, SSHUsername: "du", CredentialID: "cred_1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.MetricsDB().Exec(`INSERT INTO metrics_snapshots(server_id,time,cpu_usage_percent,memory_used_bytes,memory_total_bytes,disk_used_bytes,disk_total_bytes,network_rx_bps,network_tx_bps,load_average) VALUES(?,?,?,?,?,?,?,?,?,?)`, srv.ID, time.Now().UTC().Format(time.RFC3339Nano), 0, 0, 0, 0, 0, 0, 0, "0.42 0.30 0.20"); err != nil {
		t.Fatal(err)
	}

	servers, err := svc.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 1 || servers[0].LoadAverage != "0.42 0.30 0.20" {
		t.Fatalf("expected metrics DB load average, got %#v", servers)
	}
	got, err := svc.Get(context.Background(), srv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.LoadAverage != "0.42 0.30 0.20" {
		t.Fatalf("expected Get to include load average, got %#v", got)
	}
}

func TestConnectivityUsesBoundedSudoTimeoutAndCompletes(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	credSvc := credential.NewService(store.AppDB(), cfg)
	cred, err := credSvc.Create(context.Background(), credential.CreateRequest{Name: "c", Type: credential.TypePassword, Username: "du", Password: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	taskSvc := tasks.NewService(store.AppDB())
	exec := &connectivityFakeExec{}
	svc := NewService(store.AppDB(), exec, taskSvc)
	srv, err := svc.Create(context.Background(), SaveRequest{Name: "s", Host: "127.0.0.1", Port: 22, SSHUsername: "du", CredentialID: cred.ID})
	if err != nil {
		t.Fatal(err)
	}
	task, err := svc.TestConnectivity(context.Background(), srv.ID)
	if err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	var got tasks.Task
	for time.Now().Before(deadline) {
		got, err = taskSvc.Get(context.Background(), task.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status == tasks.StatusCompleted || got.Status == tasks.StatusFailed {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got.Status != tasks.StatusCompleted {
		t.Fatalf("expected completed task, got %#v", got)
	}
	if exec.sudoTimeout != connectivitySudoTimeout {
		t.Fatalf("expected sudo timeout %s, got %s", connectivitySudoTimeout, exec.sudoTimeout)
	}

	// 验证系统特征是否成功探测并自动入库
	srv, err = svc.Get(context.Background(), srv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if srv.Traits["sys.cpu_cores"] != "8" || srv.Traits["sys.memory_total_mb"] != "16384" || srv.Traits["sys.disk_total_gb"] != "256" || srv.Traits["sys.hostname"] != "test-node" || srv.Traits["sys.architecture"] != "x86_64" || srv.Traits["sys.cpu_model"] != "AMD EPYC" || srv.Traits["sys.network_interfaces"] != "eth0|inet|10.0.0.10/24" || srv.Traits["sys.os"] != "debian-13" || srv.Traits["sys.ufw_supported"] != "true" || srv.Traits["sys.ufw_installed"] != "false" {
		t.Fatalf("unexpected system traits detected: %#v", srv.Traits)
	}

	logs, _, err := taskSvc.Logs(context.Background(), task.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, log := range logs {
		if log.Line == "passwordless sudo unavailable: sudo denied" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected sudo unavailable log, got %#v", logs)
	}
}

func TestProbeConnectivityReturnsSynchronousResult(t *testing.T) {
	svc, _, _ := testServerService(t, &connectivityFakeExec{root: true})
	result, err := svc.ProbeConnectivity(context.Background(), SaveRequest{Host: "127.0.0.1", Port: 22, SSHUsername: "root", CredentialID: "cred_1"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Reachable || !result.Root || !result.Privileged {
		t.Fatalf("expected reachable root probe, got %#v", result)
	}
	if result.Traits["sys.cpu_cores"] != "8" || result.Traits["sys.ufw_supported"] != "true" || result.OS.PrettyName != "Debian GNU/Linux 13" {
		t.Fatalf("unexpected probe detail: %#v", result)
	}
}

func TestInstallUFWCreatesTaskAndRefreshesTraits(t *testing.T) {
	exec := &ufwInstallFakeExec{}
	svc, taskSvc, _ := testServerService(t, exec)
	srv, err := svc.Create(context.Background(), SaveRequest{Name: "s", Host: "127.0.0.1", Port: 22, SSHUsername: "du", CredentialID: "cred_1"})
	if err != nil {
		t.Fatal(err)
	}
	srv = waitServerReady(t, svc, srv.ID)

	task, err := svc.InstallUFW(context.Background(), srv.ID)
	if err != nil {
		t.Fatal(err)
	}
	waitTaskFinished(t, taskSvc, task.ID)

	if task.Type != ufwInstallTaskType || task.ResourceType != connectivityResourceType || task.ResourceID != srv.ID {
		t.Fatalf("unexpected task metadata: %#v", task)
	}
	if !strings.Contains(exec.installCommand, "apt_get install -y ufw") ||
		!strings.Contains(exec.installCommand, "ufw --version") ||
		!strings.Contains(exec.installCommand, "ufw allow 22/tcp") {
		t.Fatalf("unexpected install command: %s", exec.installCommand)
	}
	assertNoDestructiveUFWCommands(t, exec.installCommand)
	if exec.installTimeout != ufwInstallTimeout {
		t.Fatalf("expected install timeout %s, got %s", ufwInstallTimeout, exec.installTimeout)
	}
	stored, err := svc.Get(context.Background(), srv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Traits["sys.ufw_supported"] != "true" || stored.Traits["sys.ufw_installed"] != "true" || stored.Traits["sys.ufw_active"] != "false" {
		t.Fatalf("expected refreshed UFW traits, got %#v", stored.Traits)
	}
}

func TestInstallUFWMarksTaskRunningBeforeWorkerExecutes(t *testing.T) {
	blockInstall := make(chan struct{})
	exec := &ufwInstallFakeExec{blockInstall: blockInstall}
	svc, taskSvc, _ := testServerService(t, exec)
	srv, err := svc.Create(context.Background(), SaveRequest{Name: "s", Host: "127.0.0.1", Port: 22, SSHUsername: "du", CredentialID: "cred_1"})
	if err != nil {
		t.Fatal(err)
	}
	srv = waitServerReady(t, svc, srv.ID)

	task, err := svc.InstallUFW(context.Background(), srv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != tasks.StatusRunning || task.StartedAt == nil {
		t.Fatalf("expected returned task to be running before worker executes, got %#v", task)
	}
	stored, err := taskSvc.Get(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != tasks.StatusRunning || stored.StartedAt == nil {
		t.Fatalf("expected stored task to be running before worker executes, got %#v", stored)
	}

	close(blockInstall)
	waitTaskFinished(t, taskSvc, task.ID)
}

func TestInstallUFWAllowsConfiguredSSHAndReverseProxyPorts(t *testing.T) {
	exec := &ufwInstallFakeExec{}
	svc, taskSvc, _ := testServerService(t, exec)
	srv, err := svc.Create(context.Background(), SaveRequest{
		Name:         "s",
		Host:         "127.0.0.1",
		Port:         22022,
		SSHUsername:  "du",
		CredentialID: "cred_1",
		Traits:       map[string]string{reverseProxyEnabledTrait: "true"},
	})
	if err != nil {
		t.Fatal(err)
	}
	srv = waitServerReady(t, svc, srv.ID)

	task, err := svc.InstallUFW(context.Background(), srv.ID)
	if err != nil {
		t.Fatal(err)
	}
	waitTaskFinished(t, taskSvc, task.ID)

	for _, want := range []string{"ufw allow 22022/tcp", "ufw allow 80/tcp", "ufw allow 443/tcp"} {
		if !strings.Contains(exec.installCommand, want) {
			t.Fatalf("install command missing %q:\n%s", want, exec.installCommand)
		}
	}
	assertNoDestructiveUFWCommands(t, exec.installCommand)
}

func TestRestartCreatesRunningTaskAndSchedulesReboot(t *testing.T) {
	blockRestart := make(chan struct{})
	exec := &restartFakeExec{blockRestart: blockRestart}
	svc, taskSvc, store := testServerService(t, exec)
	if _, err := store.AppDB().Exec(`INSERT INTO servers(id,name,host,port,ssh_username,credential_id,os_id,os_version_id,os_supported,reachable,sudo_passwordless,created_at,updated_at) VALUES('srv_restart','s','127.0.0.1',22,'du','cred_1','debian','13',1,1,1,'now','now')`); err != nil {
		t.Fatal(err)
	}

	task, err := svc.Restart(context.Background(), "srv_restart")
	if err != nil {
		t.Fatal(err)
	}
	if task.Type != restartTaskType || task.Status != tasks.StatusRunning || task.StartedAt == nil {
		t.Fatalf("expected running restart task, got %#v", task)
	}
	stored, err := taskSvc.Get(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != tasks.StatusRunning {
		t.Fatalf("expected stored restart task to be running, got %#v", stored)
	}

	close(blockRestart)
	waitTaskFinished(t, taskSvc, task.ID)
	if exec.timeout != restartTimeout {
		t.Fatalf("expected restart timeout %s, got %s", restartTimeout, exec.timeout)
	}
	if !strings.Contains(exec.command, "sleep 1; systemctl reboot") || !strings.Contains(exec.command, "shutdown -r now") {
		t.Fatalf("unexpected restart command:\n%s", exec.command)
	}
}

func TestUFWStateAllowAndDeleteRule(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.AppDB().Exec(`INSERT INTO credentials(id,name,type,username,created_at,updated_at) VALUES('cred_1','c','password','du','now','now')`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppDB().Exec(`INSERT INTO servers(id,name,host,port,ssh_username,credential_id,os_id,os_version_id,os_supported,reachable,sudo_passwordless,created_at,updated_at) VALUES('srv_1','s','127.0.0.1',22,'du','cred_1','debian','13',1,1,1,'now','now')`); err != nil {
		t.Fatal(err)
	}
	taskSvc := tasks.NewService(store.AppDB())
	exec := &ufwManageFakeExec{}
	svc := NewService(store.AppDB(), exec, taskSvc)

	state, err := svc.UFWState(context.Background(), "srv_1")
	if err != nil {
		t.Fatal(err)
	}
	if !state.Installed || !state.Active || len(state.Rules) != 1 || state.Rules[0].To != "22/tcp" {
		t.Fatalf("unexpected UFW state: %#v", state)
	}
	if _, err := svc.AllowUFW(context.Background(), "srv_1", UFWAllowRequest{Port: 443, Protocol: "tcp", From: "10.0.0.0/8"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.DeleteUFWRule(context.Background(), "srv_1", 1); err != nil {
		t.Fatal(err)
	}
	commands := strings.Join(exec.commands, "\n")
	if !strings.Contains(commands, "ufw allow from '10.0.0.0/8' to any port 443 proto tcp") {
		t.Fatalf("expected allow command, got:\n%s", commands)
	}
	if !strings.Contains(commands, "ufw --force delete 1") {
		t.Fatalf("expected delete command, got:\n%s", commands)
	}
}

func assertNoDestructiveUFWCommands(t *testing.T, command string) {
	t.Helper()
	lower := strings.ToLower(command)
	for _, verb := range []string{"delete", "reset", "deny", "default", "enable", "disable", "reload"} {
		for _, prefix := range []string{"ufw " + verb, "ufw --force " + verb} {
			if strings.Contains(lower, prefix) {
				t.Fatalf("UFW script must not manage existing rules with %q:\n%s", prefix, command)
			}
		}
	}
}

func TestCreateServerAutomaticallyStartsInitialInfoTask(t *testing.T) {
	svc, taskSvc, _ := testServerService(t, &connectivityFakeExec{})
	srv, err := svc.Create(context.Background(), SaveRequest{Name: "s", Host: "127.0.0.1", Port: 22, SSHUsername: "du", CredentialID: "cred_1"})
	if err != nil {
		t.Fatal(err)
	}

	var task tasks.Task
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		result, err := taskSvc.List(context.Background(), tasks.ListFilter{Type: serverInfoTaskType, ServerID: srv.ID})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Items) == 1 {
			task = result.Items[0]
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if task.ID == "" {
		t.Fatal("expected auto server info task")
	}
	if task.Type != serverInfoTaskType || task.Summary != "Collecting server information" {
		t.Fatalf("unexpected initial task: %#v", task)
	}
	waitTaskFinished(t, taskSvc, task.ID)
}

func TestListStaleServersSharesConnectivityOperation(t *testing.T) {
	createSvc, taskSvc, store := testServerService(t, nil)
	for _, name := range []string{"s1", "s2"} {
		if _, err := createSvc.Create(context.Background(), SaveRequest{Name: name, Host: "127.0.0.1", Port: 22, SSHUsername: "du", CredentialID: "cred_1"}); err != nil {
			t.Fatal(err)
		}
	}
	svc := NewService(store.AppDB(), &connectivityFakeExec{}, taskSvc)

	if _, err := svc.List(context.Background()); err != nil {
		t.Fatal(err)
	}

	result, err := taskSvc.List(context.Background(), tasks.ListFilter{Type: connectivityTaskType, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 2 {
		t.Fatalf("expected two stale connectivity tasks, got %#v", result.Items)
	}
	operationID := result.Items[0].OperationID
	if operationID == "" || result.Items[1].OperationID != operationID {
		t.Fatalf("expected stale connectivity tasks to share operation, got %#v", result.Items)
	}
}

func TestConnectivityFailureSchedulesRetryAndRunNow(t *testing.T) {
	svc, taskSvc, _ := testServerService(t, failingConnectivityExec{})
	srv, err := svc.Create(context.Background(), SaveRequest{Name: "s", Host: "127.0.0.1", Port: 22, SSHUsername: "du", CredentialID: "cred_1"})
	if err != nil {
		t.Fatal(err)
	}

	var task tasks.Task
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		result, err := taskSvc.List(context.Background(), tasks.ListFilter{Type: serverInfoTaskType, ServerID: srv.ID})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Items) == 1 {
			task = result.Items[0]
			if task.Status == tasks.StatusFailedRetryable {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if task.Status != tasks.StatusFailedRetryable || task.NextRunAt == nil || task.RetryCount != 1 {
		t.Fatalf("expected retryable scheduled task, got %#v", task)
	}
	task, err = taskSvc.RunNow(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != tasks.StatusQueued || task.NextRunAt != nil {
		t.Fatalf("run now should queue immediately, got %#v", task)
	}
}

type connectivityFakeExec struct {
	sudoTimeout time.Duration
	root        bool
}

func (f *connectivityFakeExec) Exec(ctx context.Context, target sshx.Target, command sshx.CommandSpec) (sshx.CommandResult, error) {
	if strings.Contains(command.Command, "cat /etc/os-release") {
		return sshx.CommandResult{Stdout: "ID=debian\nVERSION_ID=\"13\"\nPRETTY_NAME=\"Debian GNU/Linux 13\"\n", ExitCode: 0}, nil
	}
	if strings.Contains(command.Command, "cores=") {
		return sshx.CommandResult{Stdout: "cores=8\nmem=16384\ndisk=256\nhostname=test-node\narch=x86_64\ncpu_model=AMD EPYC\nnic=eth0|inet|10.0.0.10/24\nufw_installed=false\nufw_active=false\n", ExitCode: 0}, nil
	}
	if strings.Contains(command.Command, "id -u") {
		if f.root {
			return sshx.CommandResult{Stdout: "0\n", ExitCode: 0}, nil
		}
		return sshx.CommandResult{Stdout: "1000\n", ExitCode: 0}, nil
	}
	return sshx.CommandResult{ExitCode: 0}, nil
}

func (f *connectivityFakeExec) ExecSudo(ctx context.Context, target sshx.Target, command sshx.CommandSpec) (sshx.CommandResult, error) {
	f.sudoTimeout = command.Timeout
	return sshx.CommandResult{ExitCode: 1}, errString("sudo denied")
}

func (f *connectivityFakeExec) Upload(ctx context.Context, target sshx.Target, transfer sshx.UploadSpec) error {
	return nil
}

func (f *connectivityFakeExec) Download(ctx context.Context, target sshx.Target, transfer sshx.DownloadSpec) error {
	return nil
}

type ufwInstallFakeExec struct {
	installed      bool
	installCommand string
	installTimeout time.Duration
	blockInstall   <-chan struct{}
}

func (f *ufwInstallFakeExec) Exec(ctx context.Context, target sshx.Target, command sshx.CommandSpec) (sshx.CommandResult, error) {
	if strings.Contains(command.Command, "cat /etc/os-release") {
		return sshx.CommandResult{Stdout: "ID=debian\nVERSION_ID=\"13\"\nPRETTY_NAME=\"Debian GNU/Linux 13\"\n", ExitCode: 0}, nil
	}
	if strings.Contains(command.Command, "cores=") {
		installed := "false"
		if f.installed {
			installed = "true"
		}
		return sshx.CommandResult{Stdout: "cores=8\nmem=16384\ndisk=256\nhostname=test-node\narch=x86_64\ncpu_model=AMD EPYC\nnic=eth0|inet|10.0.0.10/24\nufw_installed=" + installed + "\nufw_active=false\n", ExitCode: 0}, nil
	}
	return sshx.CommandResult{ExitCode: 0}, nil
}

func (f *ufwInstallFakeExec) ExecSudo(ctx context.Context, target sshx.Target, command sshx.CommandSpec) (sshx.CommandResult, error) {
	if strings.TrimSpace(command.Command) == "true" {
		return sshx.CommandResult{ExitCode: 0}, nil
	}
	if f.blockInstall != nil {
		<-f.blockInstall
	}
	f.installCommand = command.Command
	f.installTimeout = command.Timeout
	f.installed = true
	if command.OnStdout != nil {
		command.OnStdout("installed ufw")
	}
	return sshx.CommandResult{Stdout: "installed ufw\n", ExitCode: 0}, nil
}

func (f *ufwInstallFakeExec) Upload(ctx context.Context, target sshx.Target, transfer sshx.UploadSpec) error {
	return nil
}

func (f *ufwInstallFakeExec) Download(ctx context.Context, target sshx.Target, transfer sshx.DownloadSpec) error {
	return nil
}

type ufwManageFakeExec struct {
	commands []string
}

func (f *ufwManageFakeExec) Exec(context.Context, sshx.Target, sshx.CommandSpec) (sshx.CommandResult, error) {
	return sshx.CommandResult{ExitCode: 0}, nil
}

func (f *ufwManageFakeExec) ExecSudo(_ context.Context, _ sshx.Target, command sshx.CommandSpec) (sshx.CommandResult, error) {
	f.commands = append(f.commands, command.Command)
	if strings.Contains(command.Command, "panel_ufw_installed") {
		return sshx.CommandResult{Stdout: `panel_ufw_installed=true
Status: active
Default: deny (incoming), allow (outgoing), disabled (routed)
panel_ufw_numbered_begin
Status: active

     To                         Action      From
     --                         ------      ----
[ 1] 22/tcp                     ALLOW IN    Anywhere
`, ExitCode: 0}, nil
	}
	return sshx.CommandResult{ExitCode: 0}, nil
}

func (f *ufwManageFakeExec) Upload(context.Context, sshx.Target, sshx.UploadSpec) error {
	return nil
}

func (f *ufwManageFakeExec) Download(context.Context, sshx.Target, sshx.DownloadSpec) error {
	return nil
}

type restartFakeExec struct {
	command      string
	timeout      time.Duration
	blockRestart <-chan struct{}
}

func (f *restartFakeExec) Exec(context.Context, sshx.Target, sshx.CommandSpec) (sshx.CommandResult, error) {
	return sshx.CommandResult{ExitCode: 0}, nil
}

func (f *restartFakeExec) ExecSudo(_ context.Context, _ sshx.Target, command sshx.CommandSpec) (sshx.CommandResult, error) {
	if f.blockRestart != nil {
		<-f.blockRestart
	}
	f.command = command.Command
	f.timeout = command.Timeout
	if command.OnStdout != nil {
		command.OnStdout("[panel] scheduling server restart")
	}
	return sshx.CommandResult{Stdout: "[panel] scheduling server restart\n", ExitCode: 0}, nil
}

func (f *restartFakeExec) Upload(context.Context, sshx.Target, sshx.UploadSpec) error {
	return nil
}

func (f *restartFakeExec) Download(context.Context, sshx.Target, sshx.DownloadSpec) error {
	return nil
}

type errString string

func (e errString) Error() string { return string(e) }

type failingConnectivityExec struct{}

func (failingConnectivityExec) Exec(context.Context, sshx.Target, sshx.CommandSpec) (sshx.CommandResult, error) {
	return sshx.CommandResult{}, errString("dial timeout")
}

func (failingConnectivityExec) ExecSudo(context.Context, sshx.Target, sshx.CommandSpec) (sshx.CommandResult, error) {
	return sshx.CommandResult{}, errString("dial timeout")
}

func (failingConnectivityExec) Upload(context.Context, sshx.Target, sshx.UploadSpec) error {
	return nil
}

func (failingConnectivityExec) Download(context.Context, sshx.Target, sshx.DownloadSpec) error {
	return nil
}

func testServerService(t *testing.T, exec sshx.RemoteExecutor) (*Service, *tasks.Service, *storage.Store) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if _, err := store.AppDB().Exec(`INSERT INTO credentials(id,name,type,username,created_at,updated_at) VALUES('cred_1','c','password','du','now','now')`); err != nil {
		t.Fatal(err)
	}
	taskSvc := tasks.NewService(store.AppDB())
	return NewService(store.AppDB(), exec, taskSvc), taskSvc, store
}

func waitTaskFinished(t *testing.T, taskSvc *tasks.Service, taskID string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		task, err := taskSvc.Get(context.Background(), taskID)
		if err != nil {
			t.Fatal(err)
		}
		if task.Status == tasks.StatusCompleted || task.Status == tasks.StatusFailed || task.Status == tasks.StatusFailedRetryable || task.Status == tasks.StatusBlocked {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("task did not finish")
}

func waitServerReady(t *testing.T, svc *Service, serverID string) Server {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		srv, err := svc.Get(context.Background(), serverID)
		if err != nil {
			t.Fatal(err)
		}
		if srv.Reachable && srv.OS.Supported && srv.Sudo.Passwordless {
			return srv
		}
		time.Sleep(20 * time.Millisecond)
	}
	srv, err := svc.Get(context.Background(), serverID)
	if err != nil {
		t.Fatal(err)
	}
	t.Fatalf("server did not become ready: %#v", srv)
	return Server{}
}
