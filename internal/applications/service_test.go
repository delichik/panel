package applications

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"panel/internal/agent"
	"panel/internal/appruntime"
	"panel/internal/config"
	"panel/internal/panelerr"
	"panel/internal/server"
	"panel/internal/storage"
	"panel/internal/tasks"
)

func TestCreateDisabledAppStoresRowAndDoesNotDeployRuntime(t *testing.T) {
	svc, runtime, _, closeStore := newTestService(t)
	defer closeStore()

	app, err := svc.Create(context.Background(), SaveInput{
		Name:     "web",
		Enabled:  false,
		SpecYAML: "name: web\nimage: nginx\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if app.ID == "" || app.Name != "web" || app.Enabled {
		t.Fatalf("app = %#v", app)
	}
	if app.Generation != 1 || app.JobID != "panel-web" || app.Namespace != "apps" {
		t.Fatalf("app persistence fields = %#v", app)
	}
	if len(runtime.deploys) != 0 {
		t.Fatalf("runtime deploys = %#v", runtime.deploys)
	}
}

func TestCreateEnabledAppDeploysToAgentRuntime(t *testing.T) {
	svc, runtime, _, closeStore := newTestService(t)
	defer closeStore()

	app, err := svc.Create(context.Background(), SaveInput{
		Name:     "web",
		Enabled:  true,
		SpecYAML: "name: web\nimage: nginx\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !app.Enabled {
		t.Fatalf("app = %#v", app)
	}
	if len(runtime.deploys) != 2 {
		t.Fatalf("deploys = %#v", runtime.deploys)
	}
	deploy := runtime.deploys[0]
	if deploy.ServerID != "srv-a" || deploy.Spec.ApplicationID != app.ID || deploy.Spec.InstanceID != app.ID+"-srv-a" {
		t.Fatalf("deploy = %#v", deploy)
	}
	if deploy.Spec.ContainerName != "panel-web" || deploy.Spec.Env["PANEL_SERVER_ID"] != "srv-a" {
		t.Fatalf("runtime spec = %#v", deploy.Spec)
	}
}

func TestCreateEnabledAnyTLSHostNetworkAppDeploysRuntime(t *testing.T) {
	svc, runtime, _, closeStore := newTestService(t)
	defer closeStore()

	app, err := svc.Create(context.Background(), SaveInput{
		Name:    "anytls",
		Enabled: true,
		SpecYAML: `name: anytls
image: jiasongji/anytls
networkMode: host
command:
  - "/app/anytls-server"
args:
  - "-l"
  - ":9443"
  - "-p"
  - "password"
restart:
  policy: "unless-stopped"
`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !app.Enabled || len(runtime.deploys) != 2 {
		t.Fatalf("app=%#v deploys=%#v", app, runtime.deploys)
	}
	spec := runtime.deploys[0].Spec
	if spec.NetworkMode != "host" || len(spec.Ports) != 0 {
		t.Fatalf("runtime network = %q ports %#v", spec.NetworkMode, spec.Ports)
	}
	if len(spec.Command) != 1 || spec.Command[0] != "/app/anytls-server" {
		t.Fatalf("command = %#v", spec.Command)
	}
	if len(spec.Args) != 4 || spec.Args[1] != ":9443" {
		t.Fatalf("args = %#v", spec.Args)
	}
}

func TestCreateEnabledAppWrapsRuntimeDeploymentError(t *testing.T) {
	svc, runtime, _, closeStore := newTestService(t)
	defer closeStore()
	runtime.deployErr = errors.New("create container failed: invalid runtime config")

	_, err := svc.Create(context.Background(), SaveInput{
		Name:     "anytls",
		Enabled:  true,
		SpecYAML: "name: anytls\nimage: jiasongji/anytls\nnetworkMode: host\n",
	})
	var appErr *panelerr.Error
	if !errors.As(err, &appErr) || appErr.Code != "application_runtime_operation_failed" {
		t.Fatalf("err = %#v", err)
	}
	if !strings.Contains(appErr.Message, "invalid runtime config") {
		t.Fatalf("message = %q", appErr.Message)
	}
}

func TestCreateDuplicateAppNameReturnsValidation(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	if _, err := svc.Create(ctx, SaveInput{Name: "web", SpecYAML: "name: web\nimage: nginx\n"}); err != nil {
		t.Fatal(err)
	}
	_, err := svc.Create(ctx, SaveInput{Name: "web", SpecYAML: "name: web-copy\nimage: nginx\n"})
	var appErr *panelerr.Error
	if !errors.As(err, &appErr) || appErr.Code != "application_name_duplicate" {
		t.Fatalf("err = %#v", err)
	}
}

func TestCreateDisabledSelectedApplicationWithArgsStoresTargets(t *testing.T) {
	svc, runtime, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{
		Name:    "anytls",
		Enabled: false,
		SpecYAML: `name: anytls
image: jiasongji/anytls
networkMode: host
command:
  - /app/anytls-server
args:
  - -l
  - :9443
  - -p
  - "this is password"
env:
  TZ: Asia/Shanghai
restart:
  policy: unless-stopped
`,
		DeploymentMode:    DeploymentModeSelected,
		DeploymentServers: []string{"srv-b", "srv-a", "srv-c"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if app.DeploymentMode != DeploymentModeSelected || !reflect.DeepEqual(app.DeploymentServers, []string{"srv-a", "srv-b", "srv-c"}) {
		t.Fatalf("deployment targets = %q %#v", app.DeploymentMode, app.DeploymentServers)
	}
	if len(runtime.deploys) != 0 {
		t.Fatalf("disabled create should not call agent runtime, deploys = %#v", runtime.deploys)
	}
}

func TestCreateRejectsArgumentsInCommandArray(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()

	_, err := svc.Create(context.Background(), SaveInput{
		Name: "anytls",
		SpecYAML: `name: anytls
image: jiasongji/anytls
networkMode: host
command:
  - /app/anytls-server
  - -l
  - :9443
`,
	})
	var appErr *panelerr.Error
	if !errors.As(err, &appErr) || appErr.Code != "application_command_invalid" {
		t.Fatalf("err = %#v", err)
	}
}

func TestRedeployEnabledApplicationsDeploysEnabledApps(t *testing.T) {
	svc, runtime, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	if _, err := svc.Create(ctx, SaveInput{Name: "enabled", Enabled: true, SpecYAML: "name: enabled\nimage: nginx\n"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(ctx, SaveInput{Name: "disabled", Enabled: false, SpecYAML: "name: disabled\nimage: nginx\n"}); err != nil {
		t.Fatal(err)
	}
	runtime.deploys = nil

	count, err := svc.RedeployEnabledApplications(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("redeployed = %d, want 1", count)
	}
	if len(runtime.deploys) != 2 || runtime.deploys[0].Spec.Name != "enabled" || runtime.deploys[1].Spec.Name != "enabled" {
		t.Fatalf("deploys = %#v", runtime.deploys)
	}
}

func TestTemplateVariablesRenderIntoRuntimeSpec(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	svc.SetBuiltinVariableResolver(fakeBuiltinResolver{
		"certs": map[string]any{
			"example_com": map[string]any{
				"certificatePem": "CERT",
			},
		},
	})

	app, err := svc.Create(context.Background(), SaveInput{
		Name:      "web",
		SpecYAML:  "name: web\nimage: '{{ .vars.image }}'\nenv:\n  TLS_CERT: '{{ .certs.example_com.certificatePem }}'\n",
		Variables: map[string]string{"image": "nginx:1.27"},
	})
	if err != nil {
		t.Fatal(err)
	}
	spec, issues, err := svc.renderApplication(context.Background(), app)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) > 0 {
		t.Fatalf("issues = %#v", issues)
	}
	if spec.Image != "nginx:1.27" || spec.Env["TLS_CERT"] != "CERT" {
		t.Fatalf("rendered spec = %#v", spec)
	}
	if app.ResolvedVariables["image"] != "nginx:1.27" || app.ResolvedVariables["certs"] == nil {
		t.Fatalf("resolved variables = %#v", app.ResolvedVariables)
	}
}

func TestApplicationFileMountCreatesManagedRuntimeFile(t *testing.T) {
	svc, runtime, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{
		Name:     "web",
		Enabled:  false,
		SpecYAML: "name: web\nimage: nginx\nmounts:\n  - type: file\n    source: config/app.conf\n    target: /etc/app.conf\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SaveFile(ctx, app.ID, FileSaveInput{
		Path:          "config/app.conf",
		Kind:          "template",
		ContentBase64: base64.StdEncoding.EncodeToString([]byte("server={{ .vars.server }}")),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Update(ctx, app.ID, SaveInput{
		Name:      "web",
		Enabled:   true,
		SpecYAML:  app.SpecYAML,
		Variables: map[string]string{"server": "localhost"},
	}); err != nil {
		t.Fatal(err)
	}
	if len(runtime.deploys) != 2 {
		t.Fatalf("deploys = %#v", runtime.deploys)
	}
	spec := runtime.deploys[0].Spec
	if len(spec.Files) != 1 || spec.Files[0].Path != "config/app.conf" || string(spec.Files[0].Content) != "server=localhost" {
		t.Fatalf("files = %#v", spec.Files)
	}
	if len(spec.Mounts) != 1 || spec.Mounts[0].Type != "managed_file" || spec.Mounts[0].Target != "/etc/app.conf" {
		t.Fatalf("mounts = %#v", spec.Mounts)
	}
}

func TestPanelFileMountCreatesReadOnlyRuntimeFile(t *testing.T) {
	svc, runtime, _, closeStore := newTestService(t)
	defer closeStore()
	svc.SetPanelFileProvider(fakePanelFileProvider{content: []byte("CERTIFICATE")})

	if _, err := svc.Create(context.Background(), SaveInput{
		Name:     "tls",
		Enabled:  true,
		SpecYAML: "name: tls\nimage: nginx\nmounts:\n  - type: panel_file\n    source: certificate:cert_1:certificate\n    target: /etc/tls/cert.pem\n",
	}); err != nil {
		t.Fatal(err)
	}
	spec := runtime.deploys[0].Spec
	if len(spec.Files) != 1 || string(spec.Files[0].Content) != "CERTIFICATE" || spec.Files[0].Mode != "0644" {
		t.Fatalf("files = %#v", spec.Files)
	}
	if len(spec.Mounts) != 1 || spec.Mounts[0].Target != "/etc/tls/cert.pem" || !spec.Mounts[0].ReadOnly {
		t.Fatalf("mounts = %#v", spec.Mounts)
	}
}

func TestSelectedDeploymentDeploysOneInstancePerSelectedServer(t *testing.T) {
	svc, runtime, _, closeStore := newTestService(t)
	defer closeStore()

	if _, err := svc.Create(context.Background(), SaveInput{
		Name:              "web",
		Enabled:           true,
		SpecYAML:          "name: web\nimage: nginx\n",
		DeploymentMode:    DeploymentModeSelected,
		DeploymentServers: []string{"srv-b", "srv-a", "srv-a"},
	}); err != nil {
		t.Fatal(err)
	}
	if len(runtime.deploys) != 2 {
		t.Fatalf("deploys = %#v", runtime.deploys)
	}
	if runtime.deploys[0].ServerID != "srv-a" || runtime.deploys[1].ServerID != "srv-b" {
		t.Fatalf("deploy order = %#v", runtime.deploys)
	}
}

func TestPersistentDeploymentRequiresExactlyOneServer(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()

	_, err := svc.Create(context.Background(), SaveInput{
		Name:           "db",
		Enabled:        true,
		SpecYAML:       "name: db\nimage: postgres\nmounts:\n  - type: persistent\n    source: data\n    target: /var/lib/postgresql/data\n",
		DeploymentMode: DeploymentModeAll,
	})
	if err == nil || !strings.Contains(err.Error(), "persistent applications must target exactly one server") {
		t.Fatalf("expected persistent target validation, got %v", err)
	}
}

func TestStopAppCallsRuntimeAndDisablesApp(t *testing.T) {
	svc, runtime, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{Name: "web", Enabled: true, SpecYAML: "name: web\nimage: nginx\n"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Stop(ctx, app.ID, false); err != nil {
		t.Fatal(err)
	}
	stopped, err := svc.Get(ctx, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stopped.Enabled {
		t.Fatalf("app should be disabled: %#v", stopped)
	}
	if len(runtime.stops) != 2 || runtime.stops[0].InstanceID != app.ID+"-srv-a" || runtime.stops[1].InstanceID != app.ID+"-srv-b" {
		t.Fatalf("stops = %#v", runtime.stops)
	}
}

func TestRuntimeRefreshesInstanceStatuses(t *testing.T) {
	svc, runtime, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{Name: "web", Enabled: true, SpecYAML: "name: web\nimage: nginx\n"})
	if err != nil {
		t.Fatal(err)
	}
	instanceID := app.ID + "-srv-a"
	runtime.statuses = map[string]appruntime.InstanceStatus{
		instanceID: {InstanceID: instanceID, ContainerName: "panel-web", Status: appruntime.StatusRunning, Image: "nginx", ObservedAt: time.Now().UTC()},
	}

	result, err := svc.Runtime(ctx, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != appruntime.StatusRunning || len(result.Instances) != 2 {
		t.Fatalf("runtime = %#v", result)
	}
	found := false
	for _, inst := range result.Instances {
		if inst.InstanceID == instanceID && inst.ContainerName == "panel-web" && inst.Image == "nginx" {
			found = true
		}
	}
	if !found {
		t.Fatalf("runtime instances = %#v", result.Instances)
	}
}

func TestListWithRuntimeRefreshesRuntimeStatus(t *testing.T) {
	svc, runtime, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{Name: "web", Enabled: true, SpecYAML: "name: web\nimage: nginx\n"})
	if err != nil {
		t.Fatal(err)
	}
	runtime.statuses = map[string]appruntime.InstanceStatus{
		app.ID + "-srv-a": {InstanceID: app.ID + "-srv-a", ContainerName: "panel-web", Status: appruntime.StatusRunning, ObservedAt: time.Now().UTC()},
		app.ID + "-srv-b": {InstanceID: app.ID + "-srv-b", ContainerName: "panel-web", Status: appruntime.StatusRunning, ObservedAt: time.Now().UTC()},
	}

	apps, err := svc.ListWithRuntime(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(apps) != 1 || apps[0].RuntimeStatus != appruntime.StatusRunning {
		t.Fatalf("apps = %#v", apps)
	}
}

func TestLogsReadFromRuntimeInstance(t *testing.T) {
	svc, runtime, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{Name: "web", Enabled: true, SpecYAML: "name: web\nimage: nginx\n"})
	if err != nil {
		t.Fatal(err)
	}
	runtime.logs = "hello\n"
	result, err := svc.Logs(ctx, app.ID, LogInput{InstanceID: app.ID + "-srv-a", Tail: 20})
	if err != nil {
		t.Fatal(err)
	}
	if result.InstanceID != app.ID+"-srv-a" || result.ContainerName == "" || result.Logs != "hello\n" {
		t.Fatalf("logs = %#v", result)
	}
	if runtime.logTail != 20 {
		t.Fatalf("tail = %d", runtime.logTail)
	}
}

func TestPersistentDataDownloadsFromRuntimeInstance(t *testing.T) {
	svc, runtime, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{
		Name:              "db",
		Enabled:           true,
		SpecYAML:          "name: db\nimage: postgres\nmounts:\n  - type: persistent\n    source: data\n    target: /var/lib/postgresql/data\n",
		DeploymentMode:    DeploymentModeSelected,
		DeploymentServers: []string{"srv-a"},
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime.archiveContent = []byte("zip")
	result, err := svc.PersistentData(ctx, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if string(result.Content) != "zip" {
		t.Fatalf("content = %q", result.Content)
	}
	if runtime.archiveApplicationID != app.ID {
		t.Fatalf("archive application = %q, want %q", runtime.archiveApplicationID, app.ID)
	}
	if runtime.archiveBaseURL != "https://srv-a.agent" {
		t.Fatalf("archive baseURL = %q", runtime.archiveBaseURL)
	}
}

func TestPersistentDataRejectsApplicationWithoutPersistentStorage(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{Name: "web", Enabled: true, SpecYAML: "name: web\nimage: nginx\n"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PersistentData(ctx, app.ID); err == nil {
		t.Fatal("expected persistent data download to be rejected")
	}
}

func TestRestorePersistentDataRestoresAndRestarts(t *testing.T) {
	svc, runtime, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{
		Name:              "db",
		Enabled:           true,
		SpecYAML:          "name: db\nimage: postgres\nmounts:\n  - type: persistent\n    source: data\n    target: /var/lib/postgresql/data\n",
		DeploymentMode:    DeploymentModeSelected,
		DeploymentServers: []string{"srv-a"},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.RestorePersistentData(ctx, app.ID, []byte("zip"))
	if err != nil {
		t.Fatal(err)
	}
	if result.TaskID == "" {
		t.Fatal("expected restart task id")
	}
	if runtime.restoreApplicationID != app.ID || string(runtime.restoreContent) != "zip" {
		t.Fatalf("restore application=%q content=%q", runtime.restoreApplicationID, runtime.restoreContent)
	}
	if len(runtime.restarts) == 0 {
		t.Fatal("expected application restart after restore")
	}
}

func newTestService(t *testing.T) (*Service, *fakeRuntimeClient, *fakeServerProvider, func()) {
	t.Helper()
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
	runtime := &fakeRuntimeClient{}
	servers := &fakeServerProvider{items: map[string]server.Server{
		"srv-a": readyServer("srv-a"),
		"srv-b": readyServer("srv-b"),
	}}
	if _, err := store.AppDB().Exec(`INSERT INTO credentials(id,name,type,username,created_at,updated_at) VALUES(?,?,?,?,?,?)`,
		"cred_1", "test", "password", "root", time.Now().UTC().Format(time.RFC3339Nano), time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	for _, srv := range servers.items {
		traits, _ := json.Marshal(srv.Traits)
		now := time.Now().UTC().Format(time.RFC3339Nano)
		if _, err := store.AppDB().Exec(`INSERT INTO servers(id,name,host,port,ssh_username,credential_id,docker_host,traits,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
			srv.ID, srv.Name, srv.Host, srv.Port, srv.SSHUsername, "cred_1", srv.DockerHost, string(traits), now, now); err != nil {
			t.Fatal(err)
		}
	}
	svc := NewService(store.AppDB(), runtime, tasks.NewService(store.TaskDB()), Config{
		Namespace:      "apps",
		Region:         "global",
		Datacenter:     "dc1",
		SaveSessionDir: filepath.Join(dir, "sessions"),
	})
	svc.SetServerProvider(servers)
	return svc, runtime, servers, func() { _ = store.Close() }
}

func readyServer(id string) server.Server {
	return server.Server{
		ID:          id,
		Name:        id,
		Host:        "127.0.0.1",
		Port:        22,
		SSHUsername: "root",
		DockerHost:  agent.DefaultDockerHost,
		Traits: map[string]string{
			agent.TraitEnabled: "true",
			agent.TraitURL:     "https://" + id + ".agent",
			agent.TraitStatus:  agent.StatusCompatible,
		},
	}
}

type fakeRuntimeClient struct {
	deploys              []agent.RuntimeDeployRequest
	stops                []agent.RuntimeStopRequest
	restarts             []agent.RuntimeRestartRequest
	statuses             map[string]appruntime.InstanceStatus
	logs                 string
	logTail              int
	archiveBaseURL       string
	archiveApplicationID string
	archiveContent       []byte
	restoreBaseURL       string
	restoreApplicationID string
	restoreContent       []byte
	deployErr            error
}

func (f *fakeRuntimeClient) RuntimeDeploy(ctx context.Context, baseURL string, req agent.RuntimeDeployRequest) (agent.RuntimeInstanceResponse, error) {
	f.deploys = append(f.deploys, req)
	if f.deployErr != nil {
		return agent.RuntimeInstanceResponse{}, f.deployErr
	}
	return agent.RuntimeInstanceResponse{
		InstanceID:    req.Spec.InstanceID,
		ContainerName: req.Spec.ContainerName,
		ContainerID:   "container-" + req.ServerID,
		Status:        appruntime.StatusRunning,
		ObservedAt:    time.Now().UTC(),
	}, nil
}

func (f *fakeRuntimeClient) RuntimeStop(ctx context.Context, baseURL string, req agent.RuntimeStopRequest) (agent.RuntimeInstanceResponse, error) {
	f.stops = append(f.stops, req)
	return agent.RuntimeInstanceResponse{InstanceID: req.InstanceID, Status: appruntime.StatusStopped, ObservedAt: time.Now().UTC()}, nil
}

func (f *fakeRuntimeClient) RuntimeRestart(ctx context.Context, baseURL string, req agent.RuntimeRestartRequest) (agent.RuntimeInstanceResponse, error) {
	f.restarts = append(f.restarts, req)
	return agent.RuntimeInstanceResponse{InstanceID: req.InstanceID, Status: appruntime.StatusRunning, ObservedAt: time.Now().UTC()}, nil
}

func (f *fakeRuntimeClient) RuntimeStatus(ctx context.Context, baseURL, instanceID, containerName string) (agent.RuntimeStatusResponse, error) {
	if f.statuses != nil {
		if status, ok := f.statuses[instanceID]; ok {
			return agent.RuntimeStatusResponse{InstanceStatus: status}, nil
		}
	}
	return agent.RuntimeStatusResponse{InstanceStatus: appruntime.InstanceStatus{InstanceID: instanceID, Status: appruntime.StatusRunning, ObservedAt: time.Now().UTC()}}, nil
}

func (f *fakeRuntimeClient) RuntimeLogs(ctx context.Context, baseURL, instanceID, containerName string, tail int) (agent.RuntimeLogsResponse, error) {
	f.logTail = tail
	return agent.RuntimeLogsResponse{InstanceID: instanceID, Logs: f.logs}, nil
}

func (f *fakeRuntimeClient) RuntimePersistentArchive(ctx context.Context, baseURL, applicationID string) (agent.RuntimePersistentArchiveResponse, error) {
	f.archiveBaseURL = baseURL
	f.archiveApplicationID = applicationID
	return agent.RuntimePersistentArchiveResponse{
		ApplicationID: applicationID,
		Filename:      applicationID + "-persistent.zip",
		ContentBase64: base64.StdEncoding.EncodeToString(f.archiveContent),
	}, nil
}

func (f *fakeRuntimeClient) RuntimePersistentRestore(ctx context.Context, baseURL, applicationID string, content []byte) (agent.RuntimePersistentRestoreResponse, error) {
	f.restoreBaseURL = baseURL
	f.restoreApplicationID = applicationID
	f.restoreContent = content
	return agent.RuntimePersistentRestoreResponse{ApplicationID: applicationID, Restored: true}, nil
}

type fakeServerProvider struct {
	items map[string]server.Server
}

func (f *fakeServerProvider) List(ctx context.Context) ([]server.Server, error) {
	out := make([]server.Server, 0, len(f.items))
	for _, srv := range f.items {
		out = append(out, srv)
	}
	return out, nil
}

func (f *fakeServerProvider) Get(ctx context.Context, id string) (server.Server, error) {
	return f.items[id], nil
}

type fakeBuiltinResolver map[string]any

func (f fakeBuiltinResolver) BuiltinVariables(ctx context.Context) (map[string]any, error) {
	return map[string]any(f), nil
}

type fakePanelFileProvider struct {
	content []byte
}

func (f fakePanelFileProvider) PanelFileCatalog(ctx context.Context) ([]PanelFileDefinition, error) {
	return nil, nil
}

func (f fakePanelFileProvider) ReadPanelFile(ctx context.Context, source string) ([]byte, error) {
	return append([]byte(nil), f.content...), nil
}
