package server

import (
	"context"
	"crypto/x509"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"panel/internal/agent"
	"panel/internal/config"
	"panel/internal/credential"
	"panel/internal/linux"
	"panel/internal/remoteops"
	"panel/internal/secretstore"
	"panel/internal/sshx"
	"panel/internal/storage"
	"panel/internal/tasks"
)

func newServerTestCredentialService(t *testing.T, store *storage.Store, cfg config.Config) *credential.Service {
	t.Helper()
	secrets, err := secretstore.Open(cfg, store.AppDB())
	if err != nil {
		t.Fatal(err)
	}
	return credential.NewService(store.AppDB(), secrets)
}

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
	credSvc := newServerTestCredentialService(t, store, cfg)
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

	credSvc := newServerTestCredentialService(t, store, cfg)
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
	if srv.Traits["sys.cpu_cores"] != "8" || srv.Traits["sys.memory_total_mb"] != "16384" || srv.Traits["sys.disk_total_gb"] != "256" || srv.Traits["sys.hostname"] != "test-node" || srv.Traits["sys.architecture"] != "x86_64" || srv.Traits["sys.cpu_model"] != "AMD EPYC" || srv.Traits["sys.network_interfaces"] != "eth0|inet|10.0.0.10/24, eth0|inet6|2001:db8::10/64" || srv.Traits["sys.os"] != "debian-13" || srv.Traits["sys.ufw_supported"] != "true" || srv.Traits["sys.ufw_installed"] != "false" {
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

func TestEnableUFWInstallsWhenMissingAndAllowsSSHBeforeEnable(t *testing.T) {
	blockEnable := make(chan struct{})
	exec := &ufwEnableFakeExec{blockEnable: blockEnable}
	svc, taskSvc, store := testServerService(t, exec)
	if _, err := store.AppDB().Exec(`INSERT INTO servers(id,name,host,port,ssh_username,credential_id,os_id,os_version_id,os_pretty_name,os_supported,reachable,sudo_passwordless,created_at,updated_at) VALUES('srv_enable','s','127.0.0.1',22022,'du','cred_1','debian','13','Debian GNU/Linux 13',1,1,1,'now','now')`); err != nil {
		t.Fatal(err)
	}

	task, err := svc.EnableUFW(context.Background(), "srv_enable")
	if err != nil {
		t.Fatal(err)
	}
	if task.Type != ufwEnableTaskType || task.Status != tasks.StatusRunning || task.StartedAt == nil {
		t.Fatalf("expected running UFW enable task, got %#v", task)
	}
	close(blockEnable)
	waitTaskFinished(t, taskSvc, task.ID)

	commands := strings.Join(exec.commands, "\n---\n")
	installIndex := strings.Index(commands, "apt_get install -y ufw")
	allowIndex := strings.Index(commands, "ufw allow 22022/tcp")
	enableIndex := strings.Index(commands, "ufw --force enable")
	if installIndex < 0 || allowIndex < 0 || enableIndex < 0 || installIndex >= allowIndex || allowIndex >= enableIndex {
		t.Fatalf("expected install, SSH allow, then enable:\n%s", commands)
	}
	stored, err := svc.Get(context.Background(), "srv_enable")
	if err != nil {
		t.Fatal(err)
	}
	if stored.Traits["sys.ufw_installed"] != "true" || stored.Traits["sys.ufw_active"] != "true" {
		t.Fatalf("expected enabled UFW traits, got %#v", stored.Traits)
	}
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

func TestUFWStateUsesAgentWhenConfigured(t *testing.T) {
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
	traits := `{"agent.enabled":"true","agent.url":"https://127.0.0.1:9443"}`
	if _, err := store.AppDB().Exec(`INSERT INTO servers(id,name,host,port,ssh_username,credential_id,traits,os_id,os_version_id,os_supported,reachable,sudo_passwordless,created_at,updated_at) VALUES('srv_1','s','127.0.0.1',22,'du','cred_1',?,'debian','13',1,1,1,'now','now')`, traits); err != nil {
		t.Fatal(err)
	}
	taskSvc := tasks.NewService(store.AppDB())
	exec := &ufwManageFakeExec{}
	agentClient := &serverFakeAgentClient{ufw: remoteops.UFWStatus{Installed: true, Active: true, Status: "active", Rules: []remoteops.UFWRuleStatus{{Number: 7, To: "9443/tcp", Action: "ALLOW IN", From: "Anywhere"}}}}
	svc := NewService(store.AppDB(), exec, taskSvc)
	svc.SetAgentClient(agentClient)

	state, err := svc.UFWState(context.Background(), "srv_1")
	if err != nil {
		t.Fatal(err)
	}
	if agentClient.ufwURL != "https://127.0.0.1:9443" || len(exec.commands) != 0 {
		t.Fatalf("expected agent UFW status without SSH, agent=%q commands=%#v", agentClient.ufwURL, exec.commands)
	}
	if len(state.Rules) != 1 || state.Rules[0].Number != 7 {
		t.Fatalf("unexpected agent UFW state: %#v", state)
	}
}

func TestUFWWriteOperationsUseSSHWhenAgentConfigured(t *testing.T) {
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
	traits := `{"agent.enabled":"true","agent.url":"https://127.0.0.1:9443"}`
	if _, err := store.AppDB().Exec(`INSERT INTO servers(id,name,host,port,ssh_username,credential_id,traits,os_id,os_version_id,os_supported,reachable,sudo_passwordless,created_at,updated_at) VALUES('srv_1','s','127.0.0.1',22,'du','cred_1',?,'debian','13',1,1,1,'now','now')`, traits); err != nil {
		t.Fatal(err)
	}
	taskSvc := tasks.NewService(store.AppDB())
	exec := &ufwManageFakeExec{}
	agentClient := &serverFakeAgentClient{ufw: remoteops.UFWStatus{Installed: false, Status: "not_installed"}}
	svc := NewService(store.AppDB(), exec, taskSvc)
	svc.SetAgentClient(agentClient)

	if _, err := svc.AllowUFW(context.Background(), "srv_1", UFWAllowRequest{Port: 443, Protocol: "tcp"}); err != nil {
		t.Fatal(err)
	}
	commands := strings.Join(exec.commands, "\n")
	if !strings.Contains(commands, "panel_ufw_installed=true") || !strings.Contains(commands, "ufw allow 443/tcp") {
		t.Fatalf("expected SSH status and allow commands, got:\n%s", commands)
	}
	if agentClient.ufwURL != "" {
		t.Fatalf("write operation should not query agent UFW status, got %q", agentClient.ufwURL)
	}
}

func TestCheckConfiguredAgentsMarksIncompatibleVersion(t *testing.T) {
	svc, _, store := testServerService(t, nil)
	traits := `{"agent.enabled":"true","agent.url":"https://127.0.0.1:9443"}`
	if _, err := store.AppDB().Exec(`INSERT INTO servers(id,name,host,port,ssh_username,credential_id,traits,created_at,updated_at) VALUES('srv_agent','s','127.0.0.1',22,'du','cred_1',?,'now','now')`, traits); err != nil {
		t.Fatal(err)
	}
	svc.SetAgentClient(&serverFakeAgentClient{health: agentHealth("0.9.0")})

	svc.CheckConfiguredAgents(context.Background())
	srv, err := svc.Get(context.Background(), "srv_agent")
	if err != nil {
		t.Fatal(err)
	}
	if srv.Traits[agent.TraitStatus] != agent.StatusIncompatible || srv.Traits[agent.TraitVersion] != "0.9.0" {
		t.Fatalf("unexpected agent compatibility traits: %#v", srv.Traits)
	}
}

func TestCheckConfiguredAgentsMarksCompatible(t *testing.T) {
	svc, _, store := testServerService(t, nil)
	traits := `{"agent.enabled":"true","agent.url":"https://127.0.0.1:9443"}`
	if _, err := store.AppDB().Exec(`INSERT INTO servers(id,name,host,port,ssh_username,credential_id,traits,created_at,updated_at) VALUES('srv_agent','s','127.0.0.1',22,'du','cred_1',?,'now','now')`, traits); err != nil {
		t.Fatal(err)
	}
	svc.SetAgentClient(&serverFakeAgentClient{health: agentHealth(agent.Version)})

	svc.CheckConfiguredAgents(context.Background())
	srv, err := svc.Get(context.Background(), "srv_agent")
	if err != nil {
		t.Fatal(err)
	}
	if srv.Traits[agent.TraitStatus] != agent.StatusCompatible || srv.Traits[agent.TraitVersion] != agent.Version {
		t.Fatalf("unexpected agent compatibility traits: %#v", srv.Traits)
	}
}

func TestCheckConfiguredAgentsMarksCertificateTimeErrorIncompatible(t *testing.T) {
	svc, _, store := testServerService(t, nil)
	traits := `{"agent.enabled":"true","agent.url":"https://127.0.0.1:9443"}`
	if _, err := store.AppDB().Exec(`INSERT INTO servers(id,name,host,port,ssh_username,credential_id,traits,created_at,updated_at) VALUES('srv_agent','s','127.0.0.1',22,'du','cred_1',?,'now','now')`, traits); err != nil {
		t.Fatal(err)
	}
	certErr := x509.CertificateInvalidError{Reason: x509.Expired}
	svc.SetAgentClient(&serverFakeAgentClient{err: certErr})

	svc.CheckConfiguredAgents(context.Background())
	srv, err := svc.Get(context.Background(), "srv_agent")
	if err != nil {
		t.Fatal(err)
	}
	if srv.Traits[agent.TraitStatus] != agent.StatusIncompatible {
		t.Fatalf("expected certificate time error to mark agent incompatible, got %#v", srv.Traits)
	}
	if !strings.Contains(strings.ToLower(srv.Traits[agent.TraitLastError]), "certificate") {
		t.Fatalf("expected certificate error to be recorded, got %#v", srv.Traits)
	}
}

func TestDeployAgentStartsExistingQueuedTask(t *testing.T) {
	createSvc, taskSvc, store := testServerService(t, nil)
	traits := map[string]string{"sys.architecture": "x86_64"}
	srv, err := createSvc.Create(context.Background(), SaveRequest{Name: "s", Host: "127.0.0.1", Port: 22, SSHUsername: "du", CredentialID: "cred_1", Traits: traits})
	if err != nil {
		t.Fatal(err)
	}
	assets, err := agent.EnsureTLSAssets(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	svc := NewService(store.AppDB(), agentArchFakeExec{arch: "x86_64"}, taskSvc)
	svc.SetAgentTLSAssets(assets)
	svc.SetAgentClient(&serverFakeAgentClient{health: agentHealth(agent.Version)})
	queued, err := taskSvc.Create(context.Background(), tasks.CreateInput{
		Type:         agentDeployTaskType,
		ServerID:     srv.ID,
		ResourceType: connectivityResourceType,
		ResourceID:   srv.ID,
		Summary:      "Queued agent deploy",
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := svc.DeployAgent(context.Background(), srv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != queued.ID {
		t.Fatalf("expected existing deploy task to be reused, got %q want %q", got.ID, queued.ID)
	}
	finished := waitTaskTerminal(t, taskSvc, queued.ID)
	if finished.Status == tasks.StatusQueued {
		t.Fatalf("existing agent deploy task was not started: %#v", finished)
	}
}

func TestAgentCertificateTimeErrorStopsAutoDeployAfterFailures(t *testing.T) {
	createSvc, taskSvc, store := testServerService(t, nil)
	traits := map[string]string{
		agent.TraitEnabled: "true",
		agent.TraitURL:     "https://127.0.0.1:9443",
		"sys.architecture": "x86_64",
	}
	srv, err := createSvc.Create(context.Background(), SaveRequest{Name: "s", Host: "127.0.0.1", Port: 22, SSHUsername: "du", CredentialID: "cred_1", Traits: traits})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < agentAutoDeployMaxFailures; i++ {
		if _, err := taskSvc.Create(context.Background(), tasks.CreateInput{
			Type:         agentDeployTaskType,
			ServerID:     srv.ID,
			ResourceType: connectivityResourceType,
			ResourceID:   srv.ID,
			TriggeredBy:  "system",
			Status:       tasks.StatusFailed,
			Summary:      "failed agent deploy",
		}); err != nil {
			t.Fatal(err)
		}
	}
	assets, err := agent.EnsureTLSAssets(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	svc := NewService(store.AppDB(), agentArchFakeExec{arch: "x86_64"}, taskSvc)
	svc.SetAgentTLSAssets(assets)
	svc.SetAgentClient(&serverFakeAgentClient{health: agentHealth(agent.Version)})

	if !svc.HandleAgentError(context.Background(), srv, x509.CertificateInvalidError{Reason: x509.Expired}) {
		t.Fatal("expected certificate time error to be handled")
	}
	result, err := taskSvc.List(context.Background(), tasks.ListFilter{Type: agentDeployTaskType, ServerID: srv.ID, Statuses: []string{tasks.StatusQueued, tasks.StatusRunning, tasks.StatusFailedRetryable}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 0 {
		t.Fatalf("expected no new active auto deploy task after failures, got %#v", result.Items)
	}
	updated, err := svc.Get(context.Background(), srv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(updated.Traits[agent.TraitLastError], "stopped after") {
		t.Fatalf("expected exhausted auto deploy error, got %#v", updated.Traits)
	}
}

func TestSystemCertificatesIncludeOnlyPanelSideBuiltIns(t *testing.T) {
	svc, _, store := testServerService(t, nil)
	assets, err := agent.EnsureTLSAssets(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	svc.SetAgentTLSAssets(assets)
	traits := `{"agent.enabled":"true","agent.url":"https://127.0.0.1:9443","agent.status":"compatible","agent.certificate.fingerprint":"ABC","agent.certificate.not_after":"2027-01-01T00:00:00Z"}`
	if _, err := store.AppDB().Exec(`INSERT INTO servers(id,name,host,port,ssh_username,credential_id,traits,created_at,updated_at) VALUES('srv_agent','s','127.0.0.1',22,'du','cred_1',?,'now','now')`, traits); err != nil {
		t.Fatal(err)
	}

	items, err := svc.SystemCertificates(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("expected only Panel-side CA and client certificates, got %#v", items)
	}
	if items[0].ID != "agent-ca" || !items[0].BuiltIn || !items[0].CanReset {
		t.Fatalf("unexpected agent CA item: %#v", items[0])
	}
	if items[1].ID != "agent-panel-client" || !items[1].BuiltIn || !items[1].CanReset {
		t.Fatalf("unexpected agent client item: %#v", items[1])
	}
}

func TestResetPanelAgentClientCertificateReloadsHTTPClient(t *testing.T) {
	svc, taskSvc, _ := testServerService(t, nil)
	assets, err := agent.EnsureTLSAssets(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	client, err := agent.NewHTTPClient(assets, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	svc.SetAgentTLSAssets(assets)
	svc.SetAgentClient(client)
	before, _ := assets.ClientInfo()

	task, err := svc.ResetSystemCertificate(context.Background(), "agent-panel-client")
	if err != nil {
		t.Fatal(err)
	}
	finished := waitTaskTerminal(t, taskSvc, task.ID)
	if finished.Status != tasks.StatusCompleted {
		t.Fatalf("reset task failed: %#v", finished)
	}
	after, _ := assets.ClientInfo()
	if after.Fingerprint == before.Fingerprint {
		t.Fatal("expected client certificate to change")
	}
}

func TestUFWStateDoesNotFallbackOnAgentCertificateTimeError(t *testing.T) {
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
	traits := `{"agent.enabled":"true","agent.url":"https://127.0.0.1:9443"}`
	if _, err := store.AppDB().Exec(`INSERT INTO servers(id,name,host,port,ssh_username,credential_id,traits,os_id,os_version_id,os_supported,reachable,sudo_passwordless,created_at,updated_at) VALUES('srv_1','s','127.0.0.1',22,'du','cred_1',?,'debian','13',1,1,1,'now','now')`, traits); err != nil {
		t.Fatal(err)
	}
	exec := &ufwManageFakeExec{}
	svc := NewService(store.AppDB(), exec, tasks.NewService(store.AppDB()))
	certErr := x509.CertificateInvalidError{Reason: x509.Expired}
	svc.SetAgentClient(&serverFakeAgentClient{err: certErr})

	if _, err := svc.UFWState(context.Background(), "srv_1"); err == nil {
		t.Fatal("expected agent certificate error")
	}
	if len(exec.commands) != 0 {
		t.Fatalf("expected no SSH fallback, got commands %#v", exec.commands)
	}
	srv, err := svc.Get(context.Background(), "srv_1")
	if err != nil {
		t.Fatal(err)
	}
	if srv.Traits[agent.TraitStatus] != agent.StatusIncompatible {
		t.Fatalf("expected certificate time error to mark agent incompatible, got %#v", srv.Traits)
	}
}

func TestAgentCertificateTimeErrorDetectedFromMessage(t *testing.T) {
	err := errString(`Get "https://127.0.0.1:9443/v1/system/os-release": tls: failed to verify certificate: x509: certificate has expired or is not yet valid`)
	if !isAgentCertificateTimeError(err) {
		t.Fatal("expected Go TLS certificate time message to be detected")
	}
	if isAgentCertificateTimeError(errString("agent down")) {
		t.Fatal("unexpected non-certificate error detection")
	}
}

func TestAgentBinaryPathForPlatformIsFixed(t *testing.T) {
	cases := map[string]string{
		"linux-amd64": "/app/panel-agents/linux-amd64/panel-agent",
		"linux-arm64": "/app/panel-agents/linux-arm64/panel-agent",
	}
	for platform, want := range cases {
		if got := agentBinaryPathForPlatform(platform); got != want {
			t.Fatalf("expected %s path %q, got %q", platform, want, got)
		}
	}
}

func TestAgentTargetPlatformFallsBackToRemoteUname(t *testing.T) {
	svc := &Service{exec: agentArchFakeExec{arch: "x86_64"}}
	platform, err := svc.agentTargetPlatform(context.Background(), Server{Host: "127.0.0.1", Port: 22, CredentialID: "cred_1"})
	if err != nil {
		t.Fatal(err)
	}
	if platform != "linux-amd64" {
		t.Fatalf("expected linux-amd64, got %q", platform)
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
	if srv.InitialTaskID == "" {
		t.Fatal("expected create response to include initial task id")
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
	if task.ID != srv.InitialTaskID {
		t.Fatalf("initial task id mismatch response=%q stored=%q", srv.InitialTaskID, task.ID)
	}
	if task.Type != serverInfoTaskType || task.Summary != "Collecting server information" {
		t.Fatalf("unexpected initial task: %#v", task)
	}
	waitTaskFinished(t, taskSvc, task.ID)
}

func TestCreateServerInitialInfoFailureRollsBackServer(t *testing.T) {
	svc, taskSvc, _ := testServerService(t, failingConnectivityExec{})
	srv, err := svc.Create(context.Background(), SaveRequest{Name: "s", Host: "127.0.0.1", Port: 22, SSHUsername: "du", CredentialID: "cred_1"})
	if err != nil {
		t.Fatal(err)
	}
	if srv.InitialTaskID == "" {
		t.Fatal("expected create response to include initial task id")
	}

	task := waitTaskTerminal(t, taskSvc, srv.InitialTaskID)
	if task.Status != tasks.StatusFailed {
		t.Fatalf("expected initial task to fail without retry, got %#v", task)
	}
	if _, err := svc.Get(context.Background(), srv.ID); err == nil {
		t.Fatal("expected created server to be rolled back")
	}
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
	createSvc, taskSvc, store := testServerService(t, nil)
	srv, err := createSvc.Create(context.Background(), SaveRequest{Name: "s", Host: "127.0.0.1", Port: 22, SSHUsername: "du", CredentialID: "cred_1"})
	if err != nil {
		t.Fatal(err)
	}
	svc := NewService(store.AppDB(), failingConnectivityExec{}, taskSvc)
	if _, err := svc.EnsureConnectivityTask(context.Background(), srv.ID, true); err != nil {
		t.Fatal(err)
	}

	var task tasks.Task
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		result, err := taskSvc.List(context.Background(), tasks.ListFilter{Type: connectivityTaskType, ServerID: srv.ID})
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
		return sshx.CommandResult{Stdout: "cores=8\nmem=16384\ndisk=256\nhostname=test-node\narch=x86_64\ncpu_model=AMD EPYC\nnic=eth0|inet|10.0.0.10/24\nnic=eth0|inet6|2001:db8::10/64\nnic=docker0|inet|172.17.0.1/16\nnic=veth123|inet6|fe80::1/64\nufw_installed=false\nufw_active=false\n", ExitCode: 0}, nil
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

type agentArchFakeExec struct {
	arch string
}

func (f agentArchFakeExec) Exec(ctx context.Context, target sshx.Target, command sshx.CommandSpec) (sshx.CommandResult, error) {
	if strings.TrimSpace(command.Command) == "uname -m" {
		return sshx.CommandResult{Stdout: f.arch + "\n", ExitCode: 0}, nil
	}
	return sshx.CommandResult{ExitCode: 0}, nil
}

func (f agentArchFakeExec) ExecSudo(context.Context, sshx.Target, sshx.CommandSpec) (sshx.CommandResult, error) {
	return sshx.CommandResult{ExitCode: 0}, nil
}

func (f agentArchFakeExec) Upload(context.Context, sshx.Target, sshx.UploadSpec) error {
	return nil
}

func (f agentArchFakeExec) Download(context.Context, sshx.Target, sshx.DownloadSpec) error {
	return nil
}

func TestVirtualNetworkInterfaceDetection(t *testing.T) {
	for _, name := range []string{"lo", "docker0", "veth123", "br-abcd", "virbr0", "cni0", "flannel.1", "cali123", "tun0", "tap0", "wg0", "tailscale0", "ztabc"} {
		if !isVirtualNetworkInterface(name) {
			t.Fatalf("expected %q to be treated as virtual", name)
		}
	}
	for _, name := range []string{"eth0", "ens3", "enp5s0", "bond0"} {
		if isVirtualNetworkInterface(name) {
			t.Fatalf("expected %q to be retained", name)
		}
	}
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

type ufwEnableFakeExec struct {
	installed   bool
	active      bool
	commands    []string
	blockEnable <-chan struct{}
}

func (f *ufwEnableFakeExec) Exec(_ context.Context, _ sshx.Target, command sshx.CommandSpec) (sshx.CommandResult, error) {
	if strings.Contains(command.Command, "cat /etc/os-release") {
		return sshx.CommandResult{Stdout: "ID=debian\nVERSION_ID=\"13\"\nPRETTY_NAME=\"Debian GNU/Linux 13\"\n", ExitCode: 0}, nil
	}
	if strings.Contains(command.Command, "cores=") {
		return sshx.CommandResult{Stdout: "cores=8\nmem=16384\ndisk=256\nhostname=test-node\narch=x86_64\ncpu_model=AMD EPYC\nnic=eth0|inet|10.0.0.10/24\nufw_installed=" + boolString(f.installed) + "\nufw_active=" + boolString(f.active) + "\n", ExitCode: 0}, nil
	}
	return sshx.CommandResult{ExitCode: 0}, nil
}

func (f *ufwEnableFakeExec) ExecSudo(_ context.Context, _ sshx.Target, command sshx.CommandSpec) (sshx.CommandResult, error) {
	if strings.TrimSpace(command.Command) == "true" {
		return sshx.CommandResult{ExitCode: 0}, nil
	}
	if strings.Contains(command.Command, "panel_ufw_installed") {
		if !f.installed {
			return sshx.CommandResult{Stdout: "panel_ufw_installed=false\n", ExitCode: 0}, nil
		}
		status := "inactive"
		if f.active {
			status = "active"
		}
		return sshx.CommandResult{Stdout: "panel_ufw_installed=true\nStatus: " + status + "\npanel_ufw_numbered_begin\n", ExitCode: 0}, nil
	}
	if strings.Contains(command.Command, "apt_get install -y ufw") {
		f.installed = true
		f.commands = append(f.commands, command.Command)
		return sshx.CommandResult{ExitCode: 0}, nil
	}
	if strings.Contains(command.Command, "ufw --force enable") {
		if f.blockEnable != nil {
			<-f.blockEnable
		}
		f.active = true
		f.commands = append(f.commands, command.Command)
		return sshx.CommandResult{ExitCode: 0}, nil
	}
	return sshx.CommandResult{ExitCode: 0}, nil
}

func (f *ufwEnableFakeExec) Upload(context.Context, sshx.Target, sshx.UploadSpec) error {
	return nil
}

func (f *ufwEnableFakeExec) Download(context.Context, sshx.Target, sshx.DownloadSpec) error {
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

type serverFakeAgentClient struct {
	ufwURL string
	ufw    remoteops.UFWStatus
	health agent.HealthResponse
	err    error
}

func agentHealth(version string) agent.HealthResponse {
	return agent.HealthResponse{
		Status:       "ok",
		Version:      version,
		Capabilities: agent.RequiredCapabilities,
		Docker:       agent.DockerHealth{Host: agent.DefaultDockerHost, Status: "ok"},
	}
}

func (f *serverFakeAgentClient) Health(context.Context, string) (agent.HealthResponse, error) {
	if f.health.Version == "" {
		f.health = agentHealth(agent.Version)
	}
	return f.health, f.err
}
func (f *serverFakeAgentClient) OSRelease(context.Context, string) (linux.OSRelease, error) {
	return linux.OSRelease{}, f.err
}
func (f *serverFakeAgentClient) SystemTraits(context.Context, string) (map[string]string, error) {
	return nil, f.err
}
func (f *serverFakeAgentClient) MetricsSnapshot(context.Context, string, string) (linux.MetricsSnapshot, error) {
	return linux.MetricsSnapshot{}, f.err
}
func (f *serverFakeAgentClient) UFWStatus(_ context.Context, url string) (remoteops.UFWStatus, error) {
	f.ufwURL = url
	return f.ufw, f.err
}

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
	_ = waitTaskTerminal(t, taskSvc, taskID)
}

func waitTaskTerminal(t *testing.T, taskSvc *tasks.Service, taskID string) tasks.Task {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		task, err := taskSvc.Get(context.Background(), taskID)
		if err != nil {
			t.Fatal(err)
		}
		if task.Status == tasks.StatusCompleted || task.Status == tasks.StatusFailed || task.Status == tasks.StatusFailedRetryable || task.Status == tasks.StatusBlocked {
			return task
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("task did not finish")
	return tasks.Task{}
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
