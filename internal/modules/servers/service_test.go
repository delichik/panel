package server

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	agentclient "panel/internal/agent/client"
	agentcontract "panel/internal/agent/contract"
	agentsecurity "panel/internal/agent/security"
	"panel/internal/modules/servers/credential"
	"panel/internal/modules/tasks"
	"panel/internal/platform/config"
	storage "panel/internal/platform/database"
	panelerr "panel/internal/platform/errors"
	"panel/internal/platform/linux"
	"panel/internal/platform/linux/remoteops"
	"panel/internal/platform/secrets"
	"panel/internal/platform/ssh"
)

func newServerTestCredentialService(t *testing.T, store *storage.Store, cfg config.Config) *credential.Service {
	t.Helper()
	secrets, err := secretstore.Open(cfg, store.AppDB())
	if err != nil {
		t.Fatal(err)
	}
	return credential.NewService(store.AppDB(), secrets)
}

func newServerServiceForTest(store *storage.Store, exec sshx.RemoteExecutor, taskSvc *tasks.Service, opts ...Option) *Service {
	svc := NewService(store.AppDB(), exec, taskSvc, opts...)
	svc.RegisterTasks(taskSvc)
	return svc
}

func (s *Service) SetMetricsDB(db *sql.DB) {
	s.metricsDB = db
}

func (s *Service) SetAgentClient(client agentcontract.Client) {
	s.agent = client
}

func (s *Service) SetAgentTLSAssets(assets *agentsecurity.TLSAssets) {
	s.agentTLS = assets
}

func setServerArchitecture(t *testing.T, store *storage.Store, serverID string) {
	t.Helper()
	if _, err := store.AppDB().Exec(`UPDATE servers SET architecture_os='linux', architecture_arch='amd64', architecture_machine='x86_64' WHERE id=?`, serverID); err != nil {
		t.Fatal(err)
	}
}

func setServerTraits(t *testing.T, store *storage.Store, serverID string, traits map[string]string) {
	t.Helper()
	raw, err := json.Marshal(traits)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppDB().Exec(`UPDATE servers SET traits=? WHERE id=?`, string(raw), serverID); err != nil {
		t.Fatal(err)
	}
}

func TestServerValidation(t *testing.T) {
	if err := validateSave(SaveRequest{Port: 22}); err == nil {
		t.Fatal("expected validation error")
	}
	if err := validateSave(SaveRequest{Name: "s", IPv4: "127.0.0.1", Port: 22, CredentialID: "cred"}); err != nil {
		t.Fatalf("server username should be optional: %v", err)
	}
	if err := validateSave(SaveRequest{Name: "s", IPv4: "127.0.0.1", Port: 70000, CredentialID: "cred"}); err == nil {
		t.Fatal("expected port validation error")
	}
	if err := validateSave(SaveRequest{Name: "s", Host: "127.0.0.1", Port: 22, CredentialID: "cred"}); err == nil {
		t.Fatal("expected direct host to be rejected")
	}
	if err := validateSave(SaveRequest{Name: "s", IPv4: "example.com", Port: 22, CredentialID: "cred"}); err == nil {
		t.Fatal("expected invalid ipv4 to be rejected")
	}
	if err := validateSave(SaveRequest{Name: "s", IPv6: "127.0.0.1", Port: 22, CredentialID: "cred"}); err == nil {
		t.Fatal("expected IPv4 literal in ipv6 to be rejected")
	}
	if got := derivedServerHost(SaveRequest{IPv4: "203.0.113.5", IPv6: "2001:db8::5"}); got != "203.0.113.5" {
		t.Fatalf("expected ipv4 to win, got %q", got)
	}
	if got := derivedServerHost(SaveRequest{IPv6: "2001:db8::5"}); got != "2001:db8::5" {
		t.Fatalf("expected ipv6 fallback, got %q", got)
	}
}

