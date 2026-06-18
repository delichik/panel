package metrics

import (
	"context"
	"crypto/x509"
	"errors"
	"path/filepath"
	"testing"
	"time"

	agentcontract "panel/internal/agent/contract"
	"panel/internal/modules/servers"
	"panel/internal/modules/tasks"
	"panel/internal/platform/config"
	storage "panel/internal/platform/database"
	"panel/internal/platform/linux"
	"panel/internal/platform/linux/remoteops"
	"panel/internal/platform/ssh"
)

func TestMetricsSaveQueryCleanup(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.TaskDatabase = filepath.Join(dir, "tasks.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	svc := NewService(store.MetricsDB(), nil, nil)
	ctx := context.Background()
	base := time.Now().UTC().Add(-time.Minute)
	sampledAt := time.Date(base.Year(), base.Month(), base.Day(), base.Hour(), base.Minute(), base.Second(), 345678901, time.UTC)
	if err := svc.Save(ctx, linux.MetricsSnapshot{ServerID: "srv", Time: sampledAt, CPUUsagePercent: 50, MemoryUsedBytes: 1, MemoryTotalBytes: 2, DiskUsedBytes: 3, DiskTotalBytes: 4}); err != nil {
		t.Fatal(err)
	}
	series, err := svc.Query(ctx, "srv", "1h")
	if err != nil {
		t.Fatal(err)
	}
	if len(series.CPU) != 1 || series.CPU[0].UsagePercent != 50 {
		t.Fatalf("unexpected series: %#v", series)
	}
	if want := sampledAt.UTC().Truncate(time.Second); !series.CPU[0].Time.Equal(want) {
		t.Fatalf("expected timestamp aligned to %s, got %s", want, series.CPU[0].Time)
	}
	if _, err := svc.Query(ctx, "srv", "7d"); err != nil {
		t.Fatalf("expected 7d range to be accepted: %v", err)
	}
	if err := svc.Save(ctx, linux.MetricsSnapshot{ServerID: "srv", Time: time.Now().UTC().Add(-48 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	deleted, err := svc.Cleanup(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1 {
		t.Fatalf("expected one expired row removed, got %d", deleted)
	}
}

func TestCollectRequiresAgent(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.TaskDatabase = filepath.Join(dir, "tasks.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, err = store.AppDB().Exec(`INSERT INTO credentials(id,name,type,username,created_at,updated_at) VALUES('cred','c','password','du','now','now')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.AppDB().Exec(`INSERT INTO servers(id,name,host,port,ssh_username,credential_id,os_id,os_version_id,os_pretty_name,os_supported,created_at,updated_at) VALUES('srv','s','h',22,'du','cred','ubuntu','24.04','Ubuntu 24.04 LTS',1,'now','now')`)
	if err != nil {
		t.Fatal(err)
	}
	exec := &collectMetricsExecutor{stdout: "bad"}
	serverSvc := server.NewService(store.AppDB(), nil, tasks.NewService(store.TaskDB()))
	svc := NewService(store.MetricsDB(), serverSvc, exec)

	if err := svc.CollectAt(context.Background(), "srv", time.Now().UTC()); err == nil {
		t.Fatal("expected agent-required metrics failure")
	}
	if exec.command != "" {
		t.Fatalf("expected no SSH metrics fallback, got %q", exec.command)
	}
}

func TestCollectUsesAgentWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.TaskDatabase = filepath.Join(dir, "tasks.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, err = store.AppDB().Exec(`INSERT INTO credentials(id,name,type,username,created_at,updated_at) VALUES('cred','c','password','du','now','now')`)
	if err != nil {
		t.Fatal(err)
	}
	traits := `{"agent.enabled":"true","agent.url":"https://127.0.0.1:9443","agent.status":"compatible"}`
	_, err = store.AppDB().Exec(`INSERT INTO servers(id,name,host,port,ssh_username,credential_id,traits,os_id,os_version_id,os_pretty_name,os_supported,created_at,updated_at) VALUES('srv','s','h',22,'du','cred',?,'debian','13','Debian GNU/Linux 13',1,'now','now')`, traits)
	if err != nil {
		t.Fatal(err)
	}
	exec := &collectMetricsExecutor{stdout: "bad"}
	serverSvc := server.NewService(store.AppDB(), nil, tasks.NewService(store.TaskDB()))
	agentClient := &fakeAgentClient{snapshot: linux.MetricsSnapshot{CPUUsagePercent: 12, MemoryTotalBytes: 100, Status: linux.SystemStatus{Hostname: "agent-host"}}}
	svc := NewService(store.MetricsDB(), serverSvc, exec)
	svc.SetAgentClient(agentClient)

	if err := svc.CollectAt(context.Background(), "srv", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if agentClient.metricsURL != "https://127.0.0.1:9443" || exec.command != "" {
		t.Fatalf("expected agent metrics without SSH fallback, agent=%q ssh=%q", agentClient.metricsURL, exec.command)
	}
	series, err := svc.Query(context.Background(), "srv", "1h")
	if err != nil {
		t.Fatal(err)
	}
	if len(series.CPU) != 1 || series.CPU[0].UsagePercent != 12 {
		t.Fatalf("unexpected agent-collected series: %#v", series)
	}
}

func TestCollectFailsWhenConfiguredAgentFails(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.TaskDatabase = filepath.Join(dir, "tasks.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, err = store.AppDB().Exec(`INSERT INTO credentials(id,name,type,username,created_at,updated_at) VALUES('cred','c','password','du','now','now')`)
	if err != nil {
		t.Fatal(err)
	}
	traits := `{"agent.enabled":"true","agent.url":"https://127.0.0.1:9443","agent.status":"compatible"}`
	_, err = store.AppDB().Exec(`INSERT INTO servers(id,name,host,port,ssh_username,credential_id,traits,os_id,os_version_id,os_pretty_name,os_supported,created_at,updated_at) VALUES('srv','s','h',22,'du','cred',?,'debian','13','Debian GNU/Linux 13',1,'now','now')`, traits)
	if err != nil {
		t.Fatal(err)
	}
	exec := &collectMetricsExecutor{stdout: "100 40\n8000 2000\n100000 50000\n1000000000 10 20\n2000000000 20 30\nhost\nkernel\nDebian\n123\n0.1 0.2 0.3"}
	serverSvc := server.NewService(store.AppDB(), nil, tasks.NewService(store.TaskDB()))
	svc := NewService(store.MetricsDB(), serverSvc, exec)
	svc.SetAgentClient(&fakeAgentClient{err: errors.New("agent down")})

	if err := svc.CollectAt(context.Background(), "srv", time.Now().UTC()); err == nil {
		t.Fatal("expected agent failure")
	}
	if exec.command != "" {
		t.Fatalf("expected no SSH fallback, got %q", exec.command)
	}
}

func TestCollectMarksAgentCertificateTimeError(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.TaskDatabase = filepath.Join(dir, "tasks.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, err = store.AppDB().Exec(`INSERT INTO credentials(id,name,type,username,created_at,updated_at) VALUES('cred','c','password','du','now','now')`)
	if err != nil {
		t.Fatal(err)
	}
	traits := `{"agent.enabled":"true","agent.url":"https://127.0.0.1:9443","agent.status":"compatible"}`
	_, err = store.AppDB().Exec(`INSERT INTO servers(id,name,host,port,ssh_username,credential_id,traits,os_id,os_version_id,os_pretty_name,os_supported,created_at,updated_at) VALUES('srv','s','h',22,'du','cred',?,'debian','13','Debian GNU/Linux 13',1,'now','now')`, traits)
	if err != nil {
		t.Fatal(err)
	}
	serverSvc := server.NewService(store.AppDB(), nil, tasks.NewService(store.TaskDB()))
	svc := NewService(store.MetricsDB(), serverSvc, &collectMetricsExecutor{})
	svc.SetAgentClient(&fakeAgentClient{err: x509.CertificateInvalidError{Reason: x509.Expired}})

	if err := svc.CollectAt(context.Background(), "srv", time.Now().UTC()); err == nil {
		t.Fatal("expected agent certificate failure")
	}
	srv, err := serverSvc.Get(context.Background(), "srv")
	if err != nil {
		t.Fatal(err)
	}
	if srv.Traits[agentcontract.TraitStatus] != agentcontract.StatusIncompatible {
		t.Fatalf("expected certificate error to mark agent incompatible, got %#v", srv.Traits)
	}
}

type collectMetricsExecutor struct {
	command string
	stdout  string
}

func (s *Service) SetAgentClient(client agentcontract.Client) {
	s.agent = client
}

type fakeAgentClient struct {
	metricsURL string
	snapshot   linux.MetricsSnapshot
	err        error
}

func (f *fakeAgentClient) Health(context.Context, string) (agentcontract.HealthResponse, error) {
	return agentcontract.HealthResponse{}, f.err
}
func (f *fakeAgentClient) OSRelease(context.Context, string) (linux.OSRelease, error) {
	return linux.OSRelease{}, f.err
}
func (f *fakeAgentClient) SystemTraits(context.Context, string) (map[string]string, error) {
	return nil, f.err
}
func (f *fakeAgentClient) MetricsSnapshot(_ context.Context, url string, serverID string) (linux.MetricsSnapshot, error) {
	f.metricsURL = url
	if f.err != nil {
		return linux.MetricsSnapshot{}, f.err
	}
	snap := f.snapshot
	snap.ServerID = serverID
	return snap, nil
}
func (f *fakeAgentClient) UFWStatus(context.Context, string) (remoteops.UFWStatus, error) {
	return remoteops.UFWStatus{}, f.err
}

func (f *collectMetricsExecutor) Exec(_ context.Context, _ sshx.Target, command sshx.CommandSpec) (sshx.CommandResult, error) {
	f.command = command.Command
	return sshx.CommandResult{Stdout: f.stdout, ExitCode: 0}, nil
}

func (f *collectMetricsExecutor) ExecSudo(context.Context, sshx.Target, sshx.CommandSpec) (sshx.CommandResult, error) {
	return sshx.CommandResult{}, nil
}

func (f *collectMetricsExecutor) Upload(context.Context, sshx.Target, sshx.UploadSpec) error {
	return nil
}

func (f *collectMetricsExecutor) Download(context.Context, sshx.Target, sshx.DownloadSpec) error {
	return nil
}
