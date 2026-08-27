package appdeploy

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	agentcontract "panel/internal/agent/contract"
	"panel/internal/modules/applications"
	server "panel/internal/modules/servers"
	"panel/internal/modules/tasks"
	controlplane "panel/internal/orchestrator"
	"panel/internal/platform/config"
	storage "panel/internal/platform/database"
	"panel/internal/platform/database/models"
	"panel/internal/platform/database/orm"
)

type fakeServerProvider struct {
	items map[string]server.Server
}

func (f *fakeServerProvider) List(ctx context.Context) ([]server.Server, error) {
	out := make([]server.Server, 0, len(f.items))
	for _, srv := range f.items {
		out = append(out, srv)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (f *fakeServerProvider) Get(ctx context.Context, id string) (server.Server, error) {
	if srv, ok := f.items[id]; ok {
		return srv, nil
	}
	return server.Server{}, fmt.Errorf("server %s not found", id)
}

// Harness 是剧本驱动集成测试台：真实 SQLite Store + applications.Service +
// 已启动的 orchestrator.Controller，mock agent 按剧本响应。
type Harness struct {
	T       *testing.T
	Store   *storage.Store
	AppSvc  *applications.Service
	TaskSvc *tasks.Service
	Agent   *ScriptedAgent
	servers *fakeServerProvider
	ctx     context.Context
	cancel  context.CancelFunc
}

func NewHarness(t *testing.T) *Harness {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.LogDatabase = filepath.Join(dir, "log.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.CoordinationDatabase = filepath.Join(dir, "coordination.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	taskSvc := tasks.NewService(store.LogDB())
	agent := NewScriptedAgent()
	prov := &fakeServerProvider{items: map[string]server.Server{}}
	trigger := &harnessTrigger{}
	appSvc := applications.NewServiceWithOptions(store.AppDB(), agent, taskSvc, applications.Config{
		Namespace:      "apps",
		Region:         "global",
		Datacenter:     "dc1",
		SaveSessionDir: filepath.Join(dir, "sessions"),
	}, applications.WithLogDB(store.LogDB()), applications.WithCoordDB(store.CoordDB()),
		applications.WithServerProvider(prov), applications.WithApplicationReconcileTrigger(trigger))
	trigger.appSvc = appSvc
	appSvc.RegisterTasks(taskSvc)
	h := &Harness{T: t, Store: store, AppSvc: appSvc, TaskSvc: taskSvc, Agent: agent, servers: prov, ctx: ctx, cancel: cancel}
	t.Cleanup(func() {
		cancel()
		_ = store.Close()
	})
	if err := appSvc.StartOrchestrator(ctx); err != nil {
		t.Fatal(err)
	}
	return h
}

// AddServer 注册一台 agent 兼容服务器（内存 provider + servers/credentials 表）。
func (h *Harness) AddServer(id string) {
	h.T.Helper()
	srv := server.Server{
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
		Variables: map[string]string{"role": id + "-role"},
	}
	h.servers.items[id] = srv
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := h.Store.AppDB().Exec(`INSERT INTO credentials(id,name,type,username,created_at,updated_at) VALUES(?,?,?,?,?,?)`,
		"cred-"+id, "test", "password", "root", now, now); err != nil {
		h.T.Fatal(err)
	}
	traits, _ := json.Marshal(srv.Traits)
	variables, _ := json.Marshal(srv.Variables)
	if _, err := h.Store.AppDB().Exec(`INSERT INTO servers(id,name,host,port,ssh_username,credential_id,docker_host,traits,variables_json,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)`, srv.ID, srv.Name, srv.Host, srv.Port, srv.SSHUsername, "cred-"+id, srv.DockerHost, string(traits), string(variables), now, now); err != nil {
		h.T.Fatal(err)
	}
}

func (h *Harness) CreateApp(name string, enabled bool, specYAML string) applications.Application {
	h.T.Helper()
	app, err := h.AppSvc.Create(h.ctx, applications.SaveInput{Name: name, Enabled: enabled, SpecYAML: specYAML})
	if err != nil {
		h.T.Fatalf("create app %s: %v", name, err)
	}
	return app
}

func (h *Harness) Plan(req applications.DeploymentPlanRequest) applications.DeploymentPlanResult {
	h.T.Helper()
	result, err := h.AppSvc.PlanApplicationDeployment(h.ctx, req)
	if err != nil {
		h.T.Fatalf("plan application deployment: %v", err)
	}
	return result
}

// WriteObservation 模拟一次 agent 上报/周期巡检的观测写入（ObservationWriter）。
func (h *Harness) WriteObservation(instanceID, state string, generation int, specHash string) {
	h.T.Helper()
	w := controlplane.NewObservationWriter(h.Store.AppDB())
	if _, err := w.Write(h.ctx, controlplane.Observation{
		InstanceID:         instanceID,
		Source:             "agent_report",
		ObservedAt:         time.Now().UTC(),
		ObservedState:      state,
		ObservedGeneration: generation,
		ObservedSpecHash:   specHash,
	}); err != nil {
		h.T.Fatalf("write observation %s: %v", instanceID, err)
	}
}

// JobRow 是剧本断言的 Job 轻量视图（raw SQL + 字符串时间扫描，避免 orm 对空时间解析失败）。
type JobRow struct {
	ID                string
	ApplicationID     string
	ServerID          string
	InstanceID        string
	Action            string
	State             string
	IntentID          string
	TriggerType       string
	ErrorCode         string
	ErrorClass        string
	ErrorMessage      string
	ErrorDetail       string
	DesiredGeneration int
	Attempts          int
	RemoveData        bool
	NextRunAt         *time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

const jobSelect = "SELECT id,application_id,server_id,instance_id,action,state,intent_id,trigger_type,error_code,error_class,error_message,error_detail,desired_generation,attempts,remove_data,next_run_at,created_at,updated_at FROM jobs"

func scanJobRows(rows interface{ Scan(...any) error }) (JobRow, error) {
	var j JobRow
	var nextRun sql.NullString
	var created, updated string
	err := rows.Scan(&j.ID, &j.ApplicationID, &j.ServerID, &j.InstanceID, &j.Action, &j.State, &j.IntentID, &j.TriggerType,
		&j.ErrorCode, &j.ErrorClass, &j.ErrorMessage, &j.ErrorDetail, &j.DesiredGeneration, &j.Attempts, &j.RemoveData,
		&nextRun, &created, &updated)
	if err != nil {
		return JobRow{}, err
	}
	if nextRun.Valid && strings.TrimSpace(nextRun.String) != "" {
		if parsed, perr := time.Parse(time.RFC3339Nano, nextRun.String); perr == nil {
			j.NextRunAt = &parsed
		}
	}
	j.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	j.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return j, nil
}

func (h *Harness) queryJobs(where string, args ...any) []JobRow {
	h.T.Helper()
	rows, err := h.Store.AppDB().QueryContext(h.ctx, jobSelect+" WHERE "+where, args...)
	if err != nil {
		h.T.Fatal(err)
	}
	defer rows.Close()
	var out []JobRow
	for rows.Next() {
		job, err := scanJobRows(rows)
		if err != nil {
			h.T.Fatal(err)
		}
		out = append(out, job)

	}
	if err := rows.Err(); err != nil {
		h.T.Fatal(err)
	}
	return out
}

// WaitAgentCalls 轮询直到 mock agent 对指定 app+server+action 的 RuntimeReconcile 调用数达到 want。
func (h *Harness) WaitAgentCalls(appID, serverID, action string, want int) {
	h.T.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if h.Agent.RecordCount(appID, serverID, action) >= want {
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	h.T.Fatalf("agent %s/%s/%s calls = %d, want %d", appID, serverID, action, h.Agent.RecordCount(appID, serverID, action), want)
}

// WaitJobAction 轮询直到出现指定 action 且状态为期望之一的 Job。
func (h *Harness) WaitJobAction(appID, serverID, action string, states ...string) JobRow {
	h.T.Helper()
	want := map[string]bool{}
	for _, s := range states {
		want[s] = true
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		for _, job := range h.queryJobs("application_id=? AND server_id=? AND action=?", appID, serverID, action) {
			if want[job.State] {
				return job
			}
		}
		time.Sleep(15 * time.Millisecond)
	}
	h.T.Fatalf("job %s/%s/%s did not reach %v", appID, serverID, action, states)
	return JobRow{}
}

// Jobs 返回应用的全部 Job（按 server 排序）。
func (h *Harness) Jobs(appID string) []JobRow {
	return h.queryJobs("application_id=?", appID)
}

// JobForServer 返回指定 app+server 的 Job；不存在则失败。
func (h *Harness) JobForServer(appID, serverID string) JobRow {
	jobs := h.queryJobs("application_id=? AND server_id=?", appID, serverID)
	if len(jobs) == 0 {
		h.T.Fatalf("job for app=%s server=%s not found", appID, serverID)
	}
	return jobs[0]
}

// WaitJobState 轮询直到指定 app+server 的 Job 进入期望状态之一。
func (h *Harness) WaitJobState(appID, serverID string, states ...string) JobRow {
	h.T.Helper()
	want := map[string]bool{}
	for _, s := range states {
		want[s] = true
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		for _, job := range h.queryJobs("application_id=? AND server_id=?", appID, serverID) {
			if want[job.State] {
				return job
			}
		}
		time.Sleep(15 * time.Millisecond)
	}
	h.T.Fatalf("job for app=%s server=%s did not reach %v", appID, serverID, states)
	return JobRow{}
}

// WaitNoActiveJob 轮询直到指定 app+server 无 pending/running/failed_retryable Job。
func (h *Harness) WaitNoActiveJob(appID, serverID string) {
	h.T.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if len(h.queryJobs("application_id=? AND server_id=? AND state IN ('pending','running','failed_retryable')", appID, serverID)) == 0 {
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	h.T.Fatalf("app=%s server=%s still has active jobs", appID, serverID)
}

// WaitInstanceObserved 轮询直到实例 observed_state 达到期望值。
func (h *Harness) WaitInstanceObserved(appID, serverID, wantState string) models.ApplicationInstance {
	h.T.Helper()
	instanceID := runtimeInstanceIDFor(appID, serverID)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var rows []models.ApplicationInstance
		if err := orm.New(h.Store.AppDB()).From("application_instances").Where("id=?", instanceID).All(h.ctx, &rows); err == nil && len(rows) == 1 && rows[0].ObservedState == wantState {
			return rows[0]
		}
		time.Sleep(15 * time.Millisecond)
	}
	h.T.Fatalf("instance %s observed_state did not become %q", instanceID, wantState)
	return models.ApplicationInstance{}
}

// WaitAppDeleted 轮询直到应用行被物理删除（删除 finalizer 完成）。
func (h *Harness) WaitAppDeleted(appID string) {
	h.T.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var n int
		if err := h.Store.AppDB().QueryRow(`SELECT COUNT(*) FROM applications WHERE id=?`, appID).Scan(&n); err == nil && n == 0 {
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	h.T.Fatalf("application %s was not physically deleted", appID)
}

// RuntimeStatus 返回应用聚合运行时状态。
func (h *Harness) RuntimeStatus(appID string) string {
	h.T.Helper()
	runtime, err := h.AppSvc.Runtime(h.ctx, appID)
	if err != nil {
		h.T.Fatal(err)
	}
	return runtime.Status
}

func runtimeInstanceIDFor(appID, serverID string) string {
	return appID + "-" + serverID
}

// harnessTrigger 是应用协调触发的测试替身：把应用模块的触发（保存/部署/停止/
// 删除/周期）直接转成 PlanApplicationDeployment，走真实 planner/controller。
type harnessTrigger struct {
	appSvc *applications.Service
}

func (t *harnessTrigger) TriggerApplicationReconcile(ctx context.Context, trigger tasks.PeriodicTrigger) (tasks.Task, bool, error) {
	if t == nil || t.appSvc == nil {
		return tasks.Task{}, false, nil
	}
	payload := map[string]any{}
	if raw, ok := trigger.Payload.(map[string]any); ok {
		payload = raw
	}
	appIDs := payloadStrings(payload["applicationIds"])
	if len(appIDs) == 0 {
		return tasks.Task{}, false, nil
	}
	serverIDs := payloadStrings(payload["serverIds"])
	stopServers := payloadStrings(payload["stopServers"])
	purge, _ := payload["purge"].(bool)
	force, _ := payload["force"].(bool)
	for _, appID := range appIDs {
		if _, err := t.appSvc.PlanApplicationDeployment(ctx, applications.DeploymentPlanRequest{
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

func payloadStrings(value any) []string {
	switch items := value.(type) {
	case []string:
		return items
	case []any:
		out := []string{}
		for _, item := range items {
			if text, ok := item.(string); ok && text != "" {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