func TestLegacyServerReadDerivesIPv4IPv6FromHost(t *testing.T) {
	svc, _, store := testServerService(t, nil)
	if _, err := store.AppDB().Exec(`INSERT INTO servers(id,name,host,port,ssh_username,credential_id,traits,created_at,updated_at) VALUES('srv_legacy','legacy','203.0.113.7',22,'du','cred_1','{}','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Get(context.Background(), "srv_legacy")
	if err != nil {
		t.Fatal(err)
	}
	if got.IPv4 != "203.0.113.7" || got.IPv6 != "" {
		t.Fatalf("expected ipv4 derived from host, got ipv4=%q ipv6=%q", got.IPv4, got.IPv6)
	}

	if _, err := store.AppDB().Exec(`INSERT INTO servers(id,name,host,port,ssh_username,credential_id,traits,created_at,updated_at) VALUES('srv_legacy6','legacy6','2001:db8::7',22,'du','cred_1','{}','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	got6, err := svc.Get(context.Background(), "srv_legacy6")
	if err != nil {
		t.Fatal(err)
	}
	if got6.IPv4 != "" || got6.IPv6 != "2001:db8::7" {
		t.Fatalf("expected ipv6 derived from host, got ipv4=%q ipv6=%q", got6.IPv4, got6.IPv6)
	}

	// Stored ipv4/ipv6 always win over host-derived values.
	if _, err := store.AppDB().Exec(`UPDATE servers SET ipv4='192.0.2.1', ipv6='2001:db8::1' WHERE id='srv_legacy'`); err != nil {
		t.Fatal(err)
	}
	gotStored, err := svc.Get(context.Background(), "srv_legacy")
	if err != nil {
		t.Fatal(err)
	}
	if gotStored.IPv4 != "192.0.2.1" || gotStored.IPv6 != "2001:db8::1" {
		t.Fatalf("expected stored ipv4/ipv6 to win, got ipv4=%q ipv6=%q", gotStored.IPv4, gotStored.IPv6)
	}
}

func TestCreateListServer(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.LogDatabase = filepath.Join(dir, "log.db")
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
	taskSvc := tasks.NewService(store.LogDB())
	svc := newServerServiceForTest(store, nil, taskSvc)
	_, err = svc.Create(context.Background(), SaveRequest{Name: "s", IPv4: "127.0.0.1", Port: 22, SSHUsername: "du", CredentialID: cred.ID})
	if err != nil {
		t.Fatal(err)
	}
	servers, err := svc.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 1 || len(servers[0].Traits) != 0 {
		t.Fatalf("unexpected servers: %#v", servers)
	}
}

func TestListServersLoadsMetricsDBLoadAverage(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.LogDatabase = filepath.Join(dir, "log.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.AppDB().Exec(`INSERT INTO credentials(id,name,type,username,created_at,updated_at) VALUES('cred_1','c','password','du','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	taskSvc := tasks.NewService(store.LogDB())
	svc := newServerServiceForTest(store, nil, taskSvc)
	svc.SetMetricsDB(store.MetricsDB())
	srv, err := svc.Create(context.Background(), SaveRequest{Name: "s", IPv4: "127.0.0.1", Port: 22, SSHUsername: "du", CredentialID: "cred_1"})
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

func TestDeleteServerCancelsTasksAndCleansLocalReferences(t *testing.T) {
	svc, taskSvc, store := testServerService(t, nil)
	svc.SetMetricsDB(store.MetricsDB())
	srv, err := svc.Create(context.Background(), SaveRequest{Name: "s", IPv4: "127.0.0.1", Port: 22, SSHUsername: "du", CredentialID: "cred_1"})
	if err != nil {
		t.Fatal(err)
	}
	running, err := taskSvc.Create(context.Background(), tasks.CreateInput{Type: serverInfoTaskType, ServerID: srv.ID, ResourceType: "server", ResourceID: srv.ID, Status: tasks.StatusRunning, Summary: "running"})
	if err != nil {
		t.Fatal(err)
	}
	queued, err := taskSvc.Create(context.Background(), tasks.CreateInput{Type: agentDeployTaskType, ServerID: srv.ID, ResourceType: "server", ResourceID: srv.ID, Status: tasks.StatusQueued, Summary: "queued"})
	if err != nil {
		t.Fatal(err)
	}
	retryable, err := taskSvc.Create(context.Background(), tasks.CreateInput{Type: restartTaskType, ServerID: srv.ID, ResourceType: "server", ResourceID: srv.ID, Status: tasks.StatusFailedRetryable, Summary: "retryable"})
	if err != nil {
		t.Fatal(err)
	}
	if !taskSvc.HasRunningExecution(running.ID) {
		t.Fatal("expected running task execution to be registered")
	}
	if _, err := store.MetricsDB().Exec(`INSERT INTO metrics_snapshots(server_id,time,cpu_usage_percent,memory_used_bytes,memory_total_bytes,disk_used_bytes,disk_total_bytes,network_rx_bps,network_tx_bps,load_average) VALUES(?,?,?,?,?,?,?,?,?,?)`, srv.ID, time.Now().UTC().Format(time.RFC3339Nano), 0, 0, 0, 0, 0, 0, 0, "0.42"); err != nil {
		t.Fatal(err)
	}
	targets, _ := json.Marshal([]string{srv.ID, "srv_other"})
	if _, err := store.AppDB().Exec(`INSERT INTO applications(id,name,enabled,spec_yaml,deployment_mode,deployment_server_ids_json,generation,job_id,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		"app_1", "app", 1, "name: app\nimage: nginx\n", "selected", string(targets), 1, "job_1", "2026-01-01T00:00:00Z", "2026-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	cards := `[{"id":"card_1","kind":"cpu","width":3,"height":2,"range":"1h","networkDirection":"both","serverIds":["` + srv.ID + `","srv_other"]}]`
	if _, err := store.AppDB().Exec(`INSERT INTO overview_card_configurations(id,cards_json,updated_at) VALUES('default',?,'2026-01-01T00:00:00Z')`, cards); err != nil {
		t.Fatal(err)
	}

	if err := svc.Delete(context.Background(), srv.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Get(context.Background(), srv.ID); err == nil {
		t.Fatal("expected server to be deleted")
	}
	for _, taskID := range []string{running.ID, queued.ID, retryable.ID} {
		task, err := taskSvc.Get(context.Background(), taskID)
		if err != nil {
			t.Fatal(err)
		}
		if task.Status != tasks.StatusCancelled {
			t.Fatalf("expected task %s to be cancelled, got %#v", taskID, task)
		}
		if err := taskSvc.Complete(context.Background(), taskID, "should not overwrite cancellation"); err != nil {
			t.Fatal(err)
		}
		task, _ = taskSvc.Get(context.Background(), taskID)
		if task.Status != tasks.StatusCancelled {
			t.Fatalf("cancelled task %s was overwritten: %#v", taskID, task)
		}
	}
	if taskSvc.HasRunningExecution(running.ID) {
		t.Fatal("expected cancelled running task execution to be removed")
	}
	var metricCount int
	if err := store.MetricsDB().QueryRow(`SELECT COUNT(*) FROM metrics_snapshots WHERE server_id=?`, srv.ID).Scan(&metricCount); err != nil {
		t.Fatal(err)
	}
	if metricCount != 0 {
		t.Fatalf("expected metrics to be removed, got %d", metricCount)
	}
	var rawTargets string
	var appVersion int
	if err := store.AppDB().QueryRow(`SELECT deployment_server_ids_json,version FROM applications WHERE id='app_1'`).Scan(&rawTargets, &appVersion); err != nil {
		t.Fatal(err)
	}
	if rawTargets != `["srv_other"]` {
		t.Fatalf("expected application target to be pruned, got %s", rawTargets)
	}
	if appVersion != 2 {
		t.Fatalf("expected application version to increment after target pruning, got %d", appVersion)
	}
	var rawCards string
	if err := store.AppDB().QueryRow(`SELECT cards_json FROM overview_card_configurations WHERE id='default'`).Scan(&rawCards); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rawCards, srv.ID) || !strings.Contains(rawCards, "srv_other") {
		t.Fatalf("expected overview card server IDs to be pruned, got %s", rawCards)
	}
}

func TestConnectivityUsesBoundedSudoTimeoutAndCompletes(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.LogDatabase = filepath.Join(dir, "log.db")
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
	taskSvc := tasks.NewService(store.LogDB())
	exec := &connectivityFakeExec{}
	svc := newServerServiceForTest(store, exec, taskSvc)
	srv, err := svc.Create(context.Background(), SaveRequest{Name: "s", IPv4: "127.0.0.1", Port: 22, SSHUsername: "du", CredentialID: cred.ID})
	if err != nil {
		t.Fatal(err)
	}
	initialTaskID := srv.InitialTaskID
	waitTaskFinished(t, taskSvc, initialTaskID)
	checked, err := svc.TestConnectivity(context.Background(), srv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !checked.Reachable {
		t.Fatalf("expected connected server, got %#v", checked)
	}
	if exec.sudoTimeout != connectivitySudoTimeout {
		t.Fatalf("expected sudo timeout %s, got %s", connectivitySudoTimeout, exec.sudoTimeout)
	}

	// 初始任务只探测部署 Agent 所需的架构信息。
	srv, err = svc.Get(context.Background(), srv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if srv.Traits["sys.os"] != "debian-13" || srv.Traits["sys.ufw_supported"] != "true" ||
		srv.Architecture.OS != "linux" || srv.Architecture.Arch != "amd64" || srv.Architecture.RawMachine != "x86_64" {
		t.Fatalf("unexpected system traits detected: %#v", srv.Traits)
	}

	logs, _, err := taskSvc.Logs(context.Background(), initialTaskID, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, log := range logs {
		if log.Line == "privileged access unavailable: sudo denied" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected sudo unavailable log, got %#v", logs)
	}
}

func TestProbeConnectivityReturnsSynchronousResult(t *testing.T) {
	svc, _, _ := testServerService(t, &connectivityFakeExec{root: true})
	result, err := svc.ProbeConnectivity(context.Background(), SaveRequest{IPv4: "127.0.0.1", Port: 22, SSHUsername: "root", CredentialID: "cred_1"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Reachable || !result.Root || !result.Privileged || result.PrivilegeMode != sshx.PrivilegeModeRoot {
		t.Fatalf("expected reachable root probe, got %#v", result)
	}
	if result.Traits["sys.ufw_supported"] != "true" || result.OS.PrettyName != "Debian GNU/Linux 13" ||
		result.Architecture.OS != "linux" || result.Architecture.Arch != "amd64" {
		t.Fatalf("unexpected probe detail: %#v", result)
	}
}

func TestInitialCollectionPersistsRootPrivilege(t *testing.T) {
	svc, _, _ := testServerService(t, &connectivityFakeExec{root: true})
	srv, err := svc.Create(context.Background(), SaveRequest{Name: "root", IPv4: "127.0.0.1", Port: 22, SSHUsername: "root", CredentialID: "cred_1"})
	if err != nil {
		t.Fatal(err)
	}
	srv = waitServerReady(t, svc, srv.ID)
	if srv.Privilege.Mode != sshx.PrivilegeModeRoot || !srv.Privilege.Privileged || srv.Sudo.Passwordless {
		t.Fatalf("unexpected persisted root privilege: %#v", srv.Privilege)
	}
}

func TestInstallUFWCreatesTaskAndRefreshesTraits(t *testing.T) {
	exec := &ufwInstallFakeExec{}
	svc, taskSvc, _ := testServerService(t, exec)
	srv, err := svc.Create(context.Background(), SaveRequest{Name: "s", IPv4: "127.0.0.1", Port: 22, SSHUsername: "du", CredentialID: "cred_1"})
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
	srv, err := svc.Create(context.Background(), SaveRequest{Name: "s", IPv4: "127.0.0.1", Port: 22, SSHUsername: "du", CredentialID: "cred_1"})
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
	svc, taskSvc, store := testServerService(t, exec)
	srv, err := svc.Create(context.Background(), SaveRequest{
		Name:         "s",
		IPv4:         "127.0.0.1",
		Port:         22022,
		SSHUsername:  "du",
		CredentialID: "cred_1",
	})
	if err != nil {
		t.Fatal(err)
	}
	setServerTraits(t, store, srv.ID, map[string]string{reverseProxyEnabledTrait: "true"})
	srv, err = svc.Get(context.Background(), srv.ID)
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
	if _, err := store.AppDB().Exec(`INSERT INTO servers(id,name,host,port,ssh_username,credential_id,os_id,os_version_id,os_pretty_name,os_supported,reachable,sudo_passwordless,privilege_mode,created_at,updated_at) VALUES('srv_enable','s','127.0.0.1',22022,'du','cred_1','debian','13','Debian GNU/Linux 13',1,1,1,'passwordless_sudo','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`); err != nil {
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
	if _, err := store.AppDB().Exec(`INSERT INTO servers(id,name,host,port,ssh_username,credential_id,os_id,os_version_id,os_supported,reachable,sudo_passwordless,privilege_mode,created_at,updated_at) VALUES('srv_restart','s','127.0.0.1',22,'du','cred_1','debian','13',1,1,1,'passwordless_sudo','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`); err != nil {
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
	cfg.LogDatabase = filepath.Join(dir, "log.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.AppDB().Exec(`INSERT INTO credentials(id,name,type,username,created_at,updated_at) VALUES('cred_1','c','password','du','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppDB().Exec(`INSERT INTO servers(id,name,host,port,ssh_username,credential_id,os_id,os_version_id,os_supported,reachable,sudo_passwordless,privilege_mode,created_at,updated_at) VALUES('srv_1','s','127.0.0.1',22,'du','cred_1','debian','13',1,1,1,'passwordless_sudo','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	taskSvc := tasks.NewService(store.LogDB())
	exec := &ufwManageFakeExec{}
	svc := newServerServiceForTest(store, exec, taskSvc)

	if _, err := svc.UFWState(context.Background(), "srv_1"); err == nil {
		t.Fatal("expected agent-required UFW status failure")
	}
	if len(exec.commands) != 0 {
		t.Fatalf("expected no SSH UFW status fallback, got %#v", exec.commands)
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
	cfg.LogDatabase = filepath.Join(dir, "log.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.AppDB().Exec(`INSERT INTO credentials(id,name,type,username,created_at,updated_at) VALUES('cred_1','c','password','du','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	traits := `{"agent.enabled":"true","agent.url":"https://127.0.0.1:9786","agent.status":"compatible"}`
	if _, err := store.AppDB().Exec(`INSERT INTO servers(id,name,host,port,ssh_username,credential_id,traits,os_id,os_version_id,os_supported,reachable,sudo_passwordless,privilege_mode,created_at,updated_at) VALUES('srv_1','s','127.0.0.1',22,'du','cred_1',?,'debian','13',1,1,1,'passwordless_sudo','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`, traits); err != nil {
		t.Fatal(err)
	}
	taskSvc := tasks.NewService(store.LogDB())
	exec := &ufwManageFakeExec{}
	agentClient := &serverFakeAgentClient{ufw: remoteops.UFWStatus{Installed: true, Active: true, Status: "active", Rules: []remoteops.UFWRuleStatus{{Number: 7, To: "9786/tcp", Action: "ALLOW IN", From: "Anywhere"}}}}
	svc := newServerServiceForTest(store, exec, taskSvc)
	svc.SetAgentClient(agentClient)

	state, err := svc.UFWState(context.Background(), "srv_1")
	if err != nil {
		t.Fatal(err)
	}
	if agentClient.ufwURL != "https://127.0.0.1:9786" || len(exec.commands) != 0 {
		t.Fatalf("expected agent UFW status without SSH, agent=%q commands=%#v", agentClient.ufwURL, exec.commands)
	}
	if len(state.Rules) != 1 || state.Rules[0].Number != 7 {
		t.Fatalf("unexpected agent UFW state: %#v", state)
	}
}

func TestUFWStateUsesCompatibleAgentWithoutStoredPrivilege(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.LogDatabase = filepath.Join(dir, "log.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.AppDB().Exec(`INSERT INTO credentials(id,name,type,username,created_at,updated_at) VALUES('cred_1','c','password','du','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	traits := `{"agent.enabled":"true","agent.url":"https://127.0.0.1:9786","agent.status":"compatible"}`
	if _, err := store.AppDB().Exec(`INSERT INTO servers(id,name,host,port,ssh_username,credential_id,traits,os_id,os_version_id,os_supported,reachable,sudo_passwordless,privilege_mode,created_at,updated_at) VALUES('srv_1','s','127.0.0.1',22,'du','cred_1',?,'debian','13',1,1,0,'none','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`, traits); err != nil {
		t.Fatal(err)
	}
	taskSvc := tasks.NewService(store.LogDB())
	exec := &ufwManageFakeExec{}
	agentClient := &serverFakeAgentClient{ufw: remoteops.UFWStatus{Installed: true, Active: false, Status: "inactive"}}
	svc := newServerServiceForTest(store, exec, taskSvc)
	svc.SetAgentClient(agentClient)

	state, err := svc.UFWState(context.Background(), "srv_1")
	if err != nil {
		t.Fatal(err)
	}
	if agentClient.ufwURL != "https://127.0.0.1:9786" || len(exec.commands) != 0 {
		t.Fatalf("expected compatible agent without SSH fallback, agent=%q commands=%#v", agentClient.ufwURL, exec.commands)
	}
	if !state.Installed || state.Active {
		t.Fatalf("unexpected agent UFW state: %#v", state)
	}
}

func TestUFWWriteOperationsUseAgentWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.LogDatabase = filepath.Join(dir, "log.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.AppDB().Exec(`INSERT INTO credentials(id,name,type,username,created_at,updated_at) VALUES('cred_1','c','password','du','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	traits := `{"agent.enabled":"true","agent.url":"https://127.0.0.1:9786","agent.status":"compatible"}`
	if _, err := store.AppDB().Exec(`INSERT INTO servers(id,name,host,port,ssh_username,credential_id,traits,os_id,os_version_id,os_supported,reachable,sudo_passwordless,privilege_mode,created_at,updated_at) VALUES('srv_1','s','127.0.0.1',22,'du','cred_1',?,'debian','13',1,1,1,'passwordless_sudo','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`, traits); err != nil {
		t.Fatal(err)
	}
	taskSvc := tasks.NewService(store.LogDB())
	exec := &ufwManageFakeExec{}
	agentClient := &serverFakeAgentClient{ufw: remoteops.UFWStatus{Installed: true, Active: true, Status: "active"}}
	svc := newServerServiceForTest(store, exec, taskSvc)
	svc.SetAgentClient(agentClient)

	if _, err := svc.AllowUFW(context.Background(), "srv_1", UFWAllowRequest{Port: 443, Protocol: "tcp"}); err != nil {
		t.Fatal(err)
	}
	if len(exec.commands) != 0 {
		t.Fatalf("expected no SSH commands, got %#v", exec.commands)
	}
	if agentClient.ufwURL != "https://127.0.0.1:9786" || agentClient.allowedRule.Port != 443 {
		t.Fatalf("expected agent UFW write, got url=%q rule=%#v", agentClient.ufwURL, agentClient.allowedRule)
	}
}

func TestCheckConfiguredAgentsMarksIncompatibleVersion(t *testing.T) {
	svc, _, store := testServerService(t, nil)
	traits := `{"agent.enabled":"true","agent.url":"https://127.0.0.1:9786","agent.status":"compatible"}`
	if _, err := store.AppDB().Exec(`INSERT INTO servers(id,name,host,port,ssh_username,credential_id,traits,created_at,updated_at) VALUES('srv_agent','s','127.0.0.1',22,'du','cred_1',?,'2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`, traits); err != nil {
		t.Fatal(err)
	}
	health := agentHealth("0.9.0")
	svc.SetAgentClient(&serverFakeAgentClient{health: health})

	svc.CheckConfiguredAgents(context.Background())
	srv, err := svc.Get(context.Background(), "srv_agent")
	if err != nil {
		t.Fatal(err)
	}
	if srv.Traits[agentcontract.TraitStatus] != agentcontract.StatusIncompatible || srv.Traits[agentcontract.TraitVersion] != "0.9.0" || !strings.Contains(srv.Traits[agentcontract.TraitLastError], "agent version 0.9.0 does not match panel") {
		t.Fatalf("unexpected agent compatibility traits: %#v", srv.Traits)
	}
}

func TestCheckConfiguredAgentsMarksCompatibleWhenVersionMatches(t *testing.T) {
	svc, _, store := testServerService(t, nil)
	traits := `{"agent.enabled":"true","agent.url":"https://127.0.0.1:9786","agent.status":"compatible"}`
	if _, err := store.AppDB().Exec(`INSERT INTO servers(id,name,host,port,ssh_username,credential_id,traits,created_at,updated_at) VALUES('srv_agent','s','127.0.0.1',22,'du','cred_1',?,'2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`, traits); err != nil {
		t.Fatal(err)
	}
	health := agentHealth(agentcontract.Version)
	health.Capabilities = nil
	health.ContractHash = "different-contract-hash"
	health.Docker.Host = "unix:///var/run/other.sock"
	svc.SetAgentClient(&serverFakeAgentClient{health: health})

	svc.CheckConfiguredAgents(context.Background())
	srv, err := svc.Get(context.Background(), "srv_agent")
	if err != nil {
		t.Fatal(err)
	}
	if srv.Traits[agentcontract.TraitStatus] != agentcontract.StatusCompatible || srv.Traits[agentcontract.TraitVersion] != agentcontract.Version || srv.Traits[agentcontract.TraitLastError] != "" {
		t.Fatalf("unexpected agent compatibility traits: %#v", srv.Traits)
	}
}

func TestCheckAgentKeepsExpiringCertificateCompatible(t *testing.T) {
	svc, _, store := testServerService(t, nil)
	traits := `{"agent.enabled":"true","agent.url":"https://127.0.0.1:9786","agent.status":"compatible"}`
	if _, err := store.AppDB().Exec(`INSERT INTO servers(id,name,host,port,ssh_username,credential_id,traits,created_at,updated_at) VALUES('srv_agent','s','127.0.0.1',22,'du','cred_1',?,'2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`, traits); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	health := agentHealth(agentcontract.Version)
	health.Certificate = &agentcontract.CertificateInfo{
		Fingerprint: "ABC",
		CommonName:  "panel-agent-srv_agent",
		NotBefore:   now.Add(-time.Hour),
		NotAfter:    now.Add(agentCertificateRenewBefore / 2),
	}
	svc.SetAgentClient(&serverFakeAgentClient{health: health})
	srv, err := svc.Get(context.Background(), "srv_agent")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.checkAgent(context.Background(), srv); err != nil {
		t.Fatal(err)
	}
	updated, err := svc.Get(context.Background(), "srv_agent")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Traits[agentcontract.TraitStatus] != agentcontract.StatusCompatible || updated.Traits[agentcontract.TraitLastError] != "" {
		t.Fatalf("expiring certificate should remain compatible without warning, got %#v", updated.Traits)
	}
}

func TestSynchronousConnectivityDoesNotRedeployCompatibleAgent(t *testing.T) {
	createSvc, taskSvc, store := testServerService(t, nil)
	srv, err := createSvc.Create(context.Background(), SaveRequest{
		Name:         "s",
		IPv4:         "127.0.0.1",
		Port:         22,
		SSHUsername:  "du",
		CredentialID: "cred_1",
	})
	if err != nil {
		t.Fatal(err)
	}
	setServerTraits(t, store, srv.ID, map[string]string{
		agentcontract.TraitEnabled: "true",
		agentcontract.TraitURL:     "https://127.0.0.1:9786",
		agentcontract.TraitStatus:  agentcontract.StatusCompatible,
	})
	assets, err := agentsecurity.EnsureTLSAssets(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	svc := newServerServiceForTest(store, &connectivityFakeExec{passwordless: true}, taskSvc)
	svc.SetAgentTLSAssets(assets)
	svc.SetAgentClient(&serverFakeAgentClient{
		osRelease: linux.OSRelease{ID: "debian", VersionID: "13", PrettyName: "Debian GNU/Linux 13", Supported: true},
		health:    agentHealth(agentcontract.Version),
	})

	checked, err := svc.TestConnectivity(context.Background(), srv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !checked.Reachable {
		t.Fatalf("expected synchronous connectivity success, got %#v", checked)
	}
	result, err := taskSvc.List(context.Background(), tasks.ListFilter{Type: agentDeployTaskType, ServerID: srv.ID, IncludeInternal: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 0 {
		t.Fatalf("compatible agent should not be redeployed by connectivity checks, got %#v", result.Items)
	}
}

func TestCheckConfiguredAgentsDeploysNonDefaultAgentURL(t *testing.T) {
	_, taskSvc, store := testServerService(t, nil)
	traits := `{"agent.enabled":"true","agent.url":"https://127.0.0.1:9443","agent.status":"compatible"}`
	if _, err := store.AppDB().Exec(`INSERT INTO servers(id,name,host,port,ssh_username,credential_id,traits,created_at,updated_at) VALUES('srv_agent','s','127.0.0.1',22,'du','cred_1',?,'2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`, traits); err != nil {
		t.Fatal(err)
	}
	setServerArchitecture(t, store, "srv_agent")
	assets, err := agentsecurity.EnsureTLSAssets(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	svc := newServerServiceForTest(store, agentArchFakeExec{arch: "x86_64"}, taskSvc)
	svc.SetAgentTLSAssets(assets)
	svc.SetAgentClient(&serverFakeAgentClient{health: agentHealth(agentcontract.Version)})

	svc.CheckConfiguredAgents(context.Background())
	result, err := taskSvc.List(context.Background(), tasks.ListFilter{Type: agentDeployTaskType, ServerID: "srv_agent", IncludeInternal: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 1 || result.Items[0].TriggeredBy != "system" {
		t.Fatalf("expected non-default agent URL to trigger redeploy, got %#v", result.Items)
	}
	srv, err := svc.Get(context.Background(), "srv_agent")
	if err != nil {
		t.Fatal(err)
	}
	if srv.Traits[agentcontract.TraitStatus] != agentcontract.StatusIncompatible || !strings.Contains(srv.Traits[agentcontract.TraitLastError], "agent URL must be https://127.0.0.1:9786") {
		t.Fatalf("expected non-default URL to mark incompatible, got %#v", srv.Traits)
	}
}

func TestIssueAgentCertificateUsesCurrentServerHostURL(t *testing.T) {
	svc, _, store := testServerService(t, nil)
	assets, err := agentsecurity.EnsureTLSAssets(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	svc.SetAgentTLSAssets(assets)
	traits := `{"agent.enabled":"true","agent.url":"https://198.51.100.2:9786","agent.status":"compatible"}`
	if _, err := store.AppDB().Exec(`INSERT INTO servers(id,name,host,port,ssh_username,credential_id,traits,created_at,updated_at) VALUES('srv_agent','s','203.0.113.10',22,'du','cred_1',?,'2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`, traits); err != nil {
		t.Fatal(err)
	}

	bundle, err := svc.IssueAgentCertificate(context.Background(), "srv_agent")
	if err != nil {
		t.Fatal(err)
	}
	if bundle.AgentURL != "https://203.0.113.10:9786" {
		t.Fatalf("expected agent URL to follow current server host, got %q", bundle.AgentURL)
	}
	block, _ := pem.Decode([]byte(bundle.Certificate))
	if block == nil {
		t.Fatal("expected certificate PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if len(cert.IPAddresses) != 1 || cert.IPAddresses[0].String() != "203.0.113.10" {
		t.Fatalf("expected certificate SAN to contain current server host, got %#v", cert.IPAddresses)
	}
}

func TestUpdateHostRefreshesAgentURLAndInvalidatesCertificate(t *testing.T) {
	svc, _, store := testServerService(t, nil)
	traits := `{"agent.enabled":"true","agent.url":"https://198.51.100.2:9786","agent.status":"compatible","agent.certificate.fingerprint":"OLD","agent.certificate.not_after":"2027-01-01T00:00:00Z"}`
	if _, err := store.AppDB().Exec(`INSERT INTO servers(id,name,host,port,ssh_username,credential_id,traits,created_at,updated_at) VALUES('srv_agent','s','198.51.100.2',22,'du','cred_1',?,'2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`, traits); err != nil {
		t.Fatal(err)
	}

	updated, err := svc.Update(context.Background(), "srv_agent", SaveRequest{
		Name:         "s",
		IPv4:         "203.0.113.10",
		Port:         22,
		SSHUsername:  "du",
		CredentialID: "cred_1",
		DockerHost:   agentcontract.DefaultDockerHost,
		Traits:       map[string]string{"agent.enabled": "true", "agent.url": "https://198.51.100.2:9786", "agent.status": "compatible"},
		Variables:    map[string]string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Traits[agentcontract.TraitURL] != "https://203.0.113.10:9786" || updated.Traits[agentcontract.TraitStatus] != agentcontract.StatusIncompatible {
		t.Fatalf("expected host update to refresh agent traits, got %#v", updated.Traits)
	}
	if updated.Traits[agentcontract.TraitCertificateFingerprint] != "" || updated.Traits[agentcontract.TraitCertificateNotAfter] != "" {
		t.Fatalf("expected old certificate metadata to be cleared, got %#v", updated.Traits)
	}
}

func TestCheckConfiguredAgentsDeploysConfiguredIncompatibleAgent(t *testing.T) {
	_, taskSvc, store := testServerService(t, nil)
	traits := `{"agent.enabled":"true","agent.url":"https://127.0.0.1:9786","agent.status":"incompatible"}`
	if _, err := store.AppDB().Exec(`INSERT INTO servers(id,name,host,port,ssh_username,credential_id,traits,created_at,updated_at) VALUES('srv_agent','s','127.0.0.1',22,'du','cred_1',?,'2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`, traits); err != nil {
		t.Fatal(err)
	}
	setServerArchitecture(t, store, "srv_agent")
	assets, err := agentsecurity.EnsureTLSAssets(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	svc := newServerServiceForTest(store, agentArchFakeExec{arch: "x86_64"}, taskSvc)
	svc.SetAgentTLSAssets(assets)
	svc.SetAgentClient(&serverFakeAgentClient{health: agentHealth(agentcontract.Version)})

	svc.CheckConfiguredAgents(context.Background())
	result, err := taskSvc.List(context.Background(), tasks.ListFilter{Type: agentDeployTaskType, ServerID: "srv_agent", IncludeInternal: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 1 || result.Items[0].TriggeredBy != "system" {
		t.Fatalf("expected periodic check to deploy configured incompatible agent, got %#v", result.Items)
	}
}

func TestCheckConfiguredAgentsDeploysExpiredStoredAgentCertificate(t *testing.T) {
	_, taskSvc, store := testServerService(t, nil)
	expiredAt := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339Nano)
	traits := `{"agent.enabled":"true","agent.url":"https://127.0.0.1:9786","agent.status":"compatible","agent.certificate.not_after":"` + expiredAt + `"}`
	if _, err := store.AppDB().Exec(`INSERT INTO servers(id,name,host,port,ssh_username,credential_id,traits,created_at,updated_at) VALUES('srv_agent','s','127.0.0.1',22,'du','cred_1',?,'2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`, traits); err != nil {
		t.Fatal(err)
	}
	setServerArchitecture(t, store, "srv_agent")
	assets, err := agentsecurity.EnsureTLSAssets(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	svc := newServerServiceForTest(store, agentArchFakeExec{arch: "x86_64"}, taskSvc)
	svc.SetAgentTLSAssets(assets)
	svc.SetAgentClient(&serverFakeAgentClient{health: agentHealth(agentcontract.Version)})

	svc.CheckConfiguredAgents(context.Background())
	result, err := taskSvc.List(context.Background(), tasks.ListFilter{Type: agentDeployTaskType, ServerID: "srv_agent", IncludeInternal: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 1 || result.Items[0].TriggeredBy != "system" {
		t.Fatalf("expected periodic check to deploy expired configured agent certificate, got %#v", result.Items)
	}
}

func TestCheckConfiguredAgentsDeploysExpiringStoredAgentCertificate(t *testing.T) {
	_, taskSvc, store := testServerService(t, nil)
	expiringAt := time.Now().UTC().Add(agentCertificateRenewBefore / 2).Format(time.RFC3339Nano)
	traits := `{"agent.enabled":"true","agent.url":"https://127.0.0.1:9786","agent.status":"compatible","agent.certificate.not_after":"` + expiringAt + `"}`
	if _, err := store.AppDB().Exec(`INSERT INTO servers(id,name,host,port,ssh_username,credential_id,traits,created_at,updated_at) VALUES('srv_agent','s','127.0.0.1',22,'du','cred_1',?,'2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`, traits); err != nil {
		t.Fatal(err)
	}
	setServerArchitecture(t, store, "srv_agent")
	assets, err := agentsecurity.EnsureTLSAssets(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	svc := newServerServiceForTest(store, agentArchFakeExec{arch: "x86_64"}, taskSvc)
	svc.SetAgentTLSAssets(assets)
	svc.SetAgentClient(&serverFakeAgentClient{health: agentHealth(agentcontract.Version)})

	svc.CheckConfiguredAgents(context.Background())
	result, err := taskSvc.List(context.Background(), tasks.ListFilter{Type: agentDeployTaskType, ServerID: "srv_agent", IncludeInternal: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 1 || result.Items[0].TriggeredBy != "system" {
		t.Fatalf("expected periodic check to deploy expiring configured agent certificate, got %#v", result.Items)
	}
}

func TestAgentCertificateRenewalProblemKeepsFreshCertificateCompatible(t *testing.T) {
	now := time.Now().UTC()
	fresh := Server{Traits: map[string]string{
		agentcontract.TraitCertificateNotAfter: now.Add(agentsecurity.DefaultLeafValidity).Format(time.RFC3339Nano),
	}}
	if renewal, msg := agentCertificateRenewalProblem(fresh, now); renewal {
		t.Fatalf("fresh agent certificate should not require renewal: %s", msg)
	}

	expiring := Server{Traits: map[string]string{
		agentcontract.TraitCertificateNotAfter: now.Add(agentCertificateRenewBefore / 2).Format(time.RFC3339Nano),
	}}
	if renewal, msg := agentCertificateRenewalProblem(expiring, now); !renewal || msg != "" {
		t.Fatal("agent certificate inside renewal window should require renewal")
	}
}

func TestCheckConfiguredAgentsDoesNotDeployUnavailableNetworkError(t *testing.T) {
	_, taskSvc, store := testServerService(t, nil)
	lastErr := `rpc error: code = Unavailable desc = connection error: desc = "transport: Error while dialing: dial tcp 127.0.0.1:9786: i/o timeout"`
	traits := `{"agent.enabled":"true","agent.url":"https://127.0.0.1:9786","agent.status":"unavailable","agent.last_error":"` + strings.ReplaceAll(lastErr, `"`, `\"`) + `"}`
	if _, err := store.AppDB().Exec(`INSERT INTO servers(id,name,host,port,ssh_username,credential_id,traits,created_at,updated_at) VALUES('srv_agent','s','127.0.0.1',22,'du','cred_1',?,'2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`, traits); err != nil {
		t.Fatal(err)
	}
	assets, err := agentsecurity.EnsureTLSAssets(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	svc := newServerServiceForTest(store, agentArchFakeExec{arch: "x86_64"}, taskSvc)
	svc.SetAgentTLSAssets(assets)
	svc.SetAgentClient(&serverFakeAgentClient{err: errString(lastErr)})

	svc.CheckConfiguredAgents(context.Background())
	result, err := taskSvc.List(context.Background(), tasks.ListFilter{Type: agentDeployTaskType, ServerID: "srv_agent", IncludeInternal: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 0 {
		t.Fatalf("expected unavailable network error not to deploy agent, got %#v", result.Items)
	}
	srv, err := svc.Get(context.Background(), "srv_agent")
	if err != nil {
		t.Fatal(err)
	}
	if srv.Traits[agentcontract.TraitStatus] != agentcontract.StatusUnavailable || !strings.Contains(srv.Traits[agentcontract.TraitLastError], "i/o timeout") {
		t.Fatalf("expected unavailable network error to be recorded, got %#v", srv.Traits)
	}
}

func TestCheckConfiguredAgentsDoesNotDeployUndeployableAgent(t *testing.T) {
	_, taskSvc, store := testServerService(t, nil)
	traits := `{"agent.enabled":"true","agent.url":"https://127.0.0.1:9786","agent.status":"undeployable","agent.auto_deploy_blocked":"true"}`
	if _, err := store.AppDB().Exec(`INSERT INTO servers(id,name,host,port,ssh_username,credential_id,traits,created_at,updated_at) VALUES('srv_agent','s','127.0.0.1',22,'du','cred_1',?,'2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`, traits); err != nil {
		t.Fatal(err)
	}
	assets, err := agentsecurity.EnsureTLSAssets(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	svc := newServerServiceForTest(store, agentArchFakeExec{arch: "x86_64"}, taskSvc)
	svc.SetAgentTLSAssets(assets)
	svc.SetAgentClient(&serverFakeAgentClient{health: agentHealth(agentcontract.Version)})

	svc.CheckConfiguredAgents(context.Background())
	result, err := taskSvc.List(context.Background(), tasks.ListFilter{Type: agentDeployTaskType, ServerID: "srv_agent", IncludeInternal: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 0 {
		t.Fatalf("expected undeployable agent not to deploy automatically, got %#v", result.Items)
	}
}

func TestCheckConfiguredAgentsMarksCertificateTimeErrorIncompatible(t *testing.T) {
	svc, _, store := testServerService(t, nil)
	traits := `{"agent.enabled":"true","agent.url":"https://127.0.0.1:9786"}`
	if _, err := store.AppDB().Exec(`INSERT INTO servers(id,name,host,port,ssh_username,credential_id,traits,created_at,updated_at) VALUES('srv_agent','s','127.0.0.1',22,'du','cred_1',?,'2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`, traits); err != nil {
		t.Fatal(err)
	}
	certErr := x509.CertificateInvalidError{Reason: x509.Expired}
	svc.SetAgentClient(&serverFakeAgentClient{err: certErr})

	svc.CheckConfiguredAgents(context.Background())
	srv, err := svc.Get(context.Background(), "srv_agent")
	if err != nil {
		t.Fatal(err)
	}
	if srv.Traits[agentcontract.TraitStatus] != agentcontract.StatusIncompatible {
		t.Fatalf("expected certificate time error to mark agent incompatible, got %#v", srv.Traits)
	}
	if !strings.Contains(strings.ToLower(srv.Traits[agentcontract.TraitLastError]), "certificate") {
		t.Fatalf("expected certificate error to be recorded, got %#v", srv.Traits)
	}
}

func TestAgentCertificateTimeErrorDeploysWhenAlreadyIncompatible(t *testing.T) {
	createSvc, taskSvc, store := testServerService(t, nil)
	traits := map[string]string{
		agentcontract.TraitEnabled: "true",
		agentcontract.TraitURL:     "https://127.0.0.1:9786",
		agentcontract.TraitStatus:  agentcontract.StatusIncompatible,
	}
	srv, err := createSvc.Create(context.Background(), SaveRequest{Name: "s", IPv4: "127.0.0.1", Port: 22, SSHUsername: "du", CredentialID: "cred_1"})
	if err != nil {
		t.Fatal(err)
	}
	setServerTraits(t, store, srv.ID, traits)
	setServerArchitecture(t, store, srv.ID)
	assets, err := agentsecurity.EnsureTLSAssets(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	svc := newServerServiceForTest(store, agentArchFakeExec{arch: "x86_64"}, taskSvc)
	svc.SetAgentTLSAssets(assets)
	svc.SetAgentClient(&serverFakeAgentClient{health: agentHealth(agentcontract.Version)})

	if !svc.HandleAgentError(context.Background(), srv, x509.CertificateInvalidError{Reason: x509.Expired}) {
		t.Fatal("expected certificate time error to be handled")
	}
	result, err := taskSvc.List(context.Background(), tasks.ListFilter{Type: agentDeployTaskType, ServerID: srv.ID, IncludeInternal: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 1 || result.Items[0].TriggeredBy != "system" {
		t.Fatalf("expected handled certificate error to deploy incompatible agent, got %#v", result.Items)
	}
}

func TestSystemDetectionDeploysForIncompatibleAgent(t *testing.T) {
	_, taskSvc, store := testServerService(t, nil)
	traits := `{"agent.enabled":"true","agent.url":"https://127.0.0.1:9786","agent.status":"incompatible"}`
	if _, err := store.AppDB().Exec(`INSERT INTO servers(id,name,host,port,ssh_username,credential_id,traits,created_at,updated_at) VALUES('srv_agent','s','127.0.0.1',22,'du','cred_1',?,'2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`, traits); err != nil {
		t.Fatal(err)
	}
	setServerArchitecture(t, store, "srv_agent")
	assets, err := agentsecurity.EnsureTLSAssets(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	svc := newServerServiceForTest(store, agentArchFakeExec{arch: "x86_64"}, taskSvc)
	svc.SetAgentTLSAssets(assets)
	svc.SetAgentClient(&serverFakeAgentClient{health: agentHealth(agentcontract.Version)})
	srv, err := svc.Get(context.Background(), "srv_agent")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := svc.detectOS(context.Background(), srv, Target(srv)); err == nil {
		t.Fatal("expected incompatible agent error")
	}
	result, err := taskSvc.List(context.Background(), tasks.ListFilter{Type: agentDeployTaskType, ServerID: "srv_agent", IncludeInternal: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 1 || result.Items[0].TriggeredBy != "system" {
		t.Fatalf("expected system detection to deploy configured incompatible agent, got %#v", result.Items)
	}
}

func TestDeployAgentStartsExistingQueuedTask(t *testing.T) {
	createSvc, taskSvc, store := testServerService(t, nil)
	srv, err := createSvc.Create(context.Background(), SaveRequest{Name: "s", IPv4: "127.0.0.1", Port: 22, SSHUsername: "du", CredentialID: "cred_1"})
	if err != nil {
		t.Fatal(err)
	}
	setServerArchitecture(t, store, srv.ID)
	assets, err := agentsecurity.EnsureTLSAssets(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	svc := newServerServiceForTest(store, agentArchFakeExec{arch: "x86_64"}, taskSvc)
	svc.SetAgentTLSAssets(assets)
	svc.SetAgentClient(&serverFakeAgentClient{health: agentHealth(agentcontract.Version)})
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

func TestSystemAgentDeployRespectsAutoDeployBackoff(t *testing.T) {
	createSvc, taskSvc, store := testServerService(t, nil)
	traits := map[string]string{
		agentcontract.TraitEnabled:               "true",
		agentcontract.TraitURL:                   "https://127.0.0.1:9786",
		agentcontract.TraitStatus:                agentcontract.StatusIncompatible,
		agentcontract.TraitAutoDeployFailures:    "1",
		agentcontract.TraitAutoDeployLastFailure: time.Now().UTC().Format(time.RFC3339Nano),
	}
	srv, err := createSvc.Create(context.Background(), SaveRequest{Name: "s", IPv4: "127.0.0.1", Port: 22, SSHUsername: "du", CredentialID: "cred_1"})
	if err != nil {
		t.Fatal(err)
	}
	setServerTraits(t, store, srv.ID, traits)
	setServerArchitecture(t, store, srv.ID)
	assets, err := agentsecurity.EnsureTLSAssets(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	svc := newServerServiceForTest(store, agentArchFakeExec{arch: "x86_64"}, taskSvc)
	svc.SetAgentTLSAssets(assets)
	svc.SetAgentClient(&serverFakeAgentClient{health: agentHealth(agentcontract.Version)})

	svc.CheckConfiguredAgent(context.Background(), srv)

	result, err := taskSvc.List(context.Background(), tasks.ListFilter{Type: agentDeployTaskType, ServerID: srv.ID, IncludeInternal: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 0 {
		t.Fatalf("expected auto deploy to wait for backoff, got %#v", result.Items)
	}
}

func TestManualAgentDeployResetsAutoDeployBackoffTime(t *testing.T) {
	createSvc, taskSvc, store := testServerService(t, nil)
	oldFailureAt := time.Now().UTC().Add(-time.Hour)
	traits := map[string]string{
		agentcontract.TraitEnabled:               "true",
		agentcontract.TraitURL:                   "https://127.0.0.1:9786",
		agentcontract.TraitStatus:                agentcontract.StatusIncompatible,
		agentcontract.TraitAutoDeployFailures:    "1",
		agentcontract.TraitAutoDeployLastFailure: oldFailureAt.Format(time.RFC3339Nano),
	}
	srv, err := createSvc.Create(context.Background(), SaveRequest{Name: "s", IPv4: "127.0.0.1", Port: 22, SSHUsername: "du", CredentialID: "cred_1"})
	if err != nil {
		t.Fatal(err)
	}
	setServerTraits(t, store, srv.ID, traits)
	setServerArchitecture(t, store, srv.ID)
	assets, err := agentsecurity.EnsureTLSAssets(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	svc := newServerServiceForTest(store, agentArchFakeExec{arch: "x86_64"}, taskSvc)
	svc.SetAgentTLSAssets(assets)
	svc.SetAgentClient(&serverFakeAgentClient{health: agentHealth(agentcontract.Version)})
	before := time.Now().UTC()

	if _, err := svc.DeployAgent(context.Background(), srv.ID); err != nil {
		t.Fatal(err)
	}
	updated, err := svc.Get(context.Background(), srv.ID)
	if err != nil {
		t.Fatal(err)
	}
	resetAt, err := time.Parse(time.RFC3339Nano, updated.Traits[agentcontract.TraitAutoDeployLastFailure])
	if err != nil {
		t.Fatal(err)
	}
	if resetAt.Before(before) || !resetAt.After(oldFailureAt) {
		t.Fatalf("expected manual deploy to reset backoff time, old=%s reset=%s before=%s", oldFailureAt, resetAt, before)
	}
	if updated.Traits[agentcontract.TraitAutoDeployFailures] != "1" {
		t.Fatalf("expected manual deploy to preserve failure count until healthy checks clear it, got %#v", updated.Traits)
	}
}

func TestAgentHealthRequiresFiveSuccessesToClearAutoDeployFailures(t *testing.T) {
	svc, _, store := testServerService(t, nil)
	lastFailure := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339Nano)
	traits := `{"agent.enabled":"true","agent.url":"https://127.0.0.1:9786","agent.status":"compatible","agent.auto_deploy_failures":"1","agent.auto_deploy_last_failure_at":"` + lastFailure + `"}`
	if _, err := store.AppDB().Exec(`INSERT INTO servers(id,name,host,port,ssh_username,credential_id,traits,created_at,updated_at) VALUES('srv_agent','s','127.0.0.1',22,'du','cred_1',?,'2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`, traits); err != nil {
		t.Fatal(err)
	}
	svc.SetAgentClient(&serverFakeAgentClient{health: agentHealth(agentcontract.Version)})
	srv, err := svc.Get(context.Background(), "srv_agent")
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < agentAutoDeployHealthyChecksToReset-1; i++ {
		svc.CheckConfiguredAgent(context.Background(), srv)
		srv, err = svc.Get(context.Background(), "srv_agent")
		if err != nil {
			t.Fatal(err)
		}
		if srv.Traits[agentcontract.TraitAutoDeployFailures] == "" {
			t.Fatalf("auto deploy failures cleared after %d successful checks, want five", i+1)
		}
	}
	svc.CheckConfiguredAgent(context.Background(), srv)
	srv, err = svc.Get(context.Background(), "srv_agent")
	if err != nil {
		t.Fatal(err)
	}
	if srv.Traits[agentcontract.TraitAutoDeployFailures] != "" || srv.Traits[agentcontract.TraitAutoDeployLastFailure] != "" || srv.Traits[agentcontract.TraitHealthSuccessStreak] != "" {
		t.Fatalf("expected auto deploy retry traits to clear after five successful checks, got %#v", srv.Traits)
	}
}

func TestAgentCertificateTimeErrorMarksUndeployableAfterAutoDeployFailures(t *testing.T) {
	createSvc, taskSvc, store := testServerService(t, nil)
	traits := map[string]string{
		agentcontract.TraitEnabled:               "true",
		agentcontract.TraitURL:                   "https://127.0.0.1:9786",
		agentcontract.TraitAutoDeployFailures:    fmt.Sprintf("%d", agentAutoDeployMaxFailures),
		agentcontract.TraitAutoDeployLastFailure: time.Now().UTC().Add(-time.Hour).Format(time.RFC3339Nano),
	}
	srv, err := createSvc.Create(context.Background(), SaveRequest{Name: "s", IPv4: "127.0.0.1", Port: 22, SSHUsername: "du", CredentialID: "cred_1"})
	if err != nil {
		t.Fatal(err)
	}
	setServerTraits(t, store, srv.ID, traits)
	setServerArchitecture(t, store, srv.ID)
	assets, err := agentsecurity.EnsureTLSAssets(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	svc := newServerServiceForTest(store, agentArchFakeExec{arch: "x86_64"}, taskSvc)
	svc.SetAgentTLSAssets(assets)
	svc.SetAgentClient(&serverFakeAgentClient{health: agentHealth(agentcontract.Version)})

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
	if updated.Traits[agentcontract.TraitStatus] != agentcontract.StatusUndeployable {
		t.Fatalf("expected exhausted auto deploy to mark agent undeployable, got %#v", updated.Traits)
	}
	if !strings.Contains(strings.ToLower(updated.Traits[agentcontract.TraitLastError]), "stopped after") {
		t.Fatalf("expected exhausted auto deploy error to be retained, got %#v", updated.Traits)
	}
}

func TestSystemCertificatesIncludeAgentServerCertificates(t *testing.T) {
	svc, _, store := testServerService(t, nil)
	assets, err := agentsecurity.EnsureTLSAssets(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	svc.SetAgentTLSAssets(assets)
	traits := `{"agent.enabled":"true","agent.url":"https://127.0.0.1:9786","agent.status":"compatible","agent.certificate.fingerprint":"ABC","agent.certificate.not_before":"2025-01-01T00:00:00Z","agent.certificate.not_after":"2027-01-01T00:00:00Z"}`
	if _, err := store.AppDB().Exec(`INSERT INTO servers(id,name,host,port,ssh_username,credential_id,traits,created_at,updated_at) VALUES('srv_agent','s','127.0.0.1',22,'du','cred_1',?,'2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`, traits); err != nil {
		t.Fatal(err)
	}

	items, err := svc.SystemCertificates(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("expected Panel-side and server Agent certificates, got %#v", items)
	}
	if items[0].ID != "agent-ca" || !items[0].BuiltIn || !items[0].CanReset {
		t.Fatalf("unexpected agent CA item: %#v", items[0])
	}
	if items[1].ID != "agent-panel-client" || !items[1].BuiltIn || !items[1].CanReset {
		t.Fatalf("unexpected agent client item: %#v", items[1])
	}
	if items[2].ID != "agent-server:srv_agent" || items[2].ServerID != "srv_agent" || items[2].ServerName != "s" || items[2].Fingerprint != "ABC" || !items[2].BuiltIn || !items[2].CanReset {
		t.Fatalf("unexpected agent server certificate item: %#v", items[2])
	}
}

func TestSystemCertificatesSkipAgentServerWithoutCertificateMetadata(t *testing.T) {
	svc, _, store := testServerService(t, nil)
	assets, err := agentsecurity.EnsureTLSAssets(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	svc.SetAgentTLSAssets(assets)
	traits := `{"agent.enabled":"true","agent.url":"https://127.0.0.1:9786","agent.status":"compatible","agent.certificate.fingerprint":"ABC"}`
	if _, err := store.AppDB().Exec(`INSERT INTO servers(id,name,host,port,ssh_username,credential_id,traits,created_at,updated_at) VALUES('srv_agent','s','127.0.0.1',22,'du','cred_1',?,'2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`, traits); err != nil {
		t.Fatal(err)
	}

	items, err := svc.SystemCertificates(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("expected incomplete server certificate metadata to be skipped, got %#v", items)
	}
}

func TestResetPanelAgentClientCertificateReloadsGRPCClient(t *testing.T) {
	svc, taskSvc, _ := testServerService(t, nil)
	assets, err := agentsecurity.EnsureTLSAssets(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	client, err := agentclient.NewGRPCClient(assets, time.Second)
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
	cfg.LogDatabase = filepath.Join(dir, "log.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.AppDB().Exec(`INSERT INTO credentials(id,name,type,username,created_at,updated_at) VALUES('cred_1','c','password','du','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	traits := `{"agent.enabled":"true","agent.url":"https://127.0.0.1:9786","agent.status":"compatible"}`
	if _, err := store.AppDB().Exec(`INSERT INTO servers(id,name,host,port,ssh_username,credential_id,traits,os_id,os_version_id,os_supported,reachable,sudo_passwordless,privilege_mode,created_at,updated_at) VALUES('srv_1','s','127.0.0.1',22,'du','cred_1',?,'debian','13',1,1,1,'passwordless_sudo','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`, traits); err != nil {
		t.Fatal(err)
	}
	exec := &ufwManageFakeExec{}
	svc := newServerServiceForTest(store, exec, tasks.NewService(store.LogDB()))
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
	if srv.Traits[agentcontract.TraitStatus] != agentcontract.StatusIncompatible {
		t.Fatalf("expected certificate time error to mark agent incompatible, got %#v", srv.Traits)
	}
}

func TestAgentCertificateTimeErrorDetectedFromMessage(t *testing.T) {
	err := errString(`rpc error: code = Unavailable desc = connection error: desc = "transport: authentication handshake failed: tls: failed to verify certificate: x509: certificate has expired or is not yet valid"`)
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

func TestAgentInstallScriptStopsOldProcessesAndChecksPortOwner(t *testing.T) {
	script := agentInstallScript("/tmp/panel-agent-task")
	for _, want := range []string{
		"systemctl stop panel-agent.service",
		"pkill -x panel-agent",
		"pkill -f '^/usr/local/bin/panel-agent($| )'",
		"journalctl -u panel-agent.service -n 80 --no-pager",
		"ss -ltnp 'sport = :9786'",
		"for i in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15; do",
		"continuing with TLS certificate verification",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("expected install script to contain %q, got:\n%s", want, script)
		}
	}
}

func TestAgentSystemdUnitUsesSrvFlag(t *testing.T) {
	unit := agentSystemdUnit()
	if !strings.Contains(unit, "ExecStart=/usr/local/bin/panel-agent --srv") {
		t.Fatalf("expected systemd unit to start panel-agent with --srv, got:\n%s", unit)
	}
	if strings.Contains(unit, "ExecStart=/usr/local/bin/panel-agent\n") {
		t.Fatalf("systemd unit must not start panel-agent without --srv:\n%s", unit)
	}
}

func TestVerifyRemoteAgentCertificateFileComparesNewCertificateHash(t *testing.T) {
	certPEM := []byte("CERTIFICATE\n")
	exec := &captureSudoExec{}
	runner := remoteops.Runner{Exec: exec, Target: sshx.Target{}}

	if err := verifyRemoteAgentCertificateFile(context.Background(), runner, certPEM); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(certPEM)
	if !strings.Contains(exec.command, fmt.Sprintf("%x", sum[:])) || !strings.Contains(exec.command, agentRemoteConfigDir+"/server.pem") {
		t.Fatalf("expected certificate verification command to compare new cert hash, got:\n%s", exec.command)
	}
}

func TestAgentTargetPlatformDetectsMissingStructuredArchitecture(t *testing.T) {
	svc, _, store := testServerService(t, agentArchFakeExec{arch: "x86_64"})
	if _, err := store.AppDB().Exec(`INSERT INTO servers(id,name,host,port,ssh_username,credential_id,created_at,updated_at) VALUES('srv_arch','s','127.0.0.1',22,'du','cred_1','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	srv, err := svc.Get(context.Background(), "srv_arch")
	if err != nil {
		t.Fatal(err)
	}
	platform, err := svc.agentTargetPlatform(context.Background(), srv)
	if err != nil {
		t.Fatal(err)
	}
	if platform != "linux-amd64" {
		t.Fatalf("expected linux-amd64, got %q", platform)
	}
	updated, err := svc.Get(context.Background(), "srv_arch")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Architecture.OS != "linux" || updated.Architecture.Arch != "amd64" || updated.Architecture.RawMachine != "x86_64" {
		t.Fatalf("expected detected architecture to be persisted, got %#v", updated.Architecture)
	}

	platform, err = svc.agentTargetPlatform(context.Background(), Server{Architecture: ArchitectureInfo{OS: "linux", Arch: "amd64", RawMachine: "x86_64"}})
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
	srv, err := svc.Create(context.Background(), SaveRequest{Name: "s", IPv4: "127.0.0.1", Port: 22, SSHUsername: "du", CredentialID: "cred_1"})
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
	if task.Type != serverInfoTaskType || task.Summary != "Collecting initial server information" {
		t.Fatalf("unexpected initial task: %#v", task)
	}
	waitTaskFinished(t, taskSvc, task.ID)
}

func TestCreateServerInitialInfoFailureRollsBackServer(t *testing.T) {
	svc, taskSvc, _ := testServerService(t, failingConnectivityExec{})
	srv, err := svc.Create(context.Background(), SaveRequest{Name: "s", IPv4: "127.0.0.1", Port: 22, SSHUsername: "du", CredentialID: "cred_1"})
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

func TestCollectServerInfoInputsIncludesOnlyCompatibleAgents(t *testing.T) {
	svc, _, store := testServerService(t, nil)
	compatibleTraits := `{"agent.enabled":"true","agent.url":"https://127.0.0.1:9786","agent.status":"compatible"}`
	if _, err := store.AppDB().Exec(`INSERT INTO servers(id,name,host,port,ssh_username,credential_id,traits,created_at,updated_at) VALUES('srv_ready','ready','127.0.0.1',22,'du','cred_1',?,'2026-01-01T00:00:00Z','2026-01-01T00:00:00Z'),('srv_plain','plain','127.0.0.2',22,'du','cred_1','{}','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`, compatibleTraits); err != nil {
		t.Fatal(err)
	}
	batch, shouldRun, err := svc.CollectServerInfoInputs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !shouldRun || len(batch.Inputs) != 1 || batch.Inputs[0].ServerID != "srv_ready" || batch.Inputs[0].ParamsJSON != `{"bootstrap":false}` {
		t.Fatalf("unexpected server info batch: %#v", batch)
	}
}

func TestScheduledServerInfoFailureDoesNotRollbackServer(t *testing.T) {
	svc, taskSvc, store := testServerService(t, &connectivityFakeExec{})
	traits := `{"agent.enabled":"true","agent.url":"https://127.0.0.1:9786","agent.status":"compatible"}`
	if _, err := store.AppDB().Exec(`INSERT INTO servers(id,name,host,port,ssh_username,credential_id,traits,created_at,updated_at) VALUES('srv_refresh','refresh','127.0.0.1',22,'du','cred_1',?,'2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`, traits); err != nil {
		t.Fatal(err)
	}
	svc.SetAgentClient(&serverFakeAgentClient{err: errors.New("agent down")})
	task, err := svc.ensureServerInfoTask(context.Background(), "srv_refresh", true, "Collecting system information", "", false)
	if err != nil {
		t.Fatal(err)
	}
	finished := waitTaskTerminal(t, taskSvc, task.ID)
	if finished.Status != tasks.StatusFailedRetryable {
		t.Fatalf("expected retryable refresh failure, got %#v", finished)
	}
	if _, err := svc.Get(context.Background(), "srv_refresh"); err != nil {
		t.Fatalf("scheduled refresh must not delete server: %v", err)
	}
}

func TestListServersDoesNotCreateConnectivityTasks(t *testing.T) {
	createSvc, taskSvc, store := testServerService(t, nil)
	for _, name := range []string{"s1", "s2"} {
		if _, err := createSvc.Create(context.Background(), SaveRequest{Name: name, IPv4: "127.0.0.1", Port: 22, SSHUsername: "du", CredentialID: "cred_1"}); err != nil {
			t.Fatal(err)
		}
	}
	svc := newServerServiceForTest(store, &connectivityFakeExec{}, taskSvc)

	if _, err := svc.List(context.Background()); err != nil {
		t.Fatal(err)
	}

	result, err := taskSvc.List(context.Background(), tasks.ListFilter{Type: serverInfoTaskType, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 0 {
		t.Fatalf("server list must not create background tasks, got %#v", result.Items)
	}
}

func TestSynchronousConnectivityFailureMarksServerUnreachable(t *testing.T) {
	createSvc, taskSvc, store := testServerService(t, nil)
	srv, err := createSvc.Create(context.Background(), SaveRequest{Name: "s", IPv4: "127.0.0.1", Port: 22, SSHUsername: "du", CredentialID: "cred_1"})
	if err != nil {
		t.Fatal(err)
	}
	svc := newServerServiceForTest(store, failingConnectivityExec{}, taskSvc)
	if _, err := svc.TestConnectivity(context.Background(), srv.ID); err == nil {
		t.Fatal("expected connectivity failure")
	}
	checked, err := svc.Get(context.Background(), srv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if checked.Reachable {
		t.Fatalf("expected unreachable server, got %#v", checked)
	}
	result, err := taskSvc.List(context.Background(), tasks.ListFilter{ServerID: srv.ID, IncludeInternal: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 0 {
		t.Fatalf("connectivity function must not create tasks, got %#v", result.Items)
	}
}

type connectivityFakeExec struct {
	sudoTimeout  time.Duration
	root         bool
	passwordless bool
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
	if strings.TrimSpace(command.Command) == "uname -m" {
		return sshx.CommandResult{Stdout: "x86_64\n", ExitCode: 0}, nil
	}
	return sshx.CommandResult{ExitCode: 0}, nil
}

func (f *connectivityFakeExec) ExecSudo(ctx context.Context, target sshx.Target, command sshx.CommandSpec) (sshx.CommandResult, error) {
	f.sudoTimeout = command.Timeout
	if f.passwordless {
		return sshx.CommandResult{ExitCode: 0}, nil
	}
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
	if strings.TrimSpace(command.Command) == "uname -m" {
		return sshx.CommandResult{Stdout: "x86_64\n", ExitCode: 0}, nil
	}
	return sshx.CommandResult{ExitCode: 0}, nil
}

func (f *ufwInstallFakeExec) ExecSudo(ctx context.Context, target sshx.Target, command sshx.CommandSpec) (sshx.CommandResult, error) {
	if strings.TrimSpace(command.Command) == "true" {
		return sshx.CommandResult{ExitCode: 0}, nil
	}
	if strings.Contains(command.Command, "panel_ufw_installed") {
		installed := "false"
		if f.installed {
			installed = "true"
		}
		return sshx.CommandResult{Stdout: "panel_ufw_installed=" + installed + "\nStatus: inactive\npanel_ufw_numbered_begin\n", ExitCode: 0}, nil
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
	if strings.TrimSpace(command.Command) == "uname -m" {
		return sshx.CommandResult{Stdout: "x86_64\n", ExitCode: 0}, nil
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

type captureSudoExec struct {
	command string
}

func (f *captureSudoExec) Exec(context.Context, sshx.Target, sshx.CommandSpec) (sshx.CommandResult, error) {
	return sshx.CommandResult{ExitCode: 0}, nil
}

func (f *captureSudoExec) ExecSudo(_ context.Context, _ sshx.Target, command sshx.CommandSpec) (sshx.CommandResult, error) {
	f.command = command.Command
	return sshx.CommandResult{ExitCode: 0}, nil
}

func (f *captureSudoExec) Upload(context.Context, sshx.Target, sshx.UploadSpec) error {
	return nil
}

func (f *captureSudoExec) Download(context.Context, sshx.Target, sshx.DownloadSpec) error {
	return nil
}

type errString string

func (e errString) Error() string { return string(e) }

type serverFakeAgentClient struct {
	ufwURL              string
	ufw                 remoteops.UFWStatus
	health              agentcontract.HealthResponse
	osRelease           linux.OSRelease
	systemTraits        map[string]string
	err                 error
	allowedRule         remoteops.UFWRule
	capabilities        []string
	prepareRestartErr   error
	prepareRestartCalls int
}

func agentHealth(version string) agentcontract.HealthResponse {
	return agentcontract.HealthResponse{
		Status:       "ok",
		Version:      version,
		Capabilities: agentcontract.RequiredCapabilities,
		ContractHash: agentcontract.CurrentHash(),
		Docker:       agentcontract.DockerHealth{Host: agentcontract.DefaultDockerHost, Status: "ok"},
	}
}

func (f *serverFakeAgentClient) Health(context.Context, string) (agentcontract.HealthResponse, error) {
	if f.health.Version == "" {
		f.health = agentHealth(agentcontract.Version)
	}
	if f.capabilities != nil {
		f.health.Capabilities = f.capabilities
	}
	return f.health, f.err
}
func (f *serverFakeAgentClient) OSRelease(context.Context, string) (linux.OSRelease, error) {
	return f.osRelease, f.err
}
func (f *serverFakeAgentClient) SystemTraits(context.Context, string) (map[string]string, error) {
	return f.systemTraits, f.err
}
func (f *serverFakeAgentClient) MetricsSnapshot(context.Context, string, string) (linux.MetricsSnapshot, error) {
	return linux.MetricsSnapshot{}, f.err
}
func (f *serverFakeAgentClient) UFWStatus(_ context.Context, url string) (remoteops.UFWStatus, error) {
	f.ufwURL = url
	return f.ufw, f.err
}
func (f *serverFakeAgentClient) PackageUpdates(context.Context, string) ([]linux.PackageUpdate, error) {
	return nil, f.err
}
func (f *serverFakeAgentClient) UpgradePackages(context.Context, string, agentcontract.PackageUpgradeRequest) (agentcontract.CommandResponse, error) {
	return agentcontract.CommandResponse{}, f.err
}
func (f *serverFakeAgentClient) UFWInstall(_ context.Context, url string, _ agentcontract.UFWInstallRequest) (remoteops.UFWStatus, error) {
	f.ufwURL = url
	return f.ufw, f.err
}
func (f *serverFakeAgentClient) UFWEnable(_ context.Context, url string, _ agentcontract.UFWEnableRequest) (remoteops.UFWStatus, error) {
	f.ufwURL = url
	return f.ufw, f.err
}
func (f *serverFakeAgentClient) UFWAllow(_ context.Context, url string, req agentcontract.UFWAllowRequest) (remoteops.UFWStatus, error) {
	f.ufwURL = url
	f.allowedRule = req.Rule
	return f.ufw, f.err
}
func (f *serverFakeAgentClient) UFWDelete(_ context.Context, url string, _ agentcontract.UFWDeleteRequest) (remoteops.UFWStatus, error) {
	f.ufwURL = url
	return f.ufw, f.err
}

func (f *serverFakeAgentClient) Fail2BanStatus(context.Context, string) (agentcontract.Fail2BanStatusResponse, error) {
	return agentcontract.Fail2BanStatusResponse{Installed: true, Active: true, Jails: []string{"sshd"}}, f.err
}

func (f *serverFakeAgentClient) ApplyFail2Ban(context.Context, string, agentcontract.Fail2BanApplyRequest) (agentcontract.Fail2BanStatusResponse, error) {
	return agentcontract.Fail2BanStatusResponse{Installed: true, Active: true, Jails: []string{"sshd"}}, f.err
}
func (f *serverFakeAgentClient) ReleaseFail2Ban(context.Context, string) (agentcontract.Fail2BanStatusResponse, error) {
	return agentcontract.Fail2BanStatusResponse{Installed: true, Active: true, PanelConfigPresent: false, Jails: []string{}}, f.err
}
func (f *serverFakeAgentClient) RestartSystem(context.Context, string) error {
	return f.err
}

func (f *serverFakeAgentClient) PrepareRestart(context.Context, string) error {
	f.prepareRestartCalls++
	return f.prepareRestartErr
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
	cfg.LogDatabase = filepath.Join(dir, "log.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if _, err := store.AppDB().Exec(`INSERT INTO credentials(id,name,type,username,created_at,updated_at) VALUES('cred_1','c','password','du','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	taskSvc := tasks.NewService(store.LogDB())
	return newServerServiceForTest(store, exec, taskSvc), taskSvc, store
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
		if task.Status == tasks.StatusCompleted || task.Status == tasks.StatusFailed || task.Status == tasks.StatusFailedRetryable || task.Status == tasks.StatusBlocked || task.Status == tasks.StatusCancelled {
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
		if srv.Reachable && srv.OS.Supported && hasPrivilege(srv) {
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

func waitDeployTaskTerminal(t *testing.T, taskSvc *tasks.Service, taskID string) tasks.Task {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		task, err := taskSvc.Get(context.Background(), taskID)
		if err != nil {
			t.Fatal(err)
		}
		if task.Status == tasks.StatusCompleted || task.Status == tasks.StatusFailed || task.Status == tasks.StatusFailedRetryable || task.Status == tasks.StatusBlocked || task.Status == tasks.StatusCancelled {
			return task
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("deploy task did not finish")
	return tasks.Task{}
}
func agentHealthWithPrepareRestart(version string) agentcontract.HealthResponse {
	health := agentHealth(version)
	health.Capabilities = append(append([]string(nil), agentcontract.RequiredCapabilities...), agentcontract.CapabilityPrepareRestart)
	return health
}

type trackingAgentDeployExec struct {
	agentArchFakeExec
	uploads int
}

func (f *trackingAgentDeployExec) Upload(ctx context.Context, target sshx.Target, spec sshx.UploadSpec) error {
	f.uploads++
	return nil
}

func withAgentBundleRoot(t *testing.T, root string) {
	t.Helper()
	old := agentBundleRoot
	agentBundleRoot = root
	t.Cleanup(func() { agentBundleRoot = old })
}

func newDeployTestService(t *testing.T, traits map[string]string) (*Service, *tasks.Service, string, *trackingAgentDeployExec, *serverFakeAgentClient) {
	t.Helper()
	createSvc, taskSvc, store := testServerService(t, nil)
	srv, err := createSvc.Create(context.Background(), SaveRequest{Name: "s", IPv4: "127.0.0.1", Port: 22, SSHUsername: "du", CredentialID: "cred_1"})
	if err != nil {
		t.Fatal(err)
	}
	setServerTraits(t, store, srv.ID, traits)
	setServerArchitecture(t, store, srv.ID)
	assets, err := agentsecurity.EnsureTLSAssets(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	exec := &trackingAgentDeployExec{agentArchFakeExec: agentArchFakeExec{arch: "x86_64"}}
	svc := newServerServiceForTest(store, exec, taskSvc)
	svc.SetAgentTLSAssets(assets)
	agent := &serverFakeAgentClient{}
	svc.SetAgentClient(agent)
	return svc, taskSvc, srv.ID, exec, agent
}

func writeAgentBundle(t *testing.T) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "linux-amd64"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "linux-amd64", "panel-agent"), []byte("test binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	withAgentBundleRoot(t, root)
}

func TestAgentDeployVersionMismatchWaitsForReadyBeforeUpgrade(t *testing.T) {
	traits := map[string]string{
		agentcontract.TraitEnabled: "true",
		agentcontract.TraitURL:     "https://127.0.0.1:9786",
		agentcontract.TraitStatus:  agentcontract.StatusIncompatible,
		agentcontract.TraitVersion: "0.1.0",
	}
	svc, taskSvc, serverID, exec, agent := newDeployTestService(t, traits)
	agent.health = agentHealthWithPrepareRestart(agentcontract.Version)
	writeAgentBundle(t)

	task, err := svc.DeployAgent(context.Background(), serverID)
	if err != nil {
		t.Fatal(err)
	}
	finished := waitDeployTaskTerminal(t, taskSvc, task.ID)
	if finished.Status != tasks.StatusCompleted {
		t.Fatalf("expected completed deploy, got %#v", finished)
	}
	if agent.prepareRestartCalls != 1 {
		t.Fatalf("expected restart readiness check for version mismatch, got %d", agent.prepareRestartCalls)
	}
	if exec.uploads != 1 {
		t.Fatalf("expected binary upload for version mismatch, got %d", exec.uploads)
	}
}

func TestAgentDeployRestartOnlyDoesNotUploadBinary(t *testing.T) {
	traits := map[string]string{
		agentcontract.TraitEnabled: "true",
		agentcontract.TraitURL:     "https://127.0.0.1:9786",
		agentcontract.TraitStatus:  agentcontract.StatusIncompatible,
		agentcontract.TraitVersion: agentcontract.Version,
	}
	svc, taskSvc, serverID, exec, agent := newDeployTestService(t, traits)
	agent.health = agentHealthWithPrepareRestart(agentcontract.Version)

	task, err := svc.DeployAgent(context.Background(), serverID)
	if err != nil {
		t.Fatal(err)
	}
	finished := waitDeployTaskTerminal(t, taskSvc, task.ID)
	if finished.Status != tasks.StatusCompleted {
		t.Fatalf("expected completed restart-only deploy, got %#v", finished)
	}
	if agent.prepareRestartCalls != 1 {
		t.Fatalf("expected restart readiness check before restart, got %d", agent.prepareRestartCalls)
	}
	if exec.uploads != 0 {
		t.Fatalf("expected no binary upload for restart-only deploy, got %d", exec.uploads)
	}
}

func TestAgentDeploySkipsReadinessCheckForOldAgent(t *testing.T) {
	traits := map[string]string{
		agentcontract.TraitEnabled: "true",
		agentcontract.TraitURL:     "https://127.0.0.1:9786",
		agentcontract.TraitStatus:  agentcontract.StatusIncompatible,
		agentcontract.TraitVersion: "0.1.0",
	}
	svc, taskSvc, serverID, exec, agent := newDeployTestService(t, traits)
	// Old agent health advertises only the legacy capabilities, so the
	// readiness RPC must not be attempted and the deploy proceeds directly.
	agent.health = agentHealth(agentcontract.Version)
	writeAgentBundle(t)

	task, err := svc.DeployAgent(context.Background(), serverID)
	if err != nil {
		t.Fatal(err)
	}
	finished := waitDeployTaskTerminal(t, taskSvc, task.ID)
	if finished.Status != tasks.StatusCompleted {
		t.Fatalf("expected completed deploy, got %#v", finished)
	}
	if agent.prepareRestartCalls != 0 {
		t.Fatalf("expected no readiness check for old agent, got %d", agent.prepareRestartCalls)
	}
	if exec.uploads != 1 {
		t.Fatalf("expected binary upload for version mismatch, got %d", exec.uploads)
	}
}

func TestAgentDeployProceedsWhenReadinessCheckFails(t *testing.T) {
	traits := map[string]string{
		agentcontract.TraitEnabled: "true",
		agentcontract.TraitURL:     "https://127.0.0.1:9786",
		agentcontract.TraitStatus:  agentcontract.StatusIncompatible,
		agentcontract.TraitVersion: "0.1.0",
	}
	svc, taskSvc, serverID, exec, agent := newDeployTestService(t, traits)
	agent.health = agentHealthWithPrepareRestart(agentcontract.Version)
	agent.prepareRestartErr = errString("readiness stream failed")
	writeAgentBundle(t)

	task, err := svc.DeployAgent(context.Background(), serverID)
	if err != nil {
		t.Fatal(err)
	}
	finished := waitDeployTaskTerminal(t, taskSvc, task.ID)
	if finished.Status != tasks.StatusCompleted {
		t.Fatalf("expected deploy to proceed after readiness check failure, got %#v", finished)
	}
	if agent.prepareRestartCalls != 1 {
		t.Fatalf("expected readiness check to be attempted, got %d", agent.prepareRestartCalls)
	}
	if exec.uploads != 1 {
		t.Fatalf("expected binary upload after readiness failure, got %d", exec.uploads)
	}
}

func TestAgentNeedsBinaryUpgrade(t *testing.T) {
	base := map[string]string{
		agentcontract.TraitEnabled: "true",
		agentcontract.TraitURL:     "https://127.0.0.1:9786",
		agentcontract.TraitStatus:  agentcontract.StatusIncompatible,
		agentcontract.TraitVersion: agentcontract.Version,
	}
	serverWith := func(traits map[string]string) Server {
		return Server{Host: "127.0.0.1", Traits: traits}
	}
	if agentNeedsBinaryUpgrade(serverWith(base)) {
		t.Fatal("expected matching configured agent to use restart-only path")
	}
	mismatch := serverWith(cloneTraits(base))
	mismatch.Traits[agentcontract.TraitVersion] = "0.1.0"
	if !agentNeedsBinaryUpgrade(mismatch) {
		t.Fatal("expected version mismatch to require binary upgrade")
	}
	noURL := serverWith(map[string]string{agentcontract.TraitEnabled: "true"})
	if !agentNeedsBinaryUpgrade(noURL) {
		t.Fatal("expected unconfigured agent to require full install")
	}
	unhealthy := serverWith(cloneTraits(base))
	unhealthy.Traits[agentcontract.TraitStatus] = agentcontract.StatusUnavailable
	if !agentNeedsBinaryUpgrade(unhealthy) {
		t.Fatal("expected unavailable agent to require full install")
	}
	noVersion := serverWith(cloneTraits(base))
	delete(noVersion.Traits, agentcontract.TraitVersion)
	if !agentNeedsBinaryUpgrade(noVersion) {
		t.Fatal("expected missing version to require full install")
	}
}

func cloneTraits(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func TestPrepareAgentRestartGatesOnCapability(t *testing.T) {
	svc, _, serverID, _, _ := newDeployTestService(t, map[string]string{
		agentcontract.TraitEnabled: "true",
		agentcontract.TraitURL:     "https://127.0.0.1:9786",
		agentcontract.TraitStatus:  agentcontract.StatusIncompatible,
		agentcontract.TraitVersion: agentcontract.Version,
	})
	srv, err := svc.Get(context.Background(), serverID)
	if err != nil {
		t.Fatal(err)
	}
	svc.SetAgentClient(&serverFakeAgentClient{health: agentHealth(agentcontract.Version)})
	if err := svc.prepareAgentRestart(context.Background(), srv); err != nil {
		t.Fatalf("expected old agent without capability to skip readiness check: %v", err)
	}
	agent := &serverFakeAgentClient{health: agentHealthWithPrepareRestart(agentcontract.Version)}
	svc.SetAgentClient(agent)
	if err := svc.prepareAgentRestart(context.Background(), srv); err != nil {
		t.Fatalf("expected readiness check to pass: %v", err)
	}
	if agent.prepareRestartCalls != 1 {
		t.Fatalf("expected readiness RPC to be called, got %d", agent.prepareRestartCalls)
	}
	agent.prepareRestartErr = errString("readiness failed")
	if err := svc.prepareAgentRestart(context.Background(), srv); err == nil {
		t.Fatal("expected readiness failure to propagate")
	}
}

func TestUpdatePersistsWhenConnectivityProbeFails(t *testing.T) {
	createSvc, taskSvc, store := testServerService(t, nil)
	srv, err := createSvc.Create(context.Background(), SaveRequest{
		Name: "s", IPv4: "127.0.0.1", Port: 22, SSHUsername: "du", CredentialID: "cred_1", DockerHost: agentcontract.DefaultDockerHost,
	})
	if err != nil {
		t.Fatal(err)
	}
	svc := newServerServiceForTest(store, failingConnectivityExec{}, taskSvc)
	var synced []string
	svc.SetDNSSyncTrigger(func(_ context.Context, ids []string) error {
		synced = append(synced, ids...)
		return nil
	})

	updated, err := svc.Update(context.Background(), srv.ID, SaveRequest{
		Name: "renamed", IPv4: "203.0.113.5", Port: 22, SSHUsername: "du", CredentialID: "cred_1", DockerHost: agentcontract.DefaultDockerHost,
	})
	if err != nil {
		t.Fatalf("update must succeed even when the connectivity probe fails: %v", err)
	}
	if updated.Name != "renamed" || updated.Host != "203.0.113.5" {
		t.Fatalf("updated server = %#v", updated)
	}
	got, err := svc.Get(context.Background(), srv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Host != "203.0.113.5" || got.Name != "renamed" {
		t.Fatalf("persisted server = %#v", got)
	}
	if got.Reachable {
		t.Fatal("expected failed probe to mark the server unreachable")
	}
	if len(synced) != 1 || synced[0] != srv.ID {
		t.Fatalf("dns sync ids = %#v, want server id", synced)
	}
}

func TestHostKeyMismatchFlagOnReads(t *testing.T) {
	svc, _, store := testServerService(t, nil)
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	mismatch := "ssh host key mismatch: host key for 203.0.113.7:22 has changed (stored SHA256:AAA, presented SHA256:BBB)"
	rows := []struct {
		id        string
		lastError string
	}{
		{id: "srv_mismatch", lastError: mismatch},
		{id: "srv_other", lastError: "SSH authentication failed"},
		{id: "srv_clean", lastError: ""},
	}
	for _, row := range rows {
		if _, err := store.AppDB().Exec(`INSERT INTO servers(id,name,host,port,ssh_username,credential_id,traits,last_error,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
			row.id, row.id, "203.0.113.7", 22, "du", "cred_1", "{}", row.lastError, now, now); err != nil {
			t.Fatal(err)
		}
	}

	got, err := svc.Get(ctx, "srv_mismatch")
	if err != nil {
		t.Fatal(err)
	}
	if !got.HostKeyMismatch {
		t.Fatalf("Get: expected hostKeyMismatch for mismatch error, got %#v", got)
	}
	other, err := svc.Get(ctx, "srv_other")
	if err != nil {
		t.Fatal(err)
	}
	if other.HostKeyMismatch {
		t.Fatalf("Get: unexpected hostKeyMismatch for %q", other.LastError)
	}
	clean, err := svc.Get(ctx, "srv_clean")
	if err != nil {
		t.Fatal(err)
	}
	if clean.HostKeyMismatch {
		t.Fatalf("Get: unexpected hostKeyMismatch for empty lastError")
	}

	list, err := svc.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	assertMismatchFlag(t, list, "srv_mismatch", true)

	summaries, err := svc.ListSummaries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	assertSummaryMismatchFlag(t, summaries, "srv_mismatch", true)

	page, err := svc.ListSummaryPage(ctx, 1, 50, "")
	if err != nil {
		t.Fatal(err)
	}
	assertSummaryMismatchFlag(t, page.Items, "srv_mismatch", true)
}

func assertMismatchFlag(t *testing.T, servers []Server, id string, want bool) {
	t.Helper()
	for _, srv := range servers {
		if srv.ID == id {
			if srv.HostKeyMismatch != want {
				t.Fatalf("List: server %s hostKeyMismatch = %v, want %v", id, srv.HostKeyMismatch, want)
			}
			return
		}
	}
	t.Fatalf("List: server %s not found", id)
}

func assertSummaryMismatchFlag(t *testing.T, servers []ServerSummary, id string, want bool) {
	t.Helper()
	for _, srv := range servers {
		if srv.ID == id {
			if srv.HostKeyMismatch != want {
				t.Fatalf("summary server %s hostKeyMismatch = %v, want %v", id, srv.HostKeyMismatch, want)
			}
			return
		}
	}
	t.Fatalf("summary server %s not found", id)
}

type trustHostKeyFakeExec struct {
	*connectivityFakeExec
	trustErr error
	trusted  bool
	targets  []sshx.Target
}

func (f *trustHostKeyFakeExec) TrustHostKey(_ context.Context, target sshx.Target) error {
	f.trusted = true
	f.targets = append(f.targets, target)
	return f.trustErr
}

func insertServerWithError(t *testing.T, store *storage.Store, id, lastError string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := store.AppDB().Exec(`INSERT INTO servers(id,name,host,port,ssh_username,credential_id,traits,last_error,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		id, id, "203.0.113.7", 22, "du", "cred_1", "{}", lastError, now, now); err != nil {
		t.Fatal(err)
	}
}

func TestTrustHostKeyReplacesKeyAndRefreshesConnectivity(t *testing.T) {
	exec := &trustHostKeyFakeExec{connectivityFakeExec: &connectivityFakeExec{}}
	svc, _, store := testServerService(t, exec)
	ctx := context.Background()
	insertServerWithError(t, store, "srv_trust", "ssh host key mismatch: host key for 203.0.113.7:22 has changed (stored SHA256:AAA, presented SHA256:BBB)")

	got, err := svc.TrustHostKey(ctx, "srv_trust")
	if err != nil {
		t.Fatalf("trust host key: %v", err)
	}
	if !exec.trusted || len(exec.targets) != 1 {
		t.Fatalf("expected TrustHostKey call, trusted=%v targets=%#v", exec.trusted, exec.targets)
	}
	target := exec.targets[0]
	if target.Host != "203.0.113.7" || target.Port != 22 || target.CredentialID != "cred_1" || target.Username != "du" {
		t.Fatalf("unexpected trust target: %#v", target)
	}
	if !got.Reachable || got.LastError != "" || got.HostKeyMismatch {
		t.Fatalf("expected refreshed reachable server without mismatch, got %#v", got)
	}
}

func TestTrustHostKeyPropagatesExecutorError(t *testing.T) {
	exec := &trustHostKeyFakeExec{
		connectivityFakeExec: &connectivityFakeExec{},
		trustErr:             panelerr.BadGateway("ssh_auth_failed", "SSH authentication failed"),
	}
	svc, _, store := testServerService(t, exec)
	ctx := context.Background()
	insertServerWithError(t, store, "srv_trust", "ssh host key mismatch: host key for 203.0.113.7:22 has changed (stored SHA256:AAA, presented SHA256:BBB)")

	_, err := svc.TrustHostKey(ctx, "srv_trust")
	var perr *panelerr.Error
	if !errors.As(err, &perr) || perr.Code != "ssh_auth_failed" {
		t.Fatalf("expected ssh_auth_failed, got %v", err)
	}
	if !exec.trusted {
		t.Fatal("expected executor trust call")
	}
	var lastError string
	if err := store.AppDB().QueryRow(`SELECT last_error FROM servers WHERE id=?`, "srv_trust").Scan(&lastError); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(lastError, "ssh host key mismatch") {
		t.Fatalf("expected mismatch error to remain untouched after trust failure, got %q", lastError)
	}
}

func TestTrustHostKeyRequiresExecutorCapability(t *testing.T) {
	svc, _, store := testServerService(t, &connectivityFakeExec{})
	ctx := context.Background()
	insertServerWithError(t, store, "srv_trust", "ssh host key mismatch: host key for 203.0.113.7:22 has changed (stored SHA256:AAA, presented SHA256:BBB)")

	_, err := svc.TrustHostKey(ctx, "srv_trust")
	var perr *panelerr.Error
	if !errors.As(err, &perr) || perr.Code != "host_key_trust_unavailable" {
		t.Fatalf("expected host_key_trust_unavailable, got %v", err)
	}
}

func TestTrustHostKeyUnavailableWithoutExecutor(t *testing.T) {
	svc, _, _ := testServerService(t, nil)
	_, err := svc.TrustHostKey(context.Background(), "srv_missing")
	var perr *panelerr.Error
	if !errors.As(err, &perr) || perr.Code != "server_executor_unavailable" {
		t.Fatalf("expected server_executor_unavailable, got %v", err)
	}
}
