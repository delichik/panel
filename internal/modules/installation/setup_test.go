package installation

import (
	"context"
	"testing"
	"time"

	agentcontract "panel/internal/agent/contract"
	"panel/internal/modules/applications"
	"panel/internal/modules/facilityapps"
	servers "panel/internal/modules/servers"
	"panel/internal/modules/servers/credential"
	"panel/internal/modules/tasks"
	"panel/internal/platform/config"
	"panel/internal/platform/database"
)

func TestSetupPromotesPendingHostAndConfiguresPanelEntry(t *testing.T) {
	cfg := config.Default()
	cfg.DataRoot = t.TempDir()
	cfg.AppDatabase = cfg.DataRoot + "/app.db"
	cfg.LogDatabase = cfg.DataRoot + "/log.db"
	cfg.MetricsDatabase = cfg.DataRoot + "/metrics.db"
	cfg.CoordinationDatabase = cfg.DataRoot + "/coordination.db"
	store, err := database.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if _, err := store.AppDB().ExecContext(ctx, `INSERT INTO credentials(id,name,type,username,created_at,updated_at) VALUES('cred','host','password','root','now','now')`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppDB().ExecContext(ctx, `INSERT INTO servers(id,name,host,port,credential_id,traits,created_at,updated_at) VALUES('srv-host','Panel host','127.0.0.1',22,'cred',?,'now','now')`, `{"agent.status":"compatible"}`); err != nil {
		t.Fatal(err)
	}
	stateSvc := NewService(store.AppDB())
	if _, err := stateSvc.SetPendingServer(ctx, "srv-host", "agent_deploy"); err != nil {
		t.Fatal(err)
	}
	facility := &setupFacilityFake{}
	setup := NewSetupService(stateSvc, setupCredentialFake{}, setupServerFake{srv: servers.Server{ID: "srv-host", Traits: map[string]string{agentcontract.TraitStatus: agentcontract.StatusCompatible}}}, setupTaskFake{}, facility)
	result, err := setup.Run(ctx, SetupInput{Host: "127.0.0.1", Port: 22, Username: "root", AuthType: credential.TypePassword, Password: "secret", Domain: "panel.example.test"})
	if err != nil {
		t.Fatal(err)
	}
	if result.HostServerID != "srv-host" || result.URL != "http://panel.example.test" {
		t.Fatalf("setup result = %#v", result)
	}
	state, err := stateSvc.Get(ctx)
	if err != nil || state.HostServerID != "srv-host" || state.PendingServerID != "" {
		t.Fatalf("installation state = %#v, %v", state, err)
	}
	if !facility.saved.PanelEntry.Enabled || facility.saved.PanelEntry.ServerID != "srv-host" {
		t.Fatalf("facility save = %#v", facility.saved)
	}
}

type setupCredentialFake struct{}

func (setupCredentialFake) Create(context.Context, credential.CreateRequest) (credential.Credential, error) {
	return credential.Credential{ID: "cred"}, nil
}
func (setupCredentialFake) Delete(context.Context, string) error { return nil }

type setupServerFake struct{ srv servers.Server }

func (f setupServerFake) Create(context.Context, servers.SaveRequest) (servers.Server, error) {
	return f.srv, nil
}
func (f setupServerFake) Get(context.Context, string) (servers.Server, error) { return f.srv, nil }
func (f setupServerFake) DeployAgent(context.Context, string) (tasks.Task, error) {
	return tasks.Task{}, nil
}

type setupTaskFake struct{}

func (setupTaskFake) Get(context.Context, string) (tasks.Task, error) {
	return tasks.Task{Status: tasks.StatusCompleted}, nil
}

type setupFacilityFake struct {
	saved     facilityapps.ReverseProxySaveInput
	committed bool
}

func (f *setupFacilityFake) GetReverseProxy(context.Context) (facilityapps.ReverseProxyConfig, error) {
	operationID := "operation-old"
	if f.committed {
		operationID = "operation-new"
	}
	operation := &applications.LifecycleOperation{ID: operationID, Status: "succeeded", UpdatedAt: time.Now()}
	return facilityapps.ReverseProxyConfig{DeploymentServers: append([]string(nil), f.saved.DeploymentServers...), PanelEntry: f.saved.PanelEntry, Domains: f.saved.Domains, Operation: operation}, nil
}
func (f *setupFacilityFake) SaveReverseProxy(_ context.Context, input facilityapps.ReverseProxySaveInput) (facilityapps.ReverseProxyConfig, error) {
	f.saved = input
	f.committed = true
	return f.GetReverseProxy(context.Background())
}
