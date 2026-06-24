package applications

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	agentcontract "panel/internal/agent/contract"
	"panel/internal/modules/applications/runtime"
	"panel/internal/modules/servers"
	"panel/internal/modules/tasks"
	"panel/internal/platform/config"
	storage "panel/internal/platform/database"
	panelerr "panel/internal/platform/errors"
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
	if len(runtime.writes) != 2 || len(runtime.pulls) != 2 || len(runtime.deletes) != 2 || len(runtime.actions) != 2 {
		t.Fatalf("atomic deploy calls writes=%#v pulls=%#v deletes=%#v actions=%#v", runtime.writes, runtime.pulls, runtime.deletes, runtime.actions)
	}
	if runtime.pulls[0] != "nginx" || runtime.deletes[0] != "panel-web" || runtime.actions[0] != "container-srv-a:start" {
		t.Fatalf("atomic deploy sequence pulls=%#v deletes=%#v actions=%#v", runtime.pulls, runtime.deletes, runtime.actions)
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

func TestCreateEnabledAppContinuesDeployingRemainingTargetsAfterRuntimeError(t *testing.T) {
	svc, runtime, _, closeStore := newTestService(t)
	defer closeStore()
	runtime.deployErrByServer = map[string]error{"srv-a": errors.New("agent down")}

	_, err := svc.Create(context.Background(), SaveInput{
		Name:     "web",
		Enabled:  true,
		SpecYAML: "name: web\nimage: nginx\n",
	})
	var appErr *panelerr.Error
	if !errors.As(err, &appErr) || appErr.Code != "application_runtime_operation_failed" {
		t.Fatalf("err = %#v", err)
	}
	if !strings.Contains(appErr.Message, "srv-a") || !strings.Contains(appErr.Message, "1 of 2") {
		t.Fatalf("message = %q", appErr.Message)
	}
	if len(runtime.deploys) != 2 {
		t.Fatalf("deploys = %#v", runtime.deploys)
	}
	if runtime.deploys[0].ServerID != "srv-a" || runtime.deploys[1].ServerID != "srv-b" {
		t.Fatalf("deploy order = %#v", runtime.deploys)
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

func TestDeployTaskExecutorEnablesAppDeploysRuntimeAndCompletesTask(t *testing.T) {
	svc, runtime, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{Name: "web", Enabled: false, SpecYAML: "name: web\nimage: nginx\n"})
	if err != nil {
		t.Fatal(err)
	}
	task, err := svc.tasks.Create(ctx, tasks.CreateInput{
		Type:         TaskTypeDeploy,
		ResourceType: "application",
		ResourceID:   app.ID,
		Summary:      "Deploying application " + app.Name,
	})
	if err != nil {
		t.Fatal(err)
	}
	def, ok := svc.tasks.Registry().Definition(TaskTypeDeploy)
	if !ok || def.Execute == nil {
		t.Fatal("expected application deploy executor")
	}
	if err := def.Execute(tasks.TaskContext{Context: ctx, Task: task, Service: svc.tasks}); err != nil {
		t.Fatal(err)
	}
	if len(runtime.deploys) != 2 {
		t.Fatalf("deploys = %#v", runtime.deploys)
	}
	deployed, err := svc.Get(ctx, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !deployed.Enabled {
		t.Fatalf("expected app to be enabled after deploy task, got %#v", deployed)
	}
	storedTask, err := svc.tasks.Get(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if storedTask.Status != tasks.StatusCompleted {
		t.Fatalf("expected completed deploy task, got %#v", storedTask)
	}
}

func TestRefreshTaskExecutorRedeploysChangedApplicationAndCompletesTask(t *testing.T) {
	svc, runtime, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	svc.SetBuiltinVariableResolver(fakeBuiltinResolver{"message": "one"})

	app, err := svc.Create(ctx, SaveInput{
		Name:     "web",
		Enabled:  true,
		SpecYAML: "name: web\nimage: nginx\nenv:\n  MESSAGE: '{{ .message }}'\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	svc.SetBuiltinVariableResolver(fakeBuiltinResolver{"message": "two"})
	runtime.deploys = nil
	task, err := svc.tasks.Create(ctx, tasks.CreateInput{
		Type:         TaskTypeRefresh,
		ResourceType: "application",
		ResourceID:   app.ID,
		Summary:      "Refreshing application " + app.Name,
	})
	if err != nil {
		t.Fatal(err)
	}
	def, ok := svc.tasks.Registry().Definition(TaskTypeRefresh)
	if !ok || def.Execute == nil {
		t.Fatal("expected application refresh executor")
	}
	if err := def.Execute(tasks.TaskContext{Context: ctx, Task: task, Service: svc.tasks}); err != nil {
		t.Fatal(err)
	}
	if len(runtime.deploys) != 2 {
		t.Fatalf("deploys = %#v", runtime.deploys)
	}
	if runtime.deploys[0].Spec.Env["MESSAGE"] != "two" || runtime.deploys[1].Spec.Env["MESSAGE"] != "two" {
		t.Fatalf("runtime env = %#v", runtime.deploys)
	}
	storedTask, err := svc.tasks.Get(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if storedTask.Status != tasks.StatusCompleted {
		t.Fatalf("expected completed refresh task, got %#v", storedTask)
	}
	refreshed, err := svc.Get(ctx, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.Generation <= app.Generation || refreshed.SpecHash == app.SpecHash {
		t.Fatalf("expected refreshed snapshot, before=%#v after=%#v", app, refreshed)
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
	svc.SetInternalFileProvider(fakeInternalFileProvider{content: []byte("CERTIFICATE")})

	if _, err := svc.Create(context.Background(), SaveInput{
		Name:     "tls",
		Enabled:  true,
		SpecYAML: "name: tls\nimage: nginx\nmounts:\n  - type: panel_file\n    source: key_asset:cert_1:certificate\n    target: /etc/tls/cert.pem\n",
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
	result, err := svc.tasks.List(ctx, tasks.ListFilter{Type: TaskTypeStop, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 1 || result.Items[0].ParamsJSON != "{}" {
		t.Fatalf("expected stop task to preserve default purge params, got %#v", result.Items)
	}
}

func TestStopTaskExecutorUsesPurgeParamAndCompletesTask(t *testing.T) {
	svc, runtime, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{Name: "web", Enabled: true, SpecYAML: "name: web\nimage: nginx\n"})
	if err != nil {
		t.Fatal(err)
	}
	task, err := svc.tasks.Create(ctx, tasks.CreateInput{
		Type:         TaskTypeStop,
		ResourceType: "application",
		ResourceID:   app.ID,
		ParamsJSON:   `{"purge":true}`,
		Summary:      "Stopping application " + app.Name,
	})
	if err != nil {
		t.Fatal(err)
	}
	def, ok := svc.tasks.Registry().Definition(TaskTypeStop)
	if !ok || def.Execute == nil {
		t.Fatal("expected application stop executor")
	}
	before := len(runtime.stops)
	if err := def.Execute(tasks.TaskContext{Context: ctx, Task: task, Service: svc.tasks}); err != nil {
		t.Fatal(err)
	}
	if len(runtime.stops) != before+2 {
		t.Fatalf("expected two stop calls, got %d before=%d", len(runtime.stops), before)
	}
	for _, req := range runtime.stops[before:] {
		if !req.Purge {
			t.Fatalf("expected purge stop request, got %#v", req)
		}
	}
	storedTask, err := svc.tasks.Get(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if storedTask.Status != tasks.StatusCompleted {
		t.Fatalf("expected completed stop task, got %#v", storedTask)
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

func TestImageCheckTaskExecutorUpdatesApplicationAndCompletesTask(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	svc.imageResolver = fakeImageResolver{result: ImageDigestResult{
		Reference: "registry.example.com/team/web:latest",
		Digest:    "sha256:latest",
	}}

	app, err := svc.Create(ctx, SaveInput{Name: "web", SpecYAML: "name: web\nimage: registry.example.com/team/web:latest\n"})
	if err != nil {
		t.Fatal(err)
	}
	task, err := svc.tasks.Create(ctx, tasks.CreateInput{
		Type:         TaskTypeImageCheck,
		ResourceType: "application",
		ResourceID:   app.ID,
		Summary:      "Checking image for " + app.Name,
	})
	if err != nil {
		t.Fatal(err)
	}
	def, ok := svc.tasks.Registry().Definition(TaskTypeImageCheck)
	if !ok || def.Execute == nil {
		t.Fatal("expected application image check executor")
	}
	if err := def.Execute(tasks.TaskContext{Context: ctx, Task: task, Service: svc.tasks}); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Get(ctx, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ImageReference != "registry.example.com/team/web:latest" || got.ImageLatestDigest != "sha256:latest" || got.ImageDigest != "sha256:latest" || got.ImageLastError != "" {
		t.Fatalf("unexpected image state: %#v", got)
	}
	storedTask, err := svc.tasks.Get(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if storedTask.Status != tasks.StatusCompleted {
		t.Fatalf("expected completed image check task, got %#v", storedTask)
	}
}

func TestImageUpdateTaskExecutorUpdatesImageDeploysRuntimeAndCompletesTask(t *testing.T) {
	svc, runtime, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	svc.imageResolver = fakeImageResolver{result: ImageDigestResult{
		Reference: "registry.example.com/team/web:latest",
		Digest:    "sha256:latest",
	}}

	app, err := svc.Create(ctx, SaveInput{
		Name:     "web",
		Enabled:  true,
		SpecYAML: "name: web\nimage: registry.example.com/team/web:latest\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime.deploys = nil
	task, err := svc.tasks.Create(ctx, tasks.CreateInput{
		Type:         TaskTypeImageUpdate,
		ResourceType: "application",
		ResourceID:   app.ID,
		Summary:      "Updating image for " + app.Name,
	})
	if err != nil {
		t.Fatal(err)
	}
	def, ok := svc.tasks.Registry().Definition(TaskTypeImageUpdate)
	if !ok || def.Execute == nil {
		t.Fatal("expected application image update executor")
	}
	if err := def.Execute(tasks.TaskContext{Context: ctx, Task: task, Service: svc.tasks}); err != nil {
		t.Fatal(err)
	}
	if len(runtime.deploys) != 2 {
		t.Fatalf("deploys = %#v", runtime.deploys)
	}
	got, err := svc.Get(ctx, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ImageReference != "registry.example.com/team/web:latest" || got.ImageDigest != "sha256:latest" || got.ImageLatestDigest != "sha256:latest" || got.ImageUpdateAvailable || got.ImageLastError != "" {
		t.Fatalf("unexpected image state: %#v", got)
	}
	if got.Generation != app.Generation+1 {
		t.Fatalf("generation = %d, want %d", got.Generation, app.Generation+1)
	}
	storedTask, err := svc.tasks.Get(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if storedTask.Status != tasks.StatusCompleted {
		t.Fatalf("expected completed image update task, got %#v", storedTask)
	}
}

func TestGetAggregatesImageUpdatesFromRuntimeInstances(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	app, err := svc.Create(ctx, SaveInput{Name: "web", Enabled: true, SpecYAML: "name: web\nimage: nginx\n"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := svc.db.ExecContext(ctx, `INSERT INTO image_updates(server_id,reference,local_digest,latest_digest,update_available,last_error,checked_at) VALUES(?,?,?,?,?,?,?), (?,?,?,?,?,?,?)`,
		"srv-a", "nginx:latest", "sha256:old", "sha256:new", 1, "", now,
		"srv-b", "nginx:latest", "sha256:new", "sha256:new", 0, "", now); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Get(ctx, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.ImageUpdateAvailable {
		t.Fatalf("expected app update to be available: %#v", got)
	}
	if len(got.ImageUpdateTargets) != 2 {
		t.Fatalf("targets = %#v", got.ImageUpdateTargets)
	}
	if got.ImageUpdateTargets[0].ServerID != "srv-a" || !got.ImageUpdateTargets[0].UpdateAvailable || got.ImageUpdateTargets[0].LatestDigest != "sha256:new" {
		t.Fatalf("first target = %#v", got.ImageUpdateTargets[0])
	}
	if got.ImageUpdateTargets[1].ServerID != "srv-b" || got.ImageUpdateTargets[1].UpdateAvailable {
		t.Fatalf("second target = %#v", got.ImageUpdateTargets[1])
	}
}

func TestUpdateImageMarksNodeImageCacheCurrent(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	svc.imageResolver = fakeImageResolver{result: ImageDigestResult{Reference: "nginx", Digest: "sha256:new"}}
	app, err := svc.Create(ctx, SaveInput{Name: "web", Enabled: true, SpecYAML: "name: web\nimage: nginx\n"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano)
	if _, err := svc.db.ExecContext(ctx, `INSERT INTO image_updates(server_id,reference,local_digest,latest_digest,update_available,last_error,checked_at) VALUES(?,?,?,?,?,?,?)`,
		"srv-a", "nginx:latest", "sha256:old", "sha256:new", 1, "", now); err != nil {
		t.Fatal(err)
	}
	result, err := svc.UpdateImage(ctx, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Application.ImageUpdateAvailable {
		t.Fatalf("expected returned app to be current: %#v", result.Application.ImageUpdateTargets)
	}
	var available int
	var localDigest string
	if err := svc.db.QueryRowContext(ctx, `SELECT local_digest,update_available FROM image_updates WHERE server_id=? AND reference=?`, "srv-a", "nginx:latest").Scan(&localDigest, &available); err != nil {
		t.Fatal(err)
	}
	if localDigest != "sha256:new" || available != 0 {
		t.Fatalf("cache local=%q available=%d", localDigest, available)
	}
}

func TestRestartTaskExecutorRestartsRuntimeAndCompletesTask(t *testing.T) {
	svc, runtime, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{
		Name:              "web",
		Enabled:           true,
		SpecYAML:          "name: web\nimage: nginx\n",
		DeploymentMode:    DeploymentModeSelected,
		DeploymentServers: []string{"srv-a"},
	})
	if err != nil {
		t.Fatal(err)
	}
	task, err := svc.tasks.Create(ctx, tasks.CreateInput{
		Type:         TaskTypeRestart,
		ResourceType: "application",
		ResourceID:   app.ID,
		Summary:      "Restarting application",
	})
	if err != nil {
		t.Fatal(err)
	}
	def, ok := svc.tasks.Registry().Definition(TaskTypeRestart)
	if !ok || def.Execute == nil {
		t.Fatal("expected application restart executor")
	}
	before := len(runtime.restarts)
	if err := def.Execute(tasks.TaskContext{Context: ctx, Task: task, Service: svc.tasks}); err != nil {
		t.Fatal(err)
	}
	if len(runtime.restarts) != before+1 {
		t.Fatalf("expected one restart call, got %d before=%d", len(runtime.restarts), before)
	}
	storedTask, err := svc.tasks.Get(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if storedTask.Status != tasks.StatusCompleted {
		t.Fatalf("expected completed restart task, got %#v", storedTask)
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

func TestMigrateDeploysTargetAndDropsSourceInstance(t *testing.T) {
	svc, runtime, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{
		Name:              "web",
		Enabled:           true,
		SpecYAML:          "name: web\nimage: nginx\n",
		DeploymentMode:    DeploymentModeSelected,
		DeploymentServers: []string{"srv-a"},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.Migrate(ctx, app.ID, MigrationInput{SourceServerID: "srv-a", TargetServerID: "srv-b"})
	if err != nil {
		t.Fatal(err)
	}
	if result.TaskID == "" {
		t.Fatal("expected migration task id")
	}
	migrated, err := svc.Get(ctx, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if migrated.DeploymentMode != DeploymentModeSelected || len(migrated.DeploymentServers) != 1 || migrated.DeploymentServers[0] != "srv-b" {
		t.Fatalf("deployment target = %q %#v", migrated.DeploymentMode, migrated.DeploymentServers)
	}
	instances, err := svc.runtimeInstances(ctx, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 1 || instances[0].ServerID != "srv-b" {
		t.Fatalf("instances = %#v", instances)
	}
	if len(runtime.stops) != 0 {
		t.Fatalf("source should not be stopped during lossless migration: %#v", runtime.stops)
	}
	lastDeploy := runtime.deploys[len(runtime.deploys)-1]
	if lastDeploy.ServerID != "srv-b" {
		t.Fatalf("last deploy server = %q", lastDeploy.ServerID)
	}
}

func TestMigrateRejectsExternalMounts(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{
		Name:              "web",
		Enabled:           true,
		SpecYAML:          "name: web\nimage: nginx\nvolumes:\n  - source: web-data\n    target: /data\n",
		DeploymentMode:    DeploymentModeSelected,
		DeploymentServers: []string{"srv-a"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Migrate(ctx, app.ID, MigrationInput{SourceServerID: "srv-a", TargetServerID: "srv-b"}); err == nil || !strings.Contains(err.Error(), "host paths or Docker volumes") {
		t.Fatalf("expected migration mount validation, got %v", err)
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
	taskSvc := tasks.NewService(store.TaskDB())
	svc := NewService(store.AppDB(), runtime, taskSvc, Config{
		Namespace:      "apps",
		Region:         "global",
		Datacenter:     "dc1",
		SaveSessionDir: filepath.Join(dir, "sessions"),
	})
	svc.RegisterTasks(taskSvc)
	svc.SetServerProvider(servers)
	return svc, runtime, servers, func() { _ = store.Close() }
}

func (s *Service) SetBuiltinVariableResolver(resolver BuiltinVariableResolver) {
	s.builtinResolver = resolver
}

func (s *Service) SetInternalFileProvider(provider InternalFileProvider) {
	s.internalFiles = provider
}

func (s *Service) SetServerProvider(provider ServerProvider) {
	s.servers = provider
	if handler, ok := provider.(AgentErrorHandler); ok {
		s.agentErrors = handler
	}
}

func readyServer(id string) server.Server {
	return server.Server{
		ID:          id,
		Name:        id,
		Host:        "127.0.0.1",
		Port:        22,
		SSHUsername: "root",
		DockerHost:  agentcontract.DefaultDockerHost,
		Traits: map[string]string{
			agentcontract.TraitEnabled: "true",
			agentcontract.TraitURL:     "https://" + id + ".agent",
			agentcontract.TraitStatus:  agentcontract.StatusCompatible,
		},
	}
}

type fakeRuntimeClient struct {
	writes               []agentcontract.RuntimeWriteFilesRequest
	deploys              []agentcontract.RuntimeCreateContainerRequest
	pulls                []string
	deletes              []string
	actions              []string
	stops                []agentcontract.RuntimeStopRequest
	restarts             []agentcontract.RuntimeRestartRequest
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
	deployErrByServer    map[string]error
}

func (f *fakeRuntimeClient) RuntimeWriteFiles(ctx context.Context, baseURL string, req agentcontract.RuntimeWriteFilesRequest) error {
	f.writes = append(f.writes, req)
	return nil
}

func (f *fakeRuntimeClient) DockerImagePull(ctx context.Context, baseURL, reference string) error {
	f.pulls = append(f.pulls, reference)
	return nil
}

func (f *fakeRuntimeClient) DockerContainerDelete(ctx context.Context, baseURL, id string) error {
	f.deletes = append(f.deletes, id)
	return nil
}

func (f *fakeRuntimeClient) RuntimeCreateContainer(ctx context.Context, baseURL string, req agentcontract.RuntimeCreateContainerRequest) (agentcontract.RuntimeCreateContainerResponse, error) {
	f.deploys = append(f.deploys, req)
	if f.deployErrByServer != nil {
		if err, ok := f.deployErrByServer[req.ServerID]; ok {
			return agentcontract.RuntimeCreateContainerResponse{}, err
		}
	}
	if f.deployErr != nil {
		return agentcontract.RuntimeCreateContainerResponse{}, f.deployErr
	}
	return agentcontract.RuntimeCreateContainerResponse{ContainerID: "container-" + req.ServerID}, nil
}

func (f *fakeRuntimeClient) DockerContainerAction(ctx context.Context, baseURL, id, action string) error {
	f.actions = append(f.actions, id+":"+action)
	return nil
}

func (f *fakeRuntimeClient) RuntimeStop(ctx context.Context, baseURL string, req agentcontract.RuntimeStopRequest) (agentcontract.RuntimeInstanceResponse, error) {
	f.stops = append(f.stops, req)
	return agentcontract.RuntimeInstanceResponse{InstanceID: req.InstanceID, Status: appruntime.StatusStopped, ObservedAt: time.Now().UTC()}, nil
}

func (f *fakeRuntimeClient) RuntimeRestart(ctx context.Context, baseURL string, req agentcontract.RuntimeRestartRequest) (agentcontract.RuntimeInstanceResponse, error) {
	f.restarts = append(f.restarts, req)
	return agentcontract.RuntimeInstanceResponse{InstanceID: req.InstanceID, Status: appruntime.StatusRunning, ObservedAt: time.Now().UTC()}, nil
}

func (f *fakeRuntimeClient) RuntimeStatus(ctx context.Context, baseURL, instanceID, containerName string) (agentcontract.RuntimeStatusResponse, error) {
	if f.statuses != nil {
		if status, ok := f.statuses[instanceID]; ok {
			return agentcontract.RuntimeStatusResponse{InstanceStatus: status}, nil
		}
	}
	return agentcontract.RuntimeStatusResponse{InstanceStatus: appruntime.InstanceStatus{InstanceID: instanceID, Status: appruntime.StatusRunning, ObservedAt: time.Now().UTC()}}, nil
}

func (f *fakeRuntimeClient) RuntimeLogs(ctx context.Context, baseURL, instanceID, containerName string, tail int) (agentcontract.RuntimeLogsResponse, error) {
	f.logTail = tail
	return agentcontract.RuntimeLogsResponse{InstanceID: instanceID, Logs: f.logs}, nil
}

func (f *fakeRuntimeClient) RuntimePersistentArchive(ctx context.Context, baseURL, applicationID string) (agentcontract.RuntimePersistentArchiveResponse, error) {
	f.archiveBaseURL = baseURL
	f.archiveApplicationID = applicationID
	return agentcontract.RuntimePersistentArchiveResponse{
		ApplicationID: applicationID,
		Filename:      applicationID + "-persistent.zip",
		ContentBase64: base64.StdEncoding.EncodeToString(f.archiveContent),
	}, nil
}

func (f *fakeRuntimeClient) RuntimePersistentRestore(ctx context.Context, baseURL, applicationID string, content []byte) (agentcontract.RuntimePersistentRestoreResponse, error) {
	f.restoreBaseURL = baseURL
	f.restoreApplicationID = applicationID
	f.restoreContent = content
	return agentcontract.RuntimePersistentRestoreResponse{ApplicationID: applicationID, Restored: true}, nil
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

type fakeImageResolver struct {
	result ImageDigestResult
	err    error
}

func (f fakeImageResolver) Resolve(ctx context.Context, image string) (ImageDigestResult, error) {
	return f.result, f.err
}

type fakeInternalFileProvider struct {
	content []byte
}

func (f fakeInternalFileProvider) InternalFileCatalog(ctx context.Context) ([]PanelFileDefinition, error) {
	return nil, nil
}

func (f fakeInternalFileProvider) OpenInternalFile(ctx context.Context, source string) (io.ReadCloser, InternalFileInfo, error) {
	content := append([]byte(nil), f.content...)
	return io.NopCloser(bytes.NewReader(content)), InternalFileInfo{Mode: "0644", Size: int64(len(content))}, nil
}
