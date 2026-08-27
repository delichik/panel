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
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	agentcontract "panel/internal/agent/contract"
	"panel/internal/modules/applications/runtime"
	"panel/internal/modules/servers"
	"panel/internal/modules/tasks"
	"panel/internal/platform/config"
	storage "panel/internal/platform/database"
	"panel/internal/platform/database/models"
	"panel/internal/platform/database/orm"
	panelerr "panel/internal/platform/errors"
)

func jobsForApplication(t *testing.T, svc *Service, appID string) []models.Job {
	t.Helper()
	var jobs []models.Job
	if err := orm.New(svc.db).From("jobs").Where("application_id=?", appID).All(context.Background(), &jobs); err != nil {
		t.Fatal(err)
	}
	return jobs
}

func applyJobSpecsForApplication(t *testing.T, svc *Service, appID string) []map[string]any {
	t.Helper()
	jobs := jobsForApplication(t, svc, appID)
	var out []map[string]any
	for _, job := range jobs {
		if job.Action != "apply" || len(job.DesiredSpecJSON) == 0 {
			continue
		}
		out = append(out, job.DesiredSpecJSON)
	}
	if len(out) == 0 {
		t.Fatalf("expected apply Jobs for application %s, got %#v", appID, jobs)
	}
	return out
}

// markRuntimeInstanceSatisfied 把 planner 创建的实例行补齐为“已部署成功”的状态：
// markRuntimeInstance 只 UPDATE 已存在的行，而 planner 新建的实例行
// last_deployed_generation=0、runtime_spec_json='{}'，无法通过
// runtimeInstanceSatisfiesDesired 的判定。
func markRuntimeInstanceSatisfied(t *testing.T, svc *Service, appID, serverID string, generation int, specHash, status, lastErr string) {
	t.Helper()
	runtimeSpec, _ := json.Marshal(map[string]any{"specHash": specHash})
	if _, err := svc.db.Exec(`UPDATE application_instances SET desired_state=?,status=?,runtime_spec_json=?,last_deployed_generation=?,last_error=? WHERE id=?`,
		appruntime.DesiredRunning, status, string(runtimeSpec), generation, lastErr, runtimeInstanceID(appID, serverID)); err != nil {
		t.Fatal(err)
	}
}

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

