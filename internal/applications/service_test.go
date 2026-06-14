package applications

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"panel/internal/config"
	"panel/internal/nomad"
	"panel/internal/panelerr"
	"panel/internal/storage"
	"panel/internal/tasks"
)

func TestCreateDisabledAppStoresRowAndDoesNotCallNomad(t *testing.T) {
	svc, fake, closeStore := newTestService(t)
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
	if len(fake.calls) != 0 {
		t.Fatalf("nomad calls = %#v", fake.calls)
	}
}

func TestCreateEnabledAppValidatesPlansAndRegisters(t *testing.T) {
	svc, fake, closeStore := newTestService(t)
	defer closeStore()
	fake.registerResponse = nomad.RegisterResponse{EvalID: "eval-1"}

	app, err := svc.Create(context.Background(), SaveInput{
		Name:     "web",
		Enabled:  true,
		SpecYAML: "name: web\nimage: nginx\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !app.Enabled || app.LastEvalID != "eval-1" {
		t.Fatalf("app = %#v", app)
	}
	want := []string{"validate:panel-web", "plan:panel-web", "register:panel-web"}
	if !equalStrings(fake.calls, want) {
		t.Fatalf("calls = %#v, want %#v", fake.calls, want)
	}
}

func TestCreateDuplicateAppNameReturnsValidation(t *testing.T) {
	svc, _, closeStore := newTestService(t)
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
	svc, fake, closeStore := newTestService(t)
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
	if app.DeploymentMode != DeploymentModeSelected || !equalStrings(app.DeploymentServers, []string{"srv-a", "srv-b", "srv-c"}) {
		t.Fatalf("deployment targets = %q %#v", app.DeploymentMode, app.DeploymentServers)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("disabled create should not call Nomad, calls = %#v", fake.calls)
	}
}

func TestCreateRejectsArgumentsInCommandArray(t *testing.T) {
	svc, _, closeStore := newTestService(t)
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

func TestRedeployEnabledApplicationsRegistersAllEnabledApps(t *testing.T) {
	svc, fake, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	if _, err := svc.Create(ctx, SaveInput{Name: "enabled", Enabled: true, SpecYAML: "name: enabled\nimage: nginx\n"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(ctx, SaveInput{Name: "disabled", Enabled: false, SpecYAML: "name: disabled\nimage: nginx\n"}); err != nil {
		t.Fatal(err)
	}
	fake.calls = nil

	count, err := svc.RedeployEnabledApplications(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("redeployed = %d, want 1", count)
	}
	if len(fake.calls) < 3 || !equalStrings(fake.calls[:3], []string{"validate:panel-enabled", "plan:panel-enabled", "register:panel-enabled"}) {
		t.Fatalf("calls = %#v", fake.calls)
	}
}

func TestUpdateDisabledAppIncrementsGenerationOnlyWhenSpecHashChanges(t *testing.T) {
	svc, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{Name: "web", SpecYAML: "name: web\nimage: nginx\n"})
	if err != nil {
		t.Fatal(err)
	}
	same, err := svc.Update(ctx, app.ID, SaveInput{Name: "web", SpecYAML: "name: web\nimage: nginx\n"})
	if err != nil {
		t.Fatal(err)
	}
	if same.Generation != 1 {
		t.Fatalf("same generation = %d", same.Generation)
	}
	changed, err := svc.Update(ctx, app.ID, SaveInput{Name: "web", SpecYAML: "name: web\nimage: nginx:1.27\n"})
	if err != nil {
		t.Fatal(err)
	}
	if changed.Generation != 2 {
		t.Fatalf("changed generation = %d", changed.Generation)
	}
}

func TestUpdateDisabledAppToEnabledRegistersExistingSpec(t *testing.T) {
	svc, fake, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	fake.registerResponse = nomad.RegisterResponse{EvalID: "eval-enable"}

	app, err := svc.Create(ctx, SaveInput{Name: "web", Enabled: false, SpecYAML: "name: web\nimage: nginx\n"})
	if err != nil {
		t.Fatal(err)
	}
	enabled, err := svc.Update(ctx, app.ID, SaveInput{Name: "web", Enabled: true, SpecYAML: "name: web\nimage: nginx\n"})
	if err != nil {
		t.Fatal(err)
	}

	if !enabled.Enabled || enabled.LastEvalID != "eval-enable" {
		t.Fatalf("enabled app = %#v", enabled)
	}
	want := []string{"validate:panel-web", "plan:panel-web", "register:panel-web"}
	if !equalStrings(fake.calls, want) {
		t.Fatalf("calls = %#v, want %#v", fake.calls, want)
	}
}

func TestCheckImageUpdateRecordsAvailableDigest(t *testing.T) {
	svc, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	resolver := &fakeImageResolver{digests: []string{"sha256:old", "sha256:new"}}
	svc.SetImageDigestResolver(resolver)

	app, err := svc.Create(ctx, SaveInput{Name: "web", SpecYAML: "name: web\nimage: nginx:latest\n"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := svc.CheckImageUpdate(ctx, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if first.ImageUpdateAvailable || first.ImageDigest != "sha256:old" || first.ImageLatestDigest != "sha256:old" {
		t.Fatalf("first image state = %#v", first)
	}
	second, err := svc.CheckImageUpdate(ctx, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !second.ImageUpdateAvailable || second.ImageDigest != "sha256:old" || second.ImageLatestDigest != "sha256:new" {
		t.Fatalf("second image state = %#v", second)
	}
	if resolver.images[0] != "nginx:latest" {
		t.Fatalf("resolved images = %#v", resolver.images)
	}
}

func TestUpdateImageBumpsGenerationAndRegistersJob(t *testing.T) {
	svc, fake, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	fake.registerResponse = nomad.RegisterResponse{EvalID: "eval-image"}
	svc.SetImageDigestResolver(&fakeImageResolver{digests: []string{"sha256:new"}})

	app, err := svc.Create(ctx, SaveInput{Name: "web", Enabled: true, SpecYAML: "name: web\nimage: nginx:latest\n"})
	if err != nil {
		t.Fatal(err)
	}
	fake.calls = nil
	result, err := svc.UpdateImage(ctx, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Application.Generation != 2 || result.Application.ImageDigest != "sha256:new" || result.EvalID != "eval-image" {
		t.Fatalf("result = %#v", result)
	}
	want := []string{"validate:panel-web", "plan:panel-web", "register:panel-web"}
	if !equalStrings(fake.calls, want) {
		t.Fatalf("calls = %#v, want %#v", fake.calls, want)
	}
	if got := fake.registeredJob.Meta["panel.generation"]; got != "2" {
		t.Fatalf("generation meta = %q", got)
	}
	if got := fake.registeredJob.TaskGroups[0].Tasks[0].Config["force_pull"]; got != true {
		t.Fatalf("force_pull = %#v", got)
	}
}

func TestUpdateEnabledAppToDisabledStopsJob(t *testing.T) {
	svc, fake, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{Name: "web", Enabled: true, SpecYAML: "name: web\nimage: nginx\n"})
	if err != nil {
		t.Fatal(err)
	}
	disabled, err := svc.Update(ctx, app.ID, SaveInput{Name: "web", Enabled: false, SpecYAML: "name: web\nimage: nginx\n"})
	if err != nil {
		t.Fatal(err)
	}

	if disabled.Enabled {
		t.Fatalf("app should be disabled: %#v", disabled)
	}
	if got := fake.calls[len(fake.calls)-1]; got != "stop:panel-web:false" {
		t.Fatalf("last call = %q", got)
	}
}

func TestCreateAppRendersUserAndBuiltinVariables(t *testing.T) {
	svc, _, closeStore := newTestService(t)
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
	job, issues, err := svc.renderApplication(context.Background(), app)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) > 0 {
		t.Fatalf("issues = %#v", issues)
	}
	task := job.TaskGroups[0].Tasks[0]
	if task.Config["image"] != "nginx:1.27" || task.Env["TLS_CERT"] != "CERT" {
		t.Fatalf("rendered task = %#v", task)
	}
	if app.ResolvedVariables["image"] != "nginx:1.27" || app.ResolvedVariables["certs"] == nil {
		t.Fatalf("resolved variables = %#v", app.ResolvedVariables)
	}
}

func TestSaveFileOnEnabledAppRefreshesSnapshotAndRedeploys(t *testing.T) {
	svc, fake, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{Name: "web", Enabled: true, SpecYAML: "name: web\nimage: nginx\n"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SaveFile(ctx, app.ID, FileSaveInput{
		Path:          "config/app.conf",
		Kind:          "template",
		ContentBase64: base64.StdEncoding.EncodeToString([]byte("hello")),
	}); err != nil {
		t.Fatal(err)
	}
	updated, err := svc.Get(ctx, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Generation != 2 || updated.SpecHash == app.SpecHash {
		t.Fatalf("updated app = %#v original hash=%s", updated, app.SpecHash)
	}
	want := []string{"validate:panel-web", "plan:panel-web", "register:panel-web", "validate:panel-web", "plan:panel-web", "register:panel-web"}
	if !equalStrings(fake.calls, want) {
		t.Fatalf("calls = %#v, want %#v", fake.calls, want)
	}
}

func TestDeployApplicationRendersFilesIntoNomadJob(t *testing.T) {
	svc, fake, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	svc.SetBuiltinVariableResolver(fakeBuiltinResolver{
		"certs": map[string]any{
			"example_com": map[string]any{"certificatePem": "CERT"},
		},
	})
	fake.registerResponse = nomad.RegisterResponse{EvalID: "eval-files"}
	app, err := svc.Create(ctx, SaveInput{
		Name:      "web",
		Enabled:   false,
		SpecYAML:  "name: web\nimage: nginx\nmounts:\n  - type: file\n    source: config/app.conf\n    target: /etc/web/app.conf\n    readOnly: true\n  - type: file\n    source: assets/logo.bin\n    target: /usr/share/web/logo.bin\n    readOnly: true\n",
		Variables: map[string]string{"MODE": "prod"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SaveFile(ctx, app.ID, FileSaveInput{
		Path:          "config/app.conf",
		Kind:          "template",
		ContentBase64: base64.StdEncoding.EncodeToString([]byte("mode={{ .vars.MODE }} cert={{ .certs.example_com.certificatePem }}")),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SaveFile(ctx, app.ID, FileSaveInput{
		Path:          "assets/logo.bin",
		Kind:          "binary",
		ContentBase64: base64.StdEncoding.EncodeToString([]byte{0, 1, 2, 3}),
	}); err != nil {
		t.Fatal(err)
	}

	result, err := svc.Deploy(ctx, app.ID)
	if err != nil {
		t.Fatal(err)
	}

	if len(fake.registeredJob.TaskGroups) != 1 || len(fake.registeredJob.TaskGroups[0].Tasks) != 2 {
		t.Fatalf("registered tasks = %#v", fake.registeredJob.TaskGroups)
	}
	initTask := fake.registeredJob.TaskGroups[0].Tasks[0]
	mainTask := fake.registeredJob.TaskGroups[0].Tasks[1]
	if initTask.Lifecycle == nil || initTask.Lifecycle.Hook != "prestart" || len(initTask.Templates) != 1 {
		t.Fatalf("init task = %#v", initTask)
	}
	if !strings.Contains(initTask.Config["args"].([]string)[1], "base64 -d") {
		t.Fatalf("init args = %#v", initTask.Config["args"])
	}
	if len(mainTask.Templates) != 1 || mainTask.Templates[0].EmbeddedTmpl != "mode=prod cert=CERT" {
		t.Fatalf("main templates = %#v", mainTask.Templates)
	}
	mounts, ok := mainTask.Config["mounts"].([]map[string]any)
	if !ok || len(mounts) != 2 || mounts[0]["target"] != "/etc/web/app.conf" || mounts[1]["target"] != "/usr/share/web/logo.bin" {
		t.Fatalf("mounts = %#v", mainTask.Config["mounts"])
	}
	if mounts[0]["source"] != "../alloc/panel-files/config/app.conf" || mounts[1]["source"] != "../alloc/panel-files/assets/logo.bin" {
		t.Fatalf("mount sources = %#v", mounts)
	}
	logs, _, err := svc.tasks.Logs(ctx, result.TaskID, 0)
	if err != nil {
		t.Fatal(err)
	}
	logText := taskLogText(logs)
	if !strings.Contains(logText, "Application file transferred by Nomad to ../alloc/panel-files/assets/logo.bin and mounted at /usr/share/web/logo.bin") {
		t.Fatalf("task logs = %s", logText)
	}
}

func TestRedeployChangedApplicationsRefreshesBuiltinVariables(t *testing.T) {
	svc, fake, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	resolver := fakeBuiltinResolver{
		"certs": map[string]any{
			"example_com": map[string]any{"certificatePem": "OLD"},
		},
	}
	svc.SetBuiltinVariableResolver(resolver)
	app, err := svc.Create(ctx, SaveInput{
		Name:     "web",
		Enabled:  true,
		SpecYAML: "name: web\nimage: nginx\nenv:\n  TLS_CERT: '{{ .certs.example_com.certificatePem }}'\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	resolver["certs"] = map[string]any{
		"example_com": map[string]any{"certificatePem": "NEW"},
	}

	redeployed, err := svc.RedeployChangedApplications(ctx)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := svc.Get(ctx, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if redeployed != 1 || updated.Generation != app.Generation+1 || updated.SpecHash == app.SpecHash {
		t.Fatalf("redeployed=%d updated=%#v original=%#v", redeployed, updated, app)
	}
	task := fake.registeredJob.TaskGroups[0].Tasks[0]
	if task.Env["TLS_CERT"] != "NEW" {
		t.Fatalf("registered env = %#v", task.Env)
	}
}

func TestPersistentMountAddsFixedApplicationBindMount(t *testing.T) {
	svc, fake, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{
		Name:              "web",
		Enabled:           true,
		SpecYAML:          "name: web\nimage: nginx\nmounts:\n  - type: persistent\n    source: data\n    target: /data\n",
		DeploymentMode:    DeploymentModeSelected,
		DeploymentServers: []string{"srv-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	task := fake.registeredJob.TaskGroups[0].Tasks[0]
	mounts, ok := task.Config["mounts"].([]map[string]any)
	if !ok || len(mounts) != 1 {
		t.Fatalf("mounts = %#v", task.Config["mounts"])
	}
	if mounts[0]["source"] != "/opt/panel/apps/"+app.ID+"/persistent/data" || mounts[0]["target"] != "/data" {
		t.Fatalf("mount = %#v", mounts[0])
	}
}

func TestPanelFileMountCreatesReadOnlyNomadTemplate(t *testing.T) {
	svc, fake, closeStore := newTestService(t)
	defer closeStore()
	svc.SetPanelFileProvider(fakePanelFileProvider{content: []byte("CERTIFICATE")})

	if _, err := svc.Create(context.Background(), SaveInput{
		Name: "tls", Enabled: true,
		SpecYAML: "name: tls\nimage: nginx\nmounts:\n  - type: panel_file\n    source: certificate:cert_1:certificate\n    target: /etc/tls/cert.pem\n",
	}); err != nil {
		t.Fatal(err)
	}
	task := fake.registeredJob.TaskGroups[0].Tasks[0]
	if len(task.Templates) != 1 || task.Templates[0].EmbeddedTmpl != "CERTIFICATE" || task.Templates[0].ChangeMode != "restart" {
		t.Fatalf("templates = %#v", task.Templates)
	}
	mounts := existingMounts(task.Config["mounts"])
	if len(mounts) != 1 || mounts[0]["target"] != "/etc/tls/cert.pem" || mounts[0]["readonly"] != true {
		t.Fatalf("mounts = %#v", mounts)
	}
}

func TestDefaultDeploymentTargetsAllNomadClients(t *testing.T) {
	svc, fake, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	if _, err := svc.Create(ctx, SaveInput{Name: "web", Enabled: true, SpecYAML: "name: web\nimage: nginx\n"}); err != nil {
		t.Fatal(err)
	}
	if fake.registeredJob.Type != "system" {
		t.Fatalf("job type = %q", fake.registeredJob.Type)
	}
	if len(fake.registeredJob.TaskGroups) != 1 || fake.registeredJob.TaskGroups[0].Count != 0 {
		t.Fatalf("task groups = %#v", fake.registeredJob.TaskGroups)
	}
}

func TestSelectedDeploymentCreatesOneTaskGroupPerServer(t *testing.T) {
	svc, fake, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	_, err := svc.Create(ctx, SaveInput{
		Name:              "web",
		Enabled:           true,
		SpecYAML:          "name: web\nimage: nginx\n",
		DeploymentMode:    DeploymentModeSelected,
		DeploymentServers: []string{"srv-b", "srv-a", "srv-a"},
	})
	if err != nil {
		t.Fatal(err)
	}
	job := fake.registeredJob
	if job.Type != "service" || len(job.TaskGroups) != 2 {
		t.Fatalf("job = %#v", job)
	}
	got := []string{job.TaskGroups[0].Constraints[len(job.TaskGroups[0].Constraints)-1].RTarget, job.TaskGroups[1].Constraints[len(job.TaskGroups[1].Constraints)-1].RTarget}
	if !equalStrings(got, []string{"srv-a", "srv-b"}) {
		t.Fatalf("target constraints = %#v", got)
	}
	for _, group := range job.TaskGroups {
		constraint := group.Constraints[len(group.Constraints)-1]
		if constraint.LTarget != "${meta.panel_server_id}" || constraint.Operand != "=" {
			t.Fatalf("constraint = %#v", constraint)
		}
	}
}

func TestPersistentDeploymentRequiresExactlyOneServer(t *testing.T) {
	svc, _, closeStore := newTestService(t)
	defer closeStore()

	_, err := svc.Create(context.Background(), SaveInput{
		Name:           "db",
		SpecYAML:       "name: db\nimage: postgres\nmounts:\n  - type: persistent\n    source: data\n    target: /var/lib/postgresql/data\n",
		DeploymentMode: DeploymentModeAll,
	})
	if err == nil {
		t.Fatal("expected persistent all-server deployment to be rejected")
	}
}

func TestPackageApplicationIncludesSpecVariablesAndFiles(t *testing.T) {
	svc, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{
		Name:      "web",
		SpecYAML:  "name: web\nimage: '{{ .vars.image }}'\n",
		Variables: map[string]string{"image": "nginx", "domain": "example.com"},
		ReverseProxy: []ReverseProxyRule{{
			Domain:     "{{ .vars.domain }}",
			TargetPort: 8080,
			Paths:      []ReverseProxyPath{{Path: "/", WebSocket: true}, {Path: "/api"}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SaveFile(ctx, app.ID, FileSaveInput{
		Path:          "config/app.conf",
		Kind:          "template",
		ContentBase64: base64.StdEncoding.EncodeToString([]byte("hello")),
	}); err != nil {
		t.Fatal(err)
	}
	pkg, err := svc.Package(ctx, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(pkg.Content), int64(len(pkg.Content)))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for _, file := range zr.File {
		rc, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		raw, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		files[file.Name] = string(raw)
	}
	if files["spec.yaml"] != app.SpecYAML || files["files/config/app.conf"] != "hello" || !strings.Contains(files["variables.json"], "nginx") || !strings.Contains(files["application.json"], `"name": "web"`) {
		t.Fatalf("package files = %#v", files)
	}
	if !strings.Contains(files["resolved_variables.json"], "nginx") {
		t.Fatalf("resolved variables package = %s", files["resolved_variables.json"])
	}
	if !strings.Contains(files["application.json"], "{{ .vars.domain }}") {
		t.Fatalf("application metadata should keep proxy template source = %s", files["application.json"])
	}
	nginx := files["nginx/panel-web.conf"]
	for _, want := range []string{"server_name example.com;", "proxy_pass http://127.0.0.1:8080;", "location /api", "proxy_set_header Upgrade $http_upgrade;"} {
		if !strings.Contains(nginx, want) {
			t.Fatalf("nginx config missing %q:\n%s", want, nginx)
		}
	}
}

func TestSaveSessionCommitsConfigAndFinalFiles(t *testing.T) {
	svc, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{Name: "web", SpecYAML: "name: web\nimage: nginx\n"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SaveFile(ctx, app.ID, FileSaveInput{
		Path:          "config/old.conf",
		Kind:          "template",
		ContentBase64: base64.StdEncoding.EncodeToString([]byte("old")),
	}); err != nil {
		t.Fatal(err)
	}
	app, err = svc.Get(ctx, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	session, err := svc.BeginSaveSession(ctx, BeginSaveSessionInput{
		ApplicationID: app.ID,
		Save: SaveInput{
			Name:              "web",
			SpecYAML:          "name: web\nimage: nginx:1.28\nmounts:\n  - type: persistent\n    source: data\n    target: /data\n",
			Variables:         map[string]string{"MODE": "prod"},
			DeploymentMode:    DeploymentModeSelected,
			DeploymentServers: []string{"srv-1"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(session.Files) != 1 || session.Files[0].Path != "config/old.conf" {
		t.Fatalf("session files = %#v", session.Files)
	}
	if err := svc.DeleteSaveSessionFile(ctx, session.ID, FileDeleteInput{Path: "config/old.conf"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UploadSaveSessionFile(ctx, session.ID, FileSaveInput{
		Path:          "config/new.conf",
		Kind:          "template",
		ContentBase64: base64.StdEncoding.EncodeToString([]byte("mode={{ .vars.MODE }}")),
	}); err != nil {
		t.Fatal(err)
	}
	updated, err := svc.CommitSaveSession(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Generation != app.Generation+1 || updated.PersistentPath != "/opt/panel/apps/"+app.ID+"/persistent" || updated.Variables["MODE"] != "prod" {
		t.Fatalf("updated app = %#v", updated)
	}
	files, err := svc.listFiles(ctx, app.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path != "config/new.conf" || string(files[0].Content) != "mode={{ .vars.MODE }}" {
		t.Fatalf("files = %#v", files)
	}
	if _, err := svc.CommitSaveSession(ctx, session.ID); err == nil {
		t.Fatal("expected committed session to be discarded")
	}
}

func TestStopAppCallsNomadAndDisablesApp(t *testing.T) {
	svc, fake, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{Name: "web", Enabled: true, SpecYAML: "name: web\nimage: nginx\n"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Stop(ctx, app.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	stopped, err := svc.Get(ctx, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stopped.Enabled {
		t.Fatalf("app should be disabled: %#v", stopped)
	}
	if got := fake.calls[len(fake.calls)-1]; got != "stop:panel-web:false" {
		t.Fatalf("last call = %q", got)
	}
}

func TestRuntimeMapsNomadState(t *testing.T) {
	svc, fake, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{Name: "web", Enabled: true, SpecYAML: "name: web\nimage: nginx\n"})
	if err != nil {
		t.Fatal(err)
	}
	fake.deployment = nomad.Deployment{ID: "dep-1", JobID: "panel-web", Status: "running"}
	fake.allocations = []nomad.AllocationListItem{{ID: "alloc-1", JobID: "panel-web", ClientStatus: "running"}}
	fake.evaluations = []nomad.Evaluation{{ID: "eval-1", JobID: "panel-web", Status: "complete"}}
	fake.evaluationDetails = map[string]nomad.Evaluation{"eval-1": {ID: "eval-1", StatusDescription: "complete"}}

	runtime, err := svc.Runtime(ctx, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.JobStatus != "running" || runtime.Deployment == nil || runtime.Deployment.ID != "dep-1" {
		t.Fatalf("runtime = %#v", runtime)
	}
	if len(runtime.Allocations) != 1 || len(runtime.Evaluations) != 1 || len(runtime.EvaluationDetails) != 1 {
		t.Fatalf("runtime = %#v", runtime)
	}
}

func TestRuntimeTreatsSuccessfulDeploymentWithRunningAllocationsAsRunning(t *testing.T) {
	svc, fake, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{Name: "web", Enabled: true, SpecYAML: "name: web\nimage: nginx\n"})
	if err != nil {
		t.Fatal(err)
	}
	fake.deployment = nomad.Deployment{ID: "dep-1", JobID: "panel-web", Status: "successful"}
	fake.allocations = []nomad.AllocationListItem{{ID: "alloc-1", JobID: "panel-web", ClientStatus: "running"}}

	runtime, err := svc.Runtime(ctx, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.JobStatus != "running" {
		t.Fatalf("runtime status = %q, want running", runtime.JobStatus)
	}
}

func TestRuntimeTreatsBlockedDeploymentWithoutRunningAllocationsAsFailed(t *testing.T) {
	svc, fake, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{Name: "web", Enabled: true, SpecYAML: "name: web\nimage: nginx\n"})
	if err != nil {
		t.Fatal(err)
	}
	fake.deployment = nomad.Deployment{ID: "dep-1", JobID: "panel-web", Status: "blocked"}
	fake.allocations = []nomad.AllocationListItem{{ID: "alloc-1", JobID: "panel-web", ClientStatus: "pending"}}

	runtime, err := svc.Runtime(ctx, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.JobStatus != "failed" {
		t.Fatalf("runtime status = %q, want failed", runtime.JobStatus)
	}
}

func newTestService(t *testing.T) (*Service, *fakeNomadClient, func()) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.Nomad.Namespace = "apps"
	cfg.Nomad.Region = "global"
	cfg.Nomad.Datacenter = "dc1"
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeNomadClient{}
	svc := NewService(store.AppDB(), fake, tasks.NewService(store.AppDB()), Config{
		Namespace:      cfg.Nomad.Namespace,
		Region:         cfg.Nomad.Region,
		Datacenter:     cfg.Nomad.Datacenter,
		SaveSessionDir: filepath.Join(dir, "sessions"),
	})
	return svc, fake, func() { _ = store.Close() }
}

type fakeNomadClient struct {
	calls             []string
	registerResponse  nomad.RegisterResponse
	registeredJob     nomad.Job
	deployment        nomad.Deployment
	allocations       []nomad.AllocationListItem
	evaluations       []nomad.Evaluation
	evaluationDetails map[string]nomad.Evaluation
}

type fakeImageResolver struct {
	digests []string
	images  []string
}

func (f *fakeImageResolver) Resolve(ctx context.Context, image string) (ImageDigestResult, error) {
	f.images = append(f.images, image)
	digest := "sha256:latest"
	if len(f.digests) > 0 {
		digest = f.digests[0]
		f.digests = f.digests[1:]
	}
	return ImageDigestResult{Reference: "registry-1.docker.io/library/" + image, Registry: "registry-1.docker.io", Repository: "library/" + image, Tag: "latest", Digest: digest}, nil
}

func (f *fakeNomadClient) ValidateJob(ctx context.Context, job nomad.Job) (nomad.ValidateResponse, error) {
	f.calls = append(f.calls, "validate:"+job.ID)
	return nomad.ValidateResponse{DriverConfigValidated: true}, nil
}

func (f *fakeNomadClient) PlanJob(ctx context.Context, id string, job nomad.Job) (nomad.PlanResponse, error) {
	f.calls = append(f.calls, "plan:"+id)
	return nomad.PlanResponse{}, nil
}

func (f *fakeNomadClient) RegisterJob(ctx context.Context, id string, job nomad.Job) (nomad.RegisterResponse, error) {
	f.calls = append(f.calls, "register:"+id)
	f.registeredJob = job
	return f.registerResponse, nil
}

func (f *fakeNomadClient) StopJob(ctx context.Context, id string, purge bool) (nomad.StopResponse, error) {
	f.calls = append(f.calls, "stop:"+id+":"+boolString(purge))
	return nomad.StopResponse{}, nil
}

func (f *fakeNomadClient) JobAllocations(ctx context.Context, id string) ([]nomad.AllocationListItem, error) {
	return f.allocations, nil
}

func (f *fakeNomadClient) JobDeployment(ctx context.Context, id string) (nomad.Deployment, error) {
	return f.deployment, nil
}

func (f *fakeNomadClient) JobEvaluations(ctx context.Context, id string) ([]nomad.Evaluation, error) {
	return f.evaluations, nil
}

func (f *fakeNomadClient) Evaluation(ctx context.Context, id string) (nomad.Evaluation, error) {
	if f.evaluationDetails != nil {
		return f.evaluationDetails[id], nil
	}
	return nomad.Evaluation{ID: id}, nil
}

func (f *fakeNomadClient) RestartAllocation(ctx context.Context, allocID, task string) error {
	f.calls = append(f.calls, "restart:"+allocID+":"+task)
	return nil
}

func (f *fakeNomadClient) AllocationLogs(ctx context.Context, allocID, task, logType string, tail int) (string, error) {
	f.calls = append(f.calls, "logs:"+allocID+":"+task+":"+logType)
	return "hello", nil
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func taskLogText(logs []tasks.Log) string {
	parts := make([]string, 0, len(logs))
	for _, log := range logs {
		parts = append(parts, log.Line)
	}
	return strings.Join(parts, "\n")
}

func boolString(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

type fakeBuiltinResolver map[string]any

func (f fakeBuiltinResolver) BuiltinVariables(ctx context.Context) (map[string]any, error) {
	return map[string]any(f), nil
}

type fakePanelFileProvider struct {
	content []byte
}

func (f fakePanelFileProvider) PanelFileCatalog(context.Context) ([]PanelFileDefinition, error) {
	return []PanelFileDefinition{{ID: "cert_1:certificate", ResourceID: "cert_1", ResourceType: "test", Name: "test", Kind: "certificate", Source: "certificate:cert_1:certificate"}}, nil
}

func (f fakePanelFileProvider) ReadPanelFile(context.Context, string) ([]byte, error) {
	return append([]byte(nil), f.content...), nil
}