func TestDeployEnablesDisabledApplicationWithVersionCAS(t *testing.T) {
	svc, runtime, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{
		Name:     "web",
		Enabled:  false,
		SpecYAML: "name: web\nimage: nginx\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	beforeVersion := app.Version
	if _, err := svc.Deploy(ctx, app.ID); err != nil {
		t.Fatal(err)
	}
	after, err := svc.Get(ctx, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !after.Enabled || after.Version != beforeVersion+1 {
		t.Fatalf("HTTP deploy should explicitly persist enable intent with version bump, before=%d after=%#v", beforeVersion, after)
	}
	// 新控制面：Deploy 只写 desired/Job 并触发协调（fake trigger 直接 plan、
	// 不返回 task），绝不直接调用 agent runtime。
	if len(jobsForApplication(t, svc, app.ID)) == 0 {
		t.Fatal("expected deploy to plan application Jobs")
	}
	if len(runtime.deploys) != 0 {
		t.Fatalf("deploy must not call agent runtime directly, deploys = %#v", runtime.deploys)
	}
}

func TestListSummariesDoesNotLoadApplicationDetails(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{
		Name:     "web",
		Enabled:  false,
		SpecYAML: "name: web\nimage: nginx\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := svc.db.ExecContext(ctx, `UPDATE applications SET enabled=1,spec_yaml=?,image_update_available=1 WHERE id=?`,
		"not: [valid", app.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.db.ExecContext(ctx, `INSERT INTO application_instances(id,application_id,server_id,container_name,container_id,desired_state,status,runtime_spec_json,last_deployed_generation,last_error,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, "instance-1", app.ID, "srv-a", "panel-web", "container-1", "running", appruntime.StatusRunning, "not-json", 1, "", now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.db.ExecContext(ctx, `INSERT INTO application_instances(id,application_id,server_id,container_name,container_id,desired_state,status,runtime_spec_json,last_deployed_generation,last_error,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, "instance-2", app.ID, "srv-b", "panel-web", "container-2", "running", appruntime.StatusRunning, "not-json", 1, "", now, now); err != nil {
		t.Fatal(err)
	}

	page, err := svc.ListSummaries(ctx, 1, 50, "")
	if err != nil {
		t.Fatal(err)
	}
	summaries := page.Items
	if len(summaries) != 1 || page.Total != 1 {
		t.Fatalf("summaries = %#v", page)
	}
	if summaries[0].ID != app.ID || summaries[0].RuntimeStatus != appruntime.StatusRunning || !summaries[0].ImageUpdateAvailable {
		t.Fatalf("summary = %#v", summaries[0])
	}
	if summaries[0].InstanceCount != 2 {
		t.Fatalf("summary instance count = %d", summaries[0].InstanceCount)
	}
}

func TestCreateEnabledAppDeploysToAgentRuntime(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
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
	jobs := jobsForApplication(t, svc, app.ID)
	if len(jobs) != 2 {
		t.Fatalf("expected one Job per server, got %#v", jobs)
	}
	for _, job := range jobs {
		spec := job.DesiredSpecJSON
		if spec["containerName"] != "panel-web" {
			t.Fatalf("container name = %#v", spec["containerName"])
		}
		env, _ := spec["env"].(map[string]any)
		if env == nil || env["PANEL_SERVER_ID"] == nil {
			t.Fatalf("missing PANEL_SERVER_ID env in %#v", spec)
		}
	}
}

func TestCreateEnabledAnyTLSHostNetworkAppDeploysRuntime(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()

	app, err := svc.Create(context.Background(), SaveInput{
		Name:    "anytls",
		Enabled: true,
		SpecYAML: `name: anytls
image: jiasongji/anytls
networkMode: host
command:
  - "/app/anytls-server"
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
	if !app.Enabled {
		t.Fatalf("app = %#v", app)
	}
	jobs := jobsForApplication(t, svc, app.ID)
	if len(jobs) != 2 {
		t.Fatalf("expected one Job per server, got %#v", jobs)
	}
	spec := jobs[0].DesiredSpecJSON
	if spec["networkMode"] != "host" {
		t.Fatalf("runtime network = %#v", spec["networkMode"])
	}
	command, _ := spec["command"].([]any)
	if len(command) != 5 || command[0] != "/app/anytls-server" || command[2] != ":9443" {
		t.Fatalf("command = %#v", command)
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

func TestCreateDisabledSelectedApplicationWithCommandStoresTargets(t *testing.T) {
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

func TestRedeployEnabledApplicationsDeploysEnabledApps(t *testing.T) {
	svc, runtime, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	enabledApp, err := svc.Create(ctx, SaveInput{Name: "enabled", Enabled: true, SpecYAML: "name: enabled\nimage: nginx\n"})
	if err != nil {
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
	if len(runtime.deploys) != 0 {
		t.Fatalf("redeploy must not mutate runtime directly, deploys = %#v", runtime.deploys)
	}
	if len(jobsForApplication(t, svc, enabledApp.ID)) != 2 {
		t.Fatalf("expected redeploy to plan Jobs for enabled application")
	}
}

func TestPlanApplicationDeploymentSkipsSatisfiedTargetsUnlessForced(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{Name: "web", Enabled: true, SpecYAML: "name: web\nimage: nginx\n"})
	if err != nil {
		t.Fatal(err)
	}
	// 先规划一次，让 planner 创建实例行与 Job。
	first, err := svc.PlanApplicationDeployment(ctx, DeploymentPlanRequest{ApplicationID: app.ID, TriggerType: "system"})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.JobIDs) != 2 {
		t.Fatalf("expected initial plan to create one Job per server, got %#v", first)
	}
	// markRuntimeInstance 只 UPDATE 已存在的行，且 planner 新建的实例行
	// last_deployed_generation=0、runtime_spec_json='{}'；要让 srv-a 被判定为
	// “已满足”，需把 generation/spec hash 补齐到与期望一致。
	desired := jobsForApplication(t, svc, app.ID)[0]
	markRuntimeInstanceSatisfied(t, svc, app.ID, "srv-a", desired.DesiredGeneration, desired.DesiredSpecHash, appruntime.StatusRunning, "")
	markRuntimeInstanceSatisfied(t, svc, app.ID, "srv-b", desired.DesiredGeneration, desired.DesiredSpecHash, appruntime.StatusFailed, "boom")

	plan, err := svc.PlanApplicationDeployment(ctx, DeploymentPlanRequest{ApplicationID: app.ID, TriggerType: "scheduler"})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.JobIDs) != 1 {
		t.Fatalf("expected only the unsatisfied target to reconcile, got %#v", plan)
	}
	plan, err = svc.PlanApplicationDeployment(ctx, DeploymentPlanRequest{ApplicationID: app.ID, Force: true, TriggerType: "system"})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.JobIDs) != 2 {
		t.Fatalf("forced redeploy should include all targets, got %#v", plan)
	}
}

func TestDeployReturnsBeforeRuntimeDeploymentCompletes(t *testing.T) {
	svc, runtime, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{Name: "web", Enabled: false, SpecYAML: "name: web\nimage: nginx\n"})
	if err != nil {
		t.Fatal(err)
	}
	// 新控制面：远端部署由 orchestrator 的 RuntimeReconcile 执行；测试未启动
	// controller，因此 Deploy 必须立即返回，只留下 desired/Job，绝不直接调用
	// fake runtime。
	result, err := svc.Deploy(ctx, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Application.Enabled {
		t.Fatalf("deploy result = %#v", result)
	}
	if len(jobsForApplication(t, svc, app.ID)) == 0 {
		t.Fatal("expected Deploy to persist application Jobs")
	}
	if len(runtime.deploys) != 0 || len(runtime.writes) != 0 || len(runtime.pulls) != 0 || len(runtime.stops) != 0 {
		t.Fatalf("Deploy must not call agent runtime directly: deploys=%#v writes=%#v pulls=%#v stops=%#v", runtime.deploys, runtime.writes, runtime.pulls, runtime.stops)
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
	// 新控制面：刷新任务只触发 plan 写 Job，渲染结果体现在 Job 的
	// DesiredSpecJSON，远端执行由 orchestrator 负责。
	jobs := jobsForApplication(t, svc, app.ID)
	if len(jobs) == 0 {
		t.Fatal("expected refresh to plan application Jobs")
	}
	for _, job := range jobs {
		env, _ := job.DesiredSpecJSON["env"].(map[string]any)
		if env == nil || env["MESSAGE"] != "two" {
			t.Fatalf("job env = %#v", job.DesiredSpecJSON["env"])
		}
	}
	if len(runtime.deploys) != 0 {
		t.Fatalf("refresh must not call agent runtime directly, deploys = %#v", runtime.deploys)
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
	registry := NewApplicationVariableRegistry()
	registry.Register("certs", fakeVariableSource{value: map[string]any{
		"example_com": map[string]any{
			"certificatePem": "CERT",
		},
	}})
	svc.SetBuiltinVariableResolver(registry)

	app, err := svc.Create(context.Background(), SaveInput{
		Name:     "web",
		SpecYAML: "name: web\nimage: 'nginx:1.27'\nenv:\n  APP_NAME: '{{ .app.name }}'\n  TLS_CERT: '{{ .certs.example_com.certificatePem }}'\n",
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
	if spec.Image != "nginx:1.27" || spec.Env["APP_NAME"] != "web" || spec.Env["TLS_CERT"] != "CERT" {
		t.Fatalf("rendered spec = %#v", spec)
	}
}

func TestApplicationFileMountCreatesManagedRuntimeFile(t *testing.T) {
	svc, runtime, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{
		Name:     "web",
		Enabled:  false,
		SpecYAML: "name: web\nimage: nginx\nmounts:\n  - type: file\n    source: config-app.conf\n    target: /etc/app.conf\n    uid: 1000\n    gid: 1001\n    mode: \"0755\"\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	session, err := svc.BeginEditSession(ctx, "admin", BeginEditSessionInput{ApplicationID: app.ID, Draft: &SaveInput{Name: "web", Enabled: true, SpecYAML: app.SpecYAML}})
	if err != nil {
		t.Fatal(err)
	}
	session, err = svc.PutEditSessionFile(ctx, "admin", session.ID, "config-app.conf", "file-op-1", EditSessionFileInput{
		Revision:          session.Revision,
		ClientOperationID: "file-op-1",
		Path:              "config-app.conf",
		Kind:              ApplicationFileKindTemplate,
		ContentBase64:     base64.StdEncoding.EncodeToString([]byte("server={{ .app.name }}")),
	})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := svc.PreviewEditSession(ctx, "admin", session.ID, session.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CommitEditSession(ctx, "admin", session.ID, "commit-1", CommitEditSessionInput{Revision: session.Revision, BaseResourceVersion: session.BaseResourceVersion.Value, PreviewToken: preview.Token.Value}); err != nil {
		t.Fatal(err)
	}
	stored, err := svc.ListFiles(ctx, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	var file ApplicationFile
	for _, item := range stored {
		if item.Name == "config-app.conf" {
			file = item
		}
	}
	if file.ID == "" {
		t.Fatal("managed file not stored")
	}
	// 新控制面：提交只触发 plan 写 Job，渲染结果体现在 Job 的 DesiredSpecJSON。
	spec := applyJobSpecsForApplication(t, svc, app.ID)[0]
	allocation := applicationFileAllocationName(file.ID)
	files, _ := spec["files"].([]any)
	if len(files) != 1 {
		t.Fatalf("files = %#v", spec["files"])
	}
	managed := files[0].(map[string]any)
	content, err := base64.StdEncoding.DecodeString(managed["content"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if managed["path"] != allocation || string(content) != "server=web" {
		t.Fatalf("files = %#v", spec["files"])
	}
	if managed["mode"] != "0755" || managed["uid"].(float64) != 1000 || managed["gid"].(float64) != 1001 {
		t.Fatalf("file permissions = %#v", managed)
	}
	mounts, _ := spec["mounts"].([]any)
	if len(mounts) != 1 {
		t.Fatalf("mounts = %#v", spec["mounts"])
	}
	mount := mounts[0].(map[string]any)
	if mount["type"] != "managed_file" || mount["source"] != allocation || mount["target"] != "/etc/app.conf" {
		t.Fatalf("mounts = %#v", spec["mounts"])
	}
	if len(runtime.deploys) != 0 {
		t.Fatalf("commit must not call agent runtime directly, deploys = %#v", runtime.deploys)
	}
}

func TestApplicationArchiveFileMountDeploysSingleManagedArchive(t *testing.T) {
	svc, runtimeClient, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	archive := testApplicationZipArchive(t, "index.html", "<h1>ok</h1>")
	session, err := svc.BeginEditSession(ctx, "admin", BeginEditSessionInput{Draft: &SaveInput{
		Name:     "web",
		Enabled:  true,
		SpecYAML: "name: web\nimage: nginx\nmounts:\n  - type: file\n    source: public\n    target: /usr/share/nginx/html\n    readOnly: true\n",
	}})
	if err != nil {
		t.Fatal(err)
	}
	session, err = svc.UploadEditSessionArchive(ctx, "admin", session.ID, "archive-op-1", EditSessionArchiveInput{
		Revision:          session.Revision,
		ClientOperationID: "archive-op-1",
		Name:              "public",
		Kind:              "binary",
		FileName:          "site.zip",
		Content:           archive,
	})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := svc.PreviewEditSession(ctx, "admin", session.ID, session.Revision)
	if err != nil {
		t.Fatal(err)
	}
	commitResult, err := svc.CommitEditSession(ctx, "admin", session.ID, "commit-1", CommitEditSessionInput{Revision: session.Revision, BaseResourceVersion: "0", PreviewToken: preview.Token.Value})
	if err != nil {
		t.Fatal(err)
	}
	stored, err := svc.ListFiles(ctx, commitResult.Application.ID)
	if err != nil {
		t.Fatal(err)
	}
	var file ApplicationFile
	for _, item := range stored {
		if item.Name == "public" {
			file = item
		}
	}
	if file.ID == "" {
		t.Fatal("archive file not stored")
	}
	// 新控制面：提交只触发 plan 写 Job，渲染结果体现在 Job 的 DesiredSpecJSON。
	spec := applyJobSpecsForApplication(t, svc, commitResult.Application.ID)[0]
	allocation := applicationFileAllocationName(file.ID)
	files, _ := spec["files"].([]any)
	if len(files) != 1 {
		t.Fatalf("files = %#v", spec["files"])
	}
	managed := files[0].(map[string]any)
	content, err := base64.StdEncoding.DecodeString(managed["content"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if managed["kind"] != appruntime.ManagedFileKindArchive || managed["path"] != allocation || !bytes.Equal(content, archive) {
		t.Fatalf("files = %#v", spec["files"])
	}
	mounts, _ := spec["mounts"].([]any)
	if len(mounts) != 1 {
		t.Fatalf("mounts = %#v", spec["mounts"])
	}
	mount := mounts[0].(map[string]any)
	if mount["type"] != "managed_file" || mount["source"] != allocation || mount["target"] != "/usr/share/nginx/html" || mount["readOnly"] != true {
		t.Fatalf("mounts = %#v", spec["mounts"])
	}
	if len(runtimeClient.deploys) != 0 {
		t.Fatalf("commit must not call agent runtime directly, deploys = %#v", runtimeClient.deploys)
	}
}

func TestApplicationFileTemplateRendersPerTargetServerVariables(t *testing.T) {
	svc, runtime, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{
		Name:     "web",
		Enabled:  false,
		SpecYAML: "name: web\nimage: nginx\nmounts:\n  - type: file\n    source: config-node.conf\n    target: /etc/node.conf\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	session, err := svc.BeginEditSession(ctx, "admin", BeginEditSessionInput{ApplicationID: app.ID, Draft: &SaveInput{Name: app.Name, Enabled: true, SpecYAML: app.SpecYAML}})
	if err != nil {
		t.Fatal(err)
	}
	session, err = svc.PutEditSessionFile(ctx, "admin", session.ID, "config-node.conf", "file-op-1", EditSessionFileInput{
		Revision:          session.Revision,
		ClientOperationID: "file-op-1",
		Path:              "config-node.conf",
		Kind:              ApplicationFileKindTemplate,
		ContentBase64:     base64.StdEncoding.EncodeToString([]byte("name={{ .server.name }} role={{ index .server.variables \"role\" }} app={{ .app.name }}")),
	})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := svc.PreviewEditSession(ctx, "admin", session.ID, session.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CommitEditSession(ctx, "admin", session.ID, "commit-1", CommitEditSessionInput{Revision: session.Revision, BaseResourceVersion: session.BaseResourceVersion.Value, PreviewToken: preview.Token.Value}); err != nil {
		t.Fatal(err)
	}

	// 新控制面：planner 按服务器渲染模板并写入各 Job 的 DesiredSpecJSON。
	byServer := map[string]string{}
	for _, job := range jobsForApplication(t, svc, app.ID) {
		if job.Action != "apply" || len(job.DesiredSpecJSON) == 0 {
			continue
		}
		files, _ := job.DesiredSpecJSON["files"].([]any)
		if len(files) != 1 {
			t.Fatalf("files = %#v", job.DesiredSpecJSON["files"])
		}
		managed := files[0].(map[string]any)
		content, err := base64.StdEncoding.DecodeString(managed["content"].(string))
		if err != nil {
			t.Fatal(err)
		}
		byServer[job.ServerID] = string(content)
	}
	if byServer["srv-a"] != "name=srv-a role=srv-a-role app=web" || byServer["srv-b"] != "name=srv-b role=srv-b-role app=web" {
		t.Fatalf("rendered files by server = %#v", byServer)
	}
	if len(runtime.deploys) != 0 {
		t.Fatalf("commit must not call agent runtime directly, deploys = %#v", runtime.deploys)
	}
}

func TestPanelFileMountCreatesReadOnlyRuntimeFile(t *testing.T) {
	svc, runtime, _, closeStore := newTestService(t)
	defer closeStore()
	svc.SetInternalFileProvider(fakeInternalFileProvider{content: []byte("CERTIFICATE")})

	app, err := svc.Create(context.Background(), SaveInput{
		Name:     "tls",
		Enabled:  true,
		SpecYAML: "name: tls\nimage: nginx\nmounts:\n  - type: panel_file\n    source: key_asset:cert_1:certificate\n    target: /etc/tls/cert.pem\n    readOnly: true\n    uid: 1000\n    gid: 1001\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	// 新控制面：Create 只写 desired/Job，渲染结果体现在 Job 的 DesiredSpecJSON。
	spec := applyJobSpecsForApplication(t, svc, app.ID)[0]
	files, _ := spec["files"].([]any)
	if len(files) != 1 {
		t.Fatalf("files = %#v", spec["files"])
	}
	managed := files[0].(map[string]any)
	content, err := base64.StdEncoding.DecodeString(managed["content"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "CERTIFICATE" || managed["mode"] != "0644" {
		t.Fatalf("files = %#v", spec["files"])
	}
	if managed["uid"].(float64) != 1000 || managed["gid"].(float64) != 1001 {
		t.Fatalf("file ownership = %#v", managed)
	}
	mounts, _ := spec["mounts"].([]any)
	if len(mounts) != 1 {
		t.Fatalf("mounts = %#v", spec["mounts"])
	}
	mount := mounts[0].(map[string]any)
	if mount["target"] != "/etc/tls/cert.pem" || mount["readOnly"] != true {
		t.Fatalf("mounts = %#v", spec["mounts"])
	}
	if len(runtime.deploys) != 0 {
		t.Fatalf("create must not call agent runtime directly, deploys = %#v", runtime.deploys)
	}
}

func TestSelectedDeploymentDeploysOneInstancePerSelectedServer(t *testing.T) {
	svc, runtime, _, closeStore := newTestService(t)
	defer closeStore()

	app, err := svc.Create(context.Background(), SaveInput{
		Name:              "web",
		Enabled:           true,
		SpecYAML:          "name: web\nimage: nginx\n",
		DeploymentMode:    DeploymentModeSelected,
		DeploymentServers: []string{"srv-b", "srv-a", "srv-a"},
	})
	if err != nil {
		t.Fatal(err)
	}
	// 新控制面：Create 为每个去重后的目标服务器写一个 apply Job。
	jobs := jobsForApplication(t, svc, app.ID)
	if len(jobs) != 2 {
		t.Fatalf("expected one Job per selected server, got %#v", jobs)
	}
	servers := []string{jobs[0].ServerID, jobs[1].ServerID}
	sort.Strings(servers)
	if !reflect.DeepEqual(servers, []string{"srv-a", "srv-b"}) {
		t.Fatalf("deploy targets = %#v", servers)
	}
	if len(runtime.deploys) != 0 {
		t.Fatalf("create must not call agent runtime directly, deploys = %#v", runtime.deploys)
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

func TestPersistentPathIsDerivedAfterUpdate(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{
		Name:              "db",
		Enabled:           false,
		SpecYAML:          "name: db\nimage: postgres\nmounts:\n  - type: persistent\n    source: data\n    target: /var/lib/postgresql/data\n",
		DeploymentMode:    DeploymentModeSelected,
		DeploymentServers: []string{"srv-a"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if app.PersistentPath == "" {
		t.Fatal("expected managed persistent path")
	}
	updated, err := svc.Update(ctx, app.ID, SaveInput{
		Name:              app.Name,
		Enabled:           app.Enabled,
		SpecYAML:          app.SpecYAML,
		DeploymentMode:    app.DeploymentMode,
		DeploymentServers: app.DeploymentServers,
		ReverseProxy:      app.ReverseProxy,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.PersistentPath != app.PersistentPath {
		t.Fatalf("persistent path = %q, want %q", updated.PersistentPath, app.PersistentPath)
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
	// 新控制面：Stop 只写 desired/Job（action=stop），远端停止由 orchestrator 执行。
	jobs := jobsForApplication(t, svc, app.ID)
	if len(jobs) != 2 {
		t.Fatalf("expected one stop Job per server, got %#v", jobs)
	}
	for _, job := range jobs {
		if job.ApplicationID != app.ID || job.Action != "stop" {
			t.Fatalf("stop should plan stop Jobs without purging files: %#v", job)
		}
	}
	if len(runtime.stops) != 0 {
		t.Fatalf("stop must not call agent runtime directly, stops = %#v", runtime.stops)
	}
}

func TestStopAppWrapsReverseProxyReconcileFailure(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{Name: "web", Enabled: true, SpecYAML: "name: web\nimage: nginx\n"})
	if err != nil {
		t.Fatal(err)
	}
	svc.SetReverseProxyReconciler(fakeReverseProxyReconciler{err: errors.New("create container failed: Conflict")})

	_, err = svc.Stop(ctx, app.ID, false)
	if err == nil {
		t.Fatal("expected reverse proxy reconcile error")
	}
	var appErr *panelerr.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("expected panel error, got %T %v", err, err)
	}
	if appErr.Code != "application_runtime_operation_failed" || appErr.HTTPStatus != 502 || !strings.Contains(appErr.Message, "create container failed: Conflict") {
		t.Fatalf("unexpected wrapped error: %#v", appErr)
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
	if err := def.Execute(tasks.TaskContext{Context: ctx, Task: task, Service: svc.tasks}); err != nil {
		t.Fatal(err)
	}
	// 新控制面：purge 参数落在 Job 的 action=purge 上，不再直接调远端。
	jobs := jobsForApplication(t, svc, app.ID)
	if len(jobs) != 2 {
		t.Fatalf("expected two purge Jobs, got %#v", jobs)
	}
	for _, job := range jobs {
		if job.Action != "purge" {
			t.Fatalf("expected purge Job, got %#v", job)
		}
	}
	if len(runtime.stops) != 0 {
		t.Fatalf("stop task must not call agent runtime directly, stops = %#v", runtime.stops)
	}
	storedTask, err := svc.tasks.Get(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if storedTask.Status != tasks.StatusCompleted {
		t.Fatalf("expected completed stop task, got %#v", storedTask)
	}
}

func TestSelectedDeploymentRemovalPurgesRemovedInstance(t *testing.T) {
	svc, runtime, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{
		Name:              "web",
		Enabled:           true,
		SpecYAML:          "name: web\nimage: nginx\n",
		DeploymentMode:    DeploymentModeSelected,
		DeploymentServers: []string{"srv-a", "srv-b"},
	})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := svc.Update(ctx, app.ID, SaveInput{
		Name:              "web",
		Enabled:           true,
		SpecYAML:          "name: web\nimage: nginx\n",
		DeploymentMode:    DeploymentModeSelected,
		DeploymentServers: []string{"srv-b"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.DeploymentServers) != 1 || updated.DeploymentServers[0] != "srv-b" {
		t.Fatalf("deployment servers = %#v", updated.DeploymentServers)
	}
	// 新控制面：被移除的目标以 purge Job 表达，远端清理由 orchestrator 执行。
	purged := false
	for _, job := range jobsForApplication(t, svc, app.ID) {
		if job.ServerID == "srv-a" && job.Action == "purge" {
			purged = true
		}
	}
	if !purged {
		t.Fatalf("expected removed selected target to be purged, jobs = %#v", jobsForApplication(t, svc, app.ID))
	}
	if len(runtime.stops) != 0 {
		t.Fatalf("update must not call agent runtime directly, stops = %#v", runtime.stops)
	}
}

func TestDeleteApplicationPurgesRuntimeData(t *testing.T) {
	svc, runtime, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{Name: "web", Enabled: true, SpecYAML: "name: web\nimage: nginx\n"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(ctx, app.ID); err != nil {
		t.Fatal(err)
	}
	deleted, err := svc.Get(ctx, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !deleted.DeletionRequested || deleted.Enabled {
		t.Fatalf("expected deletion requested app, got %#v", deleted)
	}
	// 新控制面：删除以 purge Job（RemoveData=true）表达，远端清理由 orchestrator 执行。
	jobs := jobsForApplication(t, svc, app.ID)
	if len(jobs) != 2 {
		t.Fatalf("expected purge Jobs per server, got %#v", jobs)
	}
	for _, job := range jobs {
		if job.ApplicationID != app.ID || job.Action != "purge" || !job.RemoveData {
			t.Fatalf("delete should purge application runtime data: %#v", job)
		}
	}
	if len(runtime.stops) != 0 {
		t.Fatalf("delete must not call agent runtime directly, stops = %#v", runtime.stops)
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

func TestListWithRuntimeUsesCachedRuntimeStatus(t *testing.T) {
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
	statusCalls := 0
	runtime.statusHook = func(string) {
		statusCalls++
	}

	apps, err := svc.ListWithRuntime(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(apps) != 1 {
		t.Fatalf("apps = %#v", apps)
	}
	// 新控制面：ListWithRuntime 只读 DB 缓存（实例行 + 活跃 Job），不做远端
	// 调用；因此摘要状态是 DB 派生值，而不会是 fake runtime.statuses 里的
	// running——这正好证明没有走远端。
	if apps[0].RuntimeStatus == appruntime.StatusRunning {
		t.Fatalf("runtime status should come from cached AppDB state, got %#v", apps[0])
	}
	if statusCalls != 0 {
		t.Fatalf("list runtime summary made %d remote status calls", statusCalls)
	}
}

func TestAggregateRuntimeStatusReportsMissingInstance(t *testing.T) {
	status := aggregateRuntimeStatus(true, []appruntime.InstanceStatus{
		{Status: appruntime.StatusRunning},
		{Status: appruntime.StatusMissing},
	})
	if status != appruntime.StatusMissing {
		t.Fatalf("status = %q, want %q", status, appruntime.StatusMissing)
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
	// 新控制面：image update 只触发 plan 写 Job，远端执行由 orchestrator 负责。
	jobs := jobsForApplication(t, svc, app.ID)
	if len(jobs) != 2 {
		t.Fatalf("expected planned Jobs after image update, got %#v", jobs)
	}
	for _, job := range jobs {
		if job.Action != "apply" {
			t.Fatalf("expected apply Job, got %#v", job)
		}
	}
	if len(runtime.deploys) != 0 {
		t.Fatalf("image update must not call agent runtime directly, deploys = %#v", runtime.deploys)
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

func TestRestartTaskExecutorPlansForcedDeploymentAndCompletesTask(t *testing.T) {
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
	if err := def.Execute(tasks.TaskContext{Context: ctx, Task: task, Service: svc.tasks}); err != nil {
		t.Fatal(err)
	}
	if len(runtime.restarts) != 0 {
		t.Fatalf("restart task should plan lifecycle work instead of direct runtime restart, got %d restart calls", len(runtime.restarts))
	}
	storedTask, err := svc.tasks.Get(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if storedTask.Status != tasks.StatusCompleted {
		t.Fatalf("expected completed restart task, got %#v", storedTask)
	}
	// 新控制面：restart 以 Force=true 的 apply Job 表达，不再写 lifecycle target。
	jobs := jobsForApplication(t, svc, app.ID)
	if len(jobs) != 1 {
		t.Fatalf("expected restart task to plan one Job, got %#v", jobs)
	}
	if jobs[0].ForceNonce == 0 {
		t.Fatalf("expected forced deployment Job, got %#v", jobs[0])
	}
}

func TestRestorePersistentDataRestoresAndPlansForcedDeployment(t *testing.T) {
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
	if result.DeploymentID == "" {
		t.Fatal("expected deployment operation id")
	}
	if runtime.restoreApplicationID != app.ID || string(runtime.restoreContent) != "zip" {
		t.Fatalf("restore application=%q content=%q", runtime.restoreApplicationID, runtime.restoreContent)
	}
	if len(runtime.restarts) != 0 {
		t.Fatalf("persistent restore should plan lifecycle work instead of direct runtime restart, got %d restart calls", len(runtime.restarts))
	}
}

func TestRestorePersistentDataImportsBeforeFirstDeploy(t *testing.T) {
	svc, runtime, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{
		Name:              "db",
		Enabled:           false,
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
	if result.TaskID != "" {
		t.Fatalf("expected no restart task before first deploy, got %q", result.TaskID)
	}
	if result.Application.ID != app.ID {
		t.Fatalf("expected restored application %q, got %q", app.ID, result.Application.ID)
	}
	if runtime.restoreBaseURL != "https://srv-a.agent" || runtime.restoreApplicationID != app.ID || string(runtime.restoreContent) != "zip" {
		t.Fatalf("restore baseURL=%q application=%q content=%q", runtime.restoreBaseURL, runtime.restoreApplicationID, runtime.restoreContent)
	}
	if len(runtime.restarts) != 0 {
		t.Fatalf("expected no restart before first deploy, got %d", len(runtime.restarts))
	}
}

func TestGetApplicationOperationRecordHandlesEmptyStageTimes(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{Name: "web", Enabled: true, SpecYAML: "name: web\nimage: nginx\n"})
	if err != nil {
		t.Fatal(err)
	}
	jobs := jobsForApplication(t, svc, app.ID)
	if len(jobs) == 0 {
		t.Fatal("expected planned jobs")
	}
	// 未结束的步骤：last_steps_json 里的步骤不带时间字段。
	if _, err := svc.db.Exec(`UPDATE jobs SET last_steps_json=? WHERE id=?`,
		`[{"name":"write_files","status":"running","detail":"detail"}]`, jobs[0].ID); err != nil {
		t.Fatal(err)
	}

	detail, err := svc.GetApplicationOperationRecord(ctx, jobs[0].IntentID)
	if err != nil {
		t.Fatalf("GetApplicationOperationRecord should tolerate empty stage time: %v", err)
	}
	if len(detail.Targets) == 0 {
		t.Fatalf("unexpected detail targets: %#v", detail.Targets)
	}
	foundStage := false
	for _, target := range detail.Targets {
		for _, stage := range target.Stages {
			if stage.Stage == "write_files" {
				foundStage = true
				if stage.StartedAt != nil || stage.FinishedAt != nil {
					t.Fatalf("empty stage time should stay nil: %#v", stage)
				}
			}
		}
	}
	if !foundStage {
		t.Fatalf("expected write_files stage, got %#v", detail.Targets)
	}
}

func newTestService(t *testing.T) (*Service, *fakeRuntimeClient, *fakeServerProvider, func()) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.CoordinationDatabase = filepath.Join(dir, "coordination.db")
	cfg.LogDatabase = filepath.Join(dir, "log.db")
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
		variables, _ := json.Marshal(srv.Variables)
		now := time.Now().UTC().Format(time.RFC3339Nano)
		if _, err := store.AppDB().Exec(`INSERT INTO servers(id,name,host,port,ssh_username,credential_id,docker_host,traits,variables_json,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
			srv.ID, srv.Name, srv.Host, srv.Port, srv.SSHUsername, "cred_1", srv.DockerHost, string(traits), string(variables), now, now); err != nil {
			t.Fatal(err)
		}
	}
	taskSvc := tasks.NewService(store.LogDB())
	svc := NewServiceWithOptions(store.AppDB(), runtime, taskSvc, Config{
		Namespace:      "apps",
		Region:         "global",
		Datacenter:     "dc1",
		SaveSessionDir: filepath.Join(dir, "sessions"),
	}, WithLogDB(store.LogDB()), WithCoordDB(store.CoordDB()))
	svc.RegisterTasks(taskSvc)
	svc.SetServerProvider(servers)
	svc.SetApplicationReconcileTrigger(&fakeApplicationReconcileTrigger{svc: svc, tasks: taskSvc})
	return svc, runtime, servers, func() { _ = store.Close() }
}

type fakeApplicationReconcileTrigger struct {
	svc   *Service
	tasks *tasks.Service
}

func (f *fakeApplicationReconcileTrigger) TriggerApplicationReconcile(ctx context.Context, trigger tasks.PeriodicTrigger) (tasks.Task, bool, error) {
	payload := map[string]any{}
	if raw, ok := trigger.Payload.(map[string]any); ok {
		payload = raw
	}
	appIDs := stringSlicePayload(payload["applicationIds"])
	serverIDs := stringSlicePayload(payload["serverIds"])
	stopServers := stringSlicePayload(payload["stopServers"])
	purge, _ := payload["purge"].(bool)
	force, _ := payload["force"].(bool)
	if len(appIDs) == 0 {
		return tasks.Task{}, false, nil
	}
	// 新的控制面：触发只负责写 desired/Job，远端变更由 orchestrator 执行。
	for _, appID := range appIDs {
		if _, err := f.svc.PlanApplicationDeployment(ctx, DeploymentPlanRequest{
			ApplicationID: appID,
			ServerIDs:     serverIDs,
			StopServers:   stopServers,
			Purge:         purge,
			Force:         force,
			Manual:        trigger.Manual,
			TriggerType:   firstNonEmpty(trigger.Type, "test"),
		}); err != nil {
			return tasks.Task{}, false, err
		}
	}
	return tasks.Task{}, false, nil
}

func stringSlicePayload(value any) []string {
	switch items := value.(type) {
	case []string:
		return items
	case []any:
		out := []string{}
		for _, item := range items {
			if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
				out = append(out, strings.TrimSpace(text))
			}
		}
		return out
	default:
		return nil
	}
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
		Variables: map[string]string{
			"role": id + "-role",
		},
	}
}

func TestNormalizeReverseProxyRulesTargetType(t *testing.T) {
	rules, err := normalizeReverseProxyRules([]ReverseProxyRule{
		{Domain: "local.example.test", TargetPort: 8080, OriginServerIDs: []string{"srv-a"}, AnyAccess: AnyAccessConfig{Strategy: AnyAccessStrategyRoundRobin}, Paths: []ReverseProxyPath{{Path: "/"}}},
		{Domain: "container.example.test", TargetType: ReverseProxyTargetContainer, TargetPort: 80, OriginServerIDs: []string{"srv-a"}, AnyAccess: AnyAccessConfig{Strategy: AnyAccessStrategyRoundRobin}, Paths: []ReverseProxyPath{{Path: "/app"}}},
	})
	if err != nil {
		t.Fatalf("normalize reverse proxy: %v", err)
	}
	if rules[0].TargetType != ReverseProxyTargetLocal {
		t.Fatalf("local target type = %q", rules[0].TargetType)
	}
	if rules[1].TargetType != ReverseProxyTargetContainer {
		t.Fatalf("container target type = %q", rules[1].TargetType)
	}
	if _, err := normalizeReverseProxyRules([]ReverseProxyRule{{Domain: "bad.example.test", TargetType: "remote", TargetPort: 80, OriginServerIDs: []string{"srv-a"}}}); err == nil {
		t.Fatal("expected invalid target type error")
	}
}

func TestRenderReverseProxyConfigDisablesCacheAndWritesAdvancedPathOptions(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	app := Application{
		ID:   "app-proxy-options",
		Name: "proxy-options",
		ReverseProxy: []ReverseProxyRule{{
			Domain:          "app.example.test",
			TargetType:      ReverseProxyTargetLocal,
			TargetPort:      8080,
			OriginServerIDs: []string{"srv-a"},
			AnyAccess:       AnyAccessConfig{Strategy: AnyAccessStrategyRoundRobin},
			Paths: []ReverseProxyPath{{
				Path: "/api",
				Options: HTTPRouteOptions{
					GzipMode:              HTTPRouteModeOff,
					ClientMaxBodySizeMB:   10,
					ConnectTimeoutSeconds: 5,
					ReadTimeoutSeconds:    60,
					SendTimeoutSeconds:    30,
					BufferingMode:         HTTPRouteModeOn,
					WebSocketMode:         HTTPRouteWebSocketAuto,
					RequestHeaders:        []HTTPHeader{{Name: "X-App-Request", Value: "proxy"}},
					ResponseHeaders:       []HTTPHeader{{Name: "X-App-Response", Value: "ready"}},
				},
			}},
		}},
	}

	_, config, err := svc.renderReverseProxyConfig(context.Background(), app, nil)
	if err != nil {
		t.Fatalf("render reverse proxy config: %v", err)
	}
	for _, want := range []string{
		"proxy_cache off;",
		"gzip off;",
		"client_max_body_size 10m;",
		"proxy_connect_timeout 5s;",
		"proxy_read_timeout 60s;",
		"proxy_send_timeout 30s;",
		"proxy_buffering on;",
		`proxy_set_header X-App-Request "proxy";`,
		"proxy_hide_header X-App-Response;",
		`add_header X-App-Response "ready" always;`,
		"proxy_set_header Upgrade $http_upgrade;",
	} {
		if !strings.Contains(config, want) {
			t.Fatalf("application proxy config missing %q:\n%s", want, config)
		}
	}
}

func insertApplicationTestServer(t *testing.T, svc *Service, srv server.Server) {
	t.Helper()
	traits, _ := json.Marshal(srv.Traits)
	variables, _ := json.Marshal(srv.Variables)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := svc.db.Exec(`INSERT INTO servers(id,name,host,port,ssh_username,credential_id,docker_host,traits,variables_json,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		srv.ID, srv.Name, srv.Host, srv.Port, srv.SSHUsername, "cred_1", srv.DockerHost, string(traits), string(variables), now, now); err != nil {
		t.Fatal(err)
	}
}

func waitApplicationTaskStatus(t *testing.T, taskSvc *tasks.Service, taskID, status string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		task, err := taskSvc.Get(context.Background(), taskID)
		if err == nil && task.Status == status {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	task, err := taskSvc.Get(context.Background(), taskID)
	if err != nil {
		t.Fatalf("get task %s: %v", taskID, err)
	}
	t.Fatalf("task %s status = %s, want %s", taskID, task.Status, status)
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
	stopErr              error
	deleteFailures       int
	createEntered        chan struct{}
	createRelease        chan struct{}
	createOnce           sync.Once
	writeHook            func(agentcontract.RuntimeWriteFilesRequest)
	statusHook           func(instanceID string)
}

type fakeReverseProxyReconciler struct {
	err error
}

func (f fakeReverseProxyReconciler) ReconcileReverseProxy(context.Context) error {
	return f.err
}

func (f *fakeRuntimeClient) RuntimeWriteFiles(ctx context.Context, baseURL string, req agentcontract.RuntimeWriteFilesRequest) error {
	if f.writeHook != nil {
		f.writeHook(req)
	}
	f.writes = append(f.writes, req)
	return nil
}

func (f *fakeRuntimeClient) RuntimeReload(context.Context, string, agentcontract.RuntimeReloadRequest) (agentcontract.RuntimeReloadResponse, error) {
	return agentcontract.RuntimeReloadResponse{Phase: "unsupported", Error: "reload is not configured"}, nil
}

func (f *fakeRuntimeClient) DockerImagePull(ctx context.Context, baseURL, reference string) error {
	f.pulls = append(f.pulls, reference)
	return nil
}

func (f *fakeRuntimeClient) DockerContainerDelete(ctx context.Context, baseURL, id string) error {
	f.deletes = append(f.deletes, id)
	if f.deleteFailures > 0 {
		f.deleteFailures--
		return errors.New("container delete failed")
	}
	return nil
}

func (f *fakeRuntimeClient) RuntimeCreateContainer(ctx context.Context, baseURL string, req agentcontract.RuntimeCreateContainerRequest) (agentcontract.RuntimeCreateContainerResponse, error) {
	if f.createEntered != nil {
		f.createOnce.Do(func() { close(f.createEntered) })
	}
	if f.createRelease != nil {
		select {
		case <-ctx.Done():
			return agentcontract.RuntimeCreateContainerResponse{}, ctx.Err()
		case <-f.createRelease:
		}
	}
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
	if f.stopErr != nil {
		return agentcontract.RuntimeInstanceResponse{}, f.stopErr
	}
	status := appruntime.StatusStopped
	if req.Purge {
		status = "purged"
	}
	return agentcontract.RuntimeInstanceResponse{InstanceID: req.InstanceID, Status: status, ObservedAt: time.Now().UTC()}, nil
}

func (f *fakeRuntimeClient) RuntimeRestart(ctx context.Context, baseURL string, req agentcontract.RuntimeRestartRequest) (agentcontract.RuntimeInstanceResponse, error) {
	f.restarts = append(f.restarts, req)
	return agentcontract.RuntimeInstanceResponse{InstanceID: req.InstanceID, Status: appruntime.StatusRunning, ObservedAt: time.Now().UTC()}, nil
}

func (f *fakeRuntimeClient) RuntimeStatus(ctx context.Context, baseURL, instanceID, containerName string) (agentcontract.RuntimeStatusResponse, error) {
	if f.statusHook != nil {
		f.statusHook(instanceID)
	}
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

func (f fakeBuiltinResolver) BuiltinVariables(ctx context.Context, render ApplicationVariableContext) (map[string]any, error) {
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

func TestReconcileStoppedDoesNotTouchApplicationUpdatedAt(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	app, err := svc.Create(ctx, SaveInput{Name: "web", Enabled: true, SpecYAML: "name: web\nimage: nginx\n"})
	if err != nil {
		t.Fatal(err)
	}
	before, err := svc.Get(ctx, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.markApplicationReconcileStopped(ctx, app.ID); err != nil {
		t.Fatal(err)
	}
	after, err := svc.Get(ctx, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !after.ReconcileStopped {
		t.Fatalf("reconcile_stopped = false")
	}
	if !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Fatalf("updated_at changed: %v -> %v", before.UpdatedAt, after.UpdatedAt)
	}
	if after.Version != before.Version {
		t.Fatalf("version changed: %d -> %d", before.Version, after.Version)
	}
}

func TestStopDoesNotOverwriteConcurrentDerivedFields(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	app, err := svc.Create(ctx, SaveInput{Name: "web", Enabled: true, SpecYAML: "name: web\nimage: nginx\n"})
	if err != nil {
		t.Fatal(err)
	}
	app.ImageDigest = "sha256:concurrent"
	app.ImageReference = "nginx:latest"
	if err := svc.updateApplicationDerived(ctx, app); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Stop(ctx, app.ID, false); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Get(ctx, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ImageDigest != "sha256:concurrent" || got.ImageReference != "nginx:latest" {
		t.Fatalf("stop clobbered derived fields: %#v", got)
	}
	if got.Enabled {
		t.Fatalf("app should be disabled")
	}
}

type countingReverseProxyReconciler struct {
	calls int
	err   error
}

func (c *countingReverseProxyReconciler) ReconcileReverseProxy(context.Context) error {
	c.calls++
	return c.err
}

// TestAfterLifecycleTargetVerifiedSkipsFacilityProxySelfRetrigger 回归测试：
// 入口代理设施自身的目标验证成功后不得再次触发代理同步，否则会形成
// "同步完成→立即再同步"的自循环（配合验证等待时周期为约 3 分钟）。

// TestRefreshApplicationSnapshotDoesNotChurnFacilityApplication 回归测试：
// 设施应用（入口代理）的 generation/spec_hash 只由设施模块
// （ensureReverseProxyApplication，hash 为 facilityConfigHash）维护。
// 应用侧 refresh 若用 applicationHash 覆盖 SpecHash 并递增 generation，
// 两个写入方会交替改写同一行（每次协调 +2 代），容器 applied-state 标签
// 永远落后于应用行代次，5 秒漂移巡检把入口代理当作全部漂移无限重部署。
func TestRefreshApplicationSnapshotDoesNotChurnFacilityApplication(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	now := time.Now().UTC()
	if _, err := svc.db.ExecContext(ctx, `INSERT INTO applications(id,name,kind,enabled,spec_yaml,deployment_mode,deployment_server_ids_json,generation,spec_hash,job_id,namespace,created_at,updated_at)
		VALUES(?,?,?,1,?,?,?,?,?,?,?,?,?)`,
		FacilityProxyApplicationID, "facility", ApplicationKindFacility,
		"kind: facility/reverse-proxy\nname: entrance-gateway\nimage: nginx:1.27-alpine\n",
		DeploymentModeSelected, `["srv-a"]`, 7, "facility-config-hash", FacilityProxyApplicationID, "facility",
		formatTime(now), formatTime(now)); err != nil {
		t.Fatal(err)
	}

	// 模拟 ensureReverseProxyApplication 之后、refresh 之前的稳定状态。
	current, err := svc.Get(ctx, FacilityProxyApplicationID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Kind != ApplicationKindFacility {
		t.Fatalf("fixture should be a facility app, got kind %q", current.Kind)
	}
	refreshed, err := svc.refreshApplicationSnapshot(ctx, current)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.Generation != 7 || refreshed.SpecHash != "facility-config-hash" {
		t.Fatalf("facility refresh must not bump generation or overwrite spec hash, got generation=%d specHash=%q", refreshed.Generation, refreshed.SpecHash)
	}

	// 再次 refresh（模拟下一轮计划/apply）也必须保持稳定，不能出现 +1/+2 递增。
	again, err := svc.refreshApplicationSnapshot(ctx, refreshed)
	if err != nil {
		t.Fatal(err)
	}
	if again.Generation != 7 || again.SpecHash != "facility-config-hash" {
		t.Fatalf("facility refresh must stay stable across calls, got generation=%d specHash=%q", again.Generation, again.SpecHash)
	}
}
