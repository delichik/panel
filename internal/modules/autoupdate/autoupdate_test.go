package autoupdate

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	agentcontract "panel/internal/agent/contract"
	"panel/internal/modules/applications"
	"panel/internal/modules/packages"
	"panel/internal/modules/servers"
	"panel/internal/modules/settings"
	"panel/internal/modules/tasks"
	"panel/internal/platform/config"
	storage "panel/internal/platform/database"
	"panel/internal/platform/linux"
)

// fakeSettingsStore 在内存中模拟 runtime_settings：Runtime() 返回热设置，
// RecordAutoUpdateLastRun 记录每次运行态写入并更新快照（等价 settings 服务）。
type fakeSettingsStore struct {
	mu     sync.Mutex
	rt     settings.RuntimeSettings
	states []settings.RuntimeAutoUpdateRunState
}

func (f *fakeSettingsStore) Runtime() settings.RuntimeSettings {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.rt
}

func (f *fakeSettingsStore) RecordAutoUpdateLastRun(_ context.Context, state settings.RuntimeAutoUpdateRunState) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.states = append(f.states, state)
	f.rt.AutoUpdate.LastRunAt = state.LastRunAt.UTC().Format(time.RFC3339Nano)
	f.rt.AutoUpdate.LastRunStatus = state.LastRunStatus
	f.rt.AutoUpdate.LastRunTaskID = state.LastRunTaskID
	return nil
}

func (f *fakeSettingsStore) setAutoUpdate(rt settings.RuntimeAutoUpdateSettings) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rt.AutoUpdate = rt
}

func (f *fakeSettingsStore) lastState() (settings.RuntimeAutoUpdateRunState, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.states) == 0 {
		return settings.RuntimeAutoUpdateRunState{}, false
	}
	return f.states[len(f.states)-1], true
}

func (f *fakeSettingsStore) stateStatuses() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.states))
	for _, state := range f.states {
		out = append(out, state.LastRunStatus)
	}
	return out
}

func newTaskService(t *testing.T) (*tasks.Service, *sql.DB) {
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
	t.Cleanup(func() { _ = store.Close() })
	return tasks.NewService(store.AppDB()), store.AppDB()
}

func registerTestTaskTypes(t *testing.T, taskSvc *tasks.Service, taskTypes ...string) {
	t.Helper()
	for _, taskType := range taskTypes {
		taskSvc.MustRegister(tasks.Definition{
			Type:    taskType,
			Summary: "test " + taskType,
			Execute: func(tasks.TaskContext) error { return nil },
		})
	}
}

func createFinishedTask(t *testing.T, taskSvc *tasks.Service, taskType, status string) tasks.Task {
	t.Helper()
	task, err := taskSvc.Create(context.Background(), tasks.CreateInput{
		Type:    taskType,
		Status:  status,
		Summary: "test " + taskType,
	})
	if err != nil {
		t.Fatalf("create %s task: %v", taskType, err)
	}
	return task
}

func testServer(id, name string) server.Server {
	return server.Server{
		ID:        id,
		Name:      name,
		Reachable: true,
		OS:        linux.OSRelease{Supported: true},
		Traits: map[string]string{
			agentcontract.TraitStatus: agentcontract.StatusCompatible,
			agentcontract.TraitURL:    "http://127.0.0.1:9420",
		},
	}
}

type fakeServerLister struct{ servers []server.Server }

func (f fakeServerLister) List(context.Context) ([]server.Server, error) { return f.servers, nil }

type fakeApplicationLister struct{ apps []applications.Application }

func (f fakeApplicationLister) List(context.Context) ([]applications.Application, error) {
	return f.apps, nil
}

type fakeApplicationUpgrader struct {
	t          *testing.T
	taskSvc    *tasks.Service
	taskStatus string
	ids        []string
}

func (f *fakeApplicationUpgrader) UpgradeApplications(_ context.Context, ids []string, all bool) (tasks.Task, error) {
	f.ids = append([]string{}, ids...)
	return createFinishedTask(f.t, f.taskSvc, "application_image_upgrade_all", f.taskStatus), nil
}

type fakeImageRefresher struct {
	t       *testing.T
	taskSvc *tasks.Service
	servers []string
}

func (f *fakeImageRefresher) RefreshImages(_ context.Context, serverID, _, _ string) (tasks.Task, error) {
	f.servers = append(f.servers, serverID)
	return createFinishedTask(f.t, f.taskSvc, "image_refresh", tasks.StatusCompleted), nil
}

type fakePackageMaintainer struct {
	t           *testing.T
	taskSvc     *tasks.Service
	mu          sync.Mutex
	refreshed   []string
	upgrades    []string
	failServers map[string]bool
}

func (f *fakePackageMaintainer) Refresh(_ context.Context, serverID string, _ ...string) (packages.RefreshResult, error) {
	f.mu.Lock()
	f.refreshed = append(f.refreshed, serverID)
	f.mu.Unlock()
	task := createFinishedTask(f.t, f.taskSvc, "package_refresh", tasks.StatusCompleted)
	return packages.RefreshResult{ServerID: serverID, Refreshing: true, TaskID: task.ID}, nil
}

func (f *fakePackageMaintainer) UpgradeAll(_ context.Context, serverID string, _ ...string) (tasks.Task, error) {
	return f.upgrade("all", serverID)
}

func (f *fakePackageMaintainer) UpgradeSecurity(_ context.Context, serverID string, _ ...string) (tasks.Task, error) {
	return f.upgrade("security", serverID)
}

func (f *fakePackageMaintainer) upgrade(mode, serverID string) (tasks.Task, error) {
	f.mu.Lock()
	f.upgrades = append(f.upgrades, mode+":"+serverID)
	fail := f.failServers[serverID]
	f.mu.Unlock()
	if fail {
		return tasks.Task{}, errors.New("apt upgrade failed for " + serverID)
	}
	taskType := "package_upgrade_security"
	if mode == "all" {
		taskType = "package_upgrade_all"
	}
	return createFinishedTask(f.t, f.taskSvc, taskType, tasks.StatusCompleted), nil
}

func (f *fakePackageMaintainer) upgradeCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.upgrades...)
}

type fakeReverseProxyUpdater struct {
	err   error
	calls int
}

func (f *fakeReverseProxyUpdater) AutoUpdateReverseProxyImage(context.Context) error {
	f.calls++
	return f.err
}

func fixedZone() *time.Location { return time.FixedZone("UTC+8", 8*3600) }

func at(hour, minute, second int, loc *time.Location) time.Time {
	return time.Date(2026, 11, 19, hour, minute, second, 0, loc)
}

// TestShouldTriggerScheduleWindow 验证计划时间窗口：只在计划 HH:MM 之后 2 分钟内
// 触发；错过窗口（含凌晨后的任意更晚时间）当天不补跑。
func TestShouldTriggerScheduleWindow(t *testing.T) {
	loc := fixedZone()
	rt := settings.RuntimeAutoUpdateSettings{
		AppsEnabled:  true,
		PackagesMode: settings.AutoUpdatePackagesModeSecurity,
		ScheduleTime: "04:00",
	}
	cases := []struct {
		name string
		now  time.Time
		want bool
	}{
		{name: "before schedule", now: at(3, 59, 59, loc), want: false},
		{name: "at schedule", now: at(4, 0, 0, loc), want: true},
		{name: "inside window", now: at(4, 1, 59, loc), want: true},
		{name: "window end", now: at(4, 2, 0, loc), want: true},
		{name: "after window", now: at(4, 2, 1, loc), want: false},
		{name: "much later same day", now: at(10, 0, 0, loc), want: false},
	}
	for _, tc := range cases {
		if got := shouldTrigger(rt, tc.now, DefaultScheduleWindow); got != tc.want {
			t.Fatalf("%s: shouldTrigger(%s) = %v, want %v", tc.name, tc.now.Format(time.RFC3339), got, tc.want)
		}
	}
}

func TestShouldTriggerRequiresEnabledSwitch(t *testing.T) {
	now := at(4, 0, 30, fixedZone())
	rt := settings.RuntimeAutoUpdateSettings{PackagesMode: settings.AutoUpdatePackagesModeSecurity, ScheduleTime: "04:00"}
	if shouldTrigger(rt, now, DefaultScheduleWindow) {
		t.Fatal("both switches off must not trigger")
	}
	rt.PackagesEnabled = true
	if !shouldTrigger(rt, now, DefaultScheduleWindow) {
		t.Fatal("packages switch on must trigger inside the window")
	}
	rt.PackagesEnabled = false
	rt.AppsEnabled = true
	if !shouldTrigger(rt, now, DefaultScheduleWindow) {
		t.Fatal("apps switch on must trigger inside the window")
	}
}

// TestShouldTriggerDeduplicatesLocalDay 验证当日去重以 Panel 本地日期为准，
// 并覆盖跨重启（持久化 lastRunAt）与脏数据的宽容语义。
func TestShouldTriggerDeduplicatesLocalDay(t *testing.T) {
	loc := fixedZone()
	now := at(4, 1, 0, loc)
	rt := settings.RuntimeAutoUpdateSettings{
		AppsEnabled:  true,
		PackagesMode: settings.AutoUpdatePackagesModeSecurity,
		ScheduleTime: "04:00",
	}
	// 同一本地日期（尽管 UTC 日期是前一天）已运行过 → 不重复。
	sameLocalDay := time.Date(2026, 11, 19, 1, 0, 0, 0, loc)
	rt.LastRunAt = sameLocalDay.UTC().Format(time.RFC3339Nano)
	if shouldTrigger(rt, now, DefaultScheduleWindow) {
		t.Fatal("a run already recorded for the local day must not trigger again")
	}
	// 前一个本地日期 → 允许运行。
	previousDay := time.Date(2026, 11, 18, 4, 0, 0, 0, loc)
	rt.LastRunAt = previousDay.UTC().Format(time.RFC3339Nano)
	if !shouldTrigger(rt, now, DefaultScheduleWindow) {
		t.Fatal("a previous-day run must not block today's run")
	}
	// 脏数据视为没有记录（宁可多跑一次也不永久跳过）。
	rt.LastRunAt = "not-a-timestamp"
	if !shouldTrigger(rt, now, DefaultScheduleWindow) {
		t.Fatal("invalid lastRunAt must be treated as no record")
	}
	rt.LastRunAt = ""
	if !shouldTrigger(rt, now, DefaultScheduleWindow) {
		t.Fatal("empty lastRunAt must be treated as no record")
	}
}

// TestTickCreatesOncePerDayAndSurvivesRestart 覆盖调度器：命中窗口创建一次任务、
// 同一本地日期第二次 tick 去重、重启后仍按持久化 lastRunAt 去重。
func TestTickCreatesOncePerDayAndSurvivesRestart(t *testing.T) {
	taskSvc, db := newTaskService(t)
	loc := fixedZone()
	now := at(4, 0, 30, loc)
	store := &fakeSettingsStore{rt: settings.RuntimeSettings{AutoUpdate: settings.RuntimeAutoUpdateSettings{
		AppsEnabled:  true,
		PackagesMode: settings.AutoUpdatePackagesModeSecurity,
		ScheduleTime: "04:00",
	}}}
	options := func() []Option {
		return []Option{
			WithClock(func() time.Time { return now }),
			WithPhaseTimeouts(time.Second, time.Second, time.Second, time.Second),
			WithWaitPollInterval(5 * time.Millisecond),
		}
	}
	svc := NewService(taskSvc, store, options()...)
	svc.RegisterTasks(taskSvc)

	task, created, err := svc.tick(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !created || task.ID == "" {
		t.Fatalf("first tick must create the automatic update task, got created=%v task=%#v", created, task)
	}
	waitForTaskTerminal(t, taskSvc, task.ID)
	waitForLastRunStatus(t, store, RunStatusCompleted)
	if count := countAutoUpdateRuns(t, db); count != 1 {
		t.Fatalf("auto_update_run rows = %d, want 1", count)
	}

	// 同一本地日期第二次 tick：去重，不创建新任务。
	if _, created, err := svc.tick(context.Background()); err != nil || created {
		t.Fatalf("second tick must not create a task (created=%v err=%v)", created, err)
	}
	if count := countAutoUpdateRuns(t, db); count != 1 {
		t.Fatalf("auto_update_run rows after second tick = %d, want 1", count)
	}

	// 模拟 Panel 重启：新的任务服务实例 + 同一持久化设置存储。
	restartedTaskSvc := tasks.NewService(db)
	restarted := NewService(restartedTaskSvc, store, options()...)
	restarted.RegisterTasks(restartedTaskSvc)
	if _, created, err := restarted.tick(context.Background()); err != nil || created {
		t.Fatalf("restart tick must dedupe using persisted lastRunAt (created=%v err=%v)", created, err)
	}
	if count := countAutoUpdateRuns(t, db); count != 1 {
		t.Fatalf("auto_update_run rows after restart tick = %d, want 1", count)
	}

	// 错过窗口不补跑：同一天更晚的时间即使 lastRunAt 为空也不触发。
	laterStore := &fakeSettingsStore{rt: settings.RuntimeSettings{AutoUpdate: store.Runtime().AutoUpdate}}
	laterStore.rt.AutoUpdate.LastRunAt = ""
	laterTaskSvc := tasks.NewService(db)
	laterNow := at(10, 0, 0, loc)
	later := NewService(laterTaskSvc, laterStore,
		WithClock(func() time.Time { return laterNow }),
		WithPhaseTimeouts(time.Second, time.Second, time.Second, time.Second),
		WithWaitPollInterval(5*time.Millisecond))
	later.RegisterTasks(laterTaskSvc)
	if _, created, err := later.tick(context.Background()); err != nil || created {
		t.Fatalf("a missed window must not be backfilled (created=%v err=%v)", created, err)
	}
}

func TestTickDoesNotCreateWhenSwitchesOff(t *testing.T) {
	taskSvc, db := newTaskService(t)
	store := &fakeSettingsStore{rt: settings.RuntimeSettings{AutoUpdate: settings.RuntimeAutoUpdateSettings{
		PackagesMode: settings.AutoUpdatePackagesModeSecurity,
		ScheduleTime: "04:00",
	}}}
	loc := fixedZone()
	svc := NewService(taskSvc, store, WithClock(func() time.Time { return at(4, 0, 30, loc) }))
	svc.RegisterTasks(taskSvc)
	if _, created, err := svc.tick(context.Background()); err != nil || created {
		t.Fatalf("disabled switches must not create a task (created=%v err=%v)", created, err)
	}
	if count := countAutoUpdateRuns(t, db); count != 0 {
		t.Fatalf("auto_update_run rows = %d, want 0", count)
	}
}

// TestRunAutoUpdateTaskDisabledCompletes 验证执行时重新读取设置：两开关都关时
// 任务直接完成并写 disabled 运行态。
func TestRunAutoUpdateTaskDisabledCompletes(t *testing.T) {
	taskSvc, _ := newTaskService(t)
	store := &fakeSettingsStore{rt: settings.RuntimeSettings{AutoUpdate: settings.RuntimeAutoUpdateSettings{
		PackagesMode: settings.AutoUpdatePackagesModeSecurity,
		ScheduleTime: "04:00",
	}}}
	loc := fixedZone()
	svc := NewService(taskSvc, store, WithClock(func() time.Time { return at(4, 0, 30, loc) }))
	svc.RegisterTasks(taskSvc)
	task, err := taskSvc.Create(context.Background(), tasks.CreateInput{Type: TaskTypeAutoUpdateRun, Summary: "Automatic updates"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.RunAutoUpdateTask(tasks.TaskContext{Context: context.Background(), Task: task, Service: taskSvc}); err != nil {
		t.Fatal(err)
	}
	latest, err := taskSvc.Get(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if latest.Status != tasks.StatusCompleted || !strings.Contains(latest.Summary, "disabled") {
		t.Fatalf("disabled run must complete with disabled summary, got %#v", latest)
	}
	state, ok := store.lastState()
	if !ok || state.LastRunStatus != RunStatusDisabled {
		t.Fatalf("last run state = %#v, want disabled", state)
	}
}

// TestRunAutoUpdateTaskSerialPhasesContinueOnFailure 覆盖：对象级开关过滤、
// 阶段失败继续、软件包逐服务器串行且单台失败不跳过其余、运行态 running→failed。
func TestRunAutoUpdateTaskSerialPhasesContinueOnFailure(t *testing.T) {
	taskSvc, _ := newTaskService(t)
	registerTestTaskTypes(t, taskSvc,
		"image_refresh", "package_refresh", "package_upgrade_security", "package_upgrade_all", "application_image_upgrade_all")
	store := &fakeSettingsStore{rt: settings.RuntimeSettings{AutoUpdate: settings.RuntimeAutoUpdateSettings{
		AppsEnabled:     true,
		PackagesEnabled: true,
		PackagesMode:    settings.AutoUpdatePackagesModeSecurity,
		ScheduleTime:    "04:00",
	}}}
	appLister := fakeApplicationLister{apps: []applications.Application{
		{ID: "app-1", Name: "one", Enabled: true, AutoUpdateEnabled: true, ImageUpdateAvailable: true},
		{ID: "app-2", Name: "two", Enabled: true, AutoUpdateEnabled: false, ImageUpdateAvailable: true},
		{ID: "app-3", Name: "three", Enabled: true, AutoUpdateEnabled: true, ImageUpdateAvailable: false},
	}}
	appUpgrader := &fakeApplicationUpgrader{t: t, taskSvc: taskSvc, taskStatus: tasks.StatusFailed}
	imageRefresher := &fakeImageRefresher{t: t, taskSvc: taskSvc}
	packageMaintainer := &fakePackageMaintainer{
		t: t, taskSvc: taskSvc,
		failServers: map[string]bool{"srv-a": true},
	}
	reverseProxy := &fakeReverseProxyUpdater{err: errors.New("registry unreachable")}
	loc := fixedZone()
	svc := NewService(taskSvc, store,
		WithServerLister(fakeServerLister{servers: []server.Server{testServer("srv-a", "a"), testServer("srv-b", "b")}}),
		WithApplicationLister(appLister),
		WithApplicationUpgrader(appUpgrader),
		WithImageRefresher(imageRefresher),
		WithPackageMaintainer(packageMaintainer),
		WithReverseProxyImageUpdater(reverseProxy),
		WithClock(func() time.Time { return at(4, 0, 30, loc) }),
		WithPhaseTimeouts(2*time.Second, 2*time.Second, 2*time.Second, 2*time.Second),
		WithWaitPollInterval(5*time.Millisecond),
	)
	svc.RegisterTasks(taskSvc)
	task, err := taskSvc.Create(context.Background(), tasks.CreateInput{Type: TaskTypeAutoUpdateRun, Summary: "Automatic updates"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.RunAutoUpdateTask(tasks.TaskContext{Context: context.Background(), Task: task, Service: taskSvc}); err == nil {
		t.Fatal("a run with failed phases must return an error")
	}
	latest, err := taskSvc.Get(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if latest.Status != tasks.StatusFailed {
		t.Fatalf("task status = %q, want failed; error=%q", latest.Status, latest.Error)
	}
	for _, want := range []string{"reverse proxy image", "apt upgrade failed for srv-a"} {
		if !strings.Contains(latest.Error, want) {
			t.Fatalf("task error %q does not contain %q", latest.Error, want)
		}
	}

	// 对象级开关过滤：只把 app-1 交给升级器。
	if len(appUpgrader.ids) != 1 || appUpgrader.ids[0] != "app-1" {
		t.Fatalf("application ids passed to upgrader = %#v, want [app-1]", appUpgrader.ids)
	}
	// 阶段失败继续：入口代理与软件包阶段都执行了。
	if reverseProxy.calls != 1 {
		t.Fatalf("reverse proxy phase calls = %d, want 1", reverseProxy.calls)
	}
	// 软件包逐服务器串行：srv-a 失败后仍处理 srv-b。
	if calls := packageMaintainer.upgradeCalls(); len(calls) != 2 || calls[0] != "security:srv-a" || calls[1] != "security:srv-b" {
		t.Fatalf("package upgrade calls = %#v, want [security:srv-a security:srv-b]", calls)
	}
	if len(imageRefresher.servers) != 2 {
		t.Fatalf("image refresh servers = %#v, want both servers", imageRefresher.servers)
	}
	// 运行态：开始 running，结束 failed，且两次记录使用同一个计划运行时间。
	statuses := store.stateStatuses()
	if len(statuses) != 2 || statuses[0] != RunStatusRunning || statuses[1] != RunStatusFailed {
		t.Fatalf("last run statuses = %#v, want [running failed]", statuses)
	}
	state, _ := store.lastState()
	if !state.LastRunAt.Equal(at(4, 0, 30, loc)) {
		t.Fatalf("last run time = %s, want the run start time", state.LastRunAt)
	}
}

// TestRunAutoUpdateTaskPackagesModeAll 验证 all 模式使用 package_upgrade_all。
func TestRunAutoUpdateTaskPackagesModeAll(t *testing.T) {
	taskSvc, _ := newTaskService(t)
	registerTestTaskTypes(t, taskSvc, "package_refresh", "package_upgrade_security", "package_upgrade_all")
	store := &fakeSettingsStore{rt: settings.RuntimeSettings{AutoUpdate: settings.RuntimeAutoUpdateSettings{
		PackagesEnabled: true,
		PackagesMode:    settings.AutoUpdatePackagesModeAll,
		ScheduleTime:    "04:00",
	}}}
	packageMaintainer := &fakePackageMaintainer{t: t, taskSvc: taskSvc}
	loc := fixedZone()
	svc := NewService(taskSvc, store,
		WithServerLister(fakeServerLister{servers: []server.Server{testServer("srv-a", "a")}}),
		WithPackageMaintainer(packageMaintainer),
		WithClock(func() time.Time { return at(4, 0, 30, loc) }),
		WithPhaseTimeouts(time.Second, time.Second, time.Second, time.Second),
		WithWaitPollInterval(5*time.Millisecond),
	)
	svc.RegisterTasks(taskSvc)
	task, err := taskSvc.Create(context.Background(), tasks.CreateInput{Type: TaskTypeAutoUpdateRun, Summary: "Automatic updates"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.RunAutoUpdateTask(tasks.TaskContext{Context: context.Background(), Task: task, Service: taskSvc}); err != nil {
		t.Fatal(err)
	}
	latest, err := taskSvc.Get(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if latest.Status != tasks.StatusCompleted {
		t.Fatalf("task status = %q, want completed; error=%q", latest.Status, latest.Error)
	}
	if calls := packageMaintainer.upgradeCalls(); len(calls) != 1 || calls[0] != "all:srv-a" {
		t.Fatalf("package upgrade calls = %#v, want [all:srv-a]", calls)
	}
	if state, _ := store.lastState(); state.LastRunStatus != RunStatusCompleted {
		t.Fatalf("last run status = %q, want completed", state.LastRunStatus)
	}
}

// TestRunAutoUpdateNowUsesManualTriggerAndTemporaryOverrides 覆盖手动「立即运行
// 一次」：跳过计划时间窗口与当日去重（当前设置已关且当日已运行过仍创建任务）、
// 任务 TriggerType=user、ParamsJSON.trigger=manual、执行阶段使用任务参数中的
// 有效设置（全量升级），并仍写 lastRun（running→completed）。
func TestRunAutoUpdateNowUsesManualTriggerAndTemporaryOverrides(t *testing.T) {
	taskSvc, db := newTaskService(t)
	registerTestTaskTypes(t, taskSvc, "package_refresh", "package_upgrade_security", "package_upgrade_all")
	loc := fixedZone()
	store := &fakeSettingsStore{rt: settings.RuntimeSettings{AutoUpdate: settings.RuntimeAutoUpdateSettings{
		PackagesMode: settings.AutoUpdatePackagesModeSecurity,
		ScheduleTime: "04:00",
		LastRunAt:    at(2, 0, 0, loc).UTC().Format(time.RFC3339Nano),
	}}}
	packageMaintainer := &fakePackageMaintainer{t: t, taskSvc: taskSvc}
	svc := NewService(taskSvc, store,
		WithServerLister(fakeServerLister{servers: []server.Server{testServer("srv-a", "a")}}),
		WithPackageMaintainer(packageMaintainer),
		WithClock(func() time.Time { return at(10, 0, 0, loc) }),
		WithPhaseTimeouts(time.Second, time.Second, time.Second, time.Second),
		WithWaitPollInterval(5*time.Millisecond),
	)
	svc.RegisterTasks(taskSvc)

	rt := settings.RuntimeAutoUpdateSettings{
		AppsEnabled:     false,
		PackagesEnabled: true,
		PackagesMode:    settings.AutoUpdatePackagesModeAll,
		ScheduleTime:    "04:00",
	}
	taskID, err := svc.RunAutoUpdateNow(context.Background(), rt)
	if err != nil {
		t.Fatal(err)
	}
	if taskID == "" {
		t.Fatal("manual run must return the created task id")
	}
	latest := waitForTaskTerminal(t, taskSvc, taskID)
	if latest.TriggerType != triggerTypeUser {
		t.Fatalf("task TriggerType = %q, want %q", latest.TriggerType, triggerTypeUser)
	}
	var params autoUpdateParams
	if err := json.Unmarshal([]byte(latest.ParamsJSON), &params); err != nil {
		t.Fatalf("task ParamsJSON = %q: %v", latest.ParamsJSON, err)
	}
	if params.Trigger != triggerTypeManual || !params.PackagesEnabled || params.PackagesMode != settings.AutoUpdatePackagesModeAll {
		t.Fatalf("manual run params = %#v, want trigger=manual with packages all", params)
	}
	// 执行阶段使用任务参数里的临时覆盖（全量升级），而不是当前关闭的设置。
	if calls := packageMaintainer.upgradeCalls(); len(calls) != 1 || calls[0] != "all:srv-a" {
		t.Fatalf("package upgrade calls = %#v, want [all:srv-a]", calls)
	}
	if latest.Status != tasks.StatusCompleted {
		t.Fatalf("task status = %q, want completed; error=%q", latest.Status, latest.Error)
	}
	// 手动运行仍写 lastRun：开始 running、结束 completed，且 taskId/时间一致。
	state, ok := store.lastState()
	if !ok || state.LastRunTaskID != taskID || state.LastRunStatus != RunStatusCompleted {
		t.Fatalf("last run state = %#v, want completed for %s", state, taskID)
	}
	if !state.LastRunAt.Equal(at(10, 0, 0, loc)) {
		t.Fatalf("last run time = %s, want the manual run start time", state.LastRunAt)
	}
	if statuses := store.stateStatuses(); len(statuses) != 2 || statuses[0] != RunStatusRunning || statuses[1] != RunStatusCompleted {
		t.Fatalf("last run statuses = %#v, want [running completed]", statuses)
	}
	if count := countAutoUpdateRuns(t, db); count != 1 {
		t.Fatalf("auto_update_run rows = %d, want 1", count)
	}
}

// TestRunAutoUpdateNowReturnsActiveTaskID 验证与调度触发共用全局互斥：已有活跃
// auto_update_run（例如调度刚创建、尚未执行完）时手动立即运行幂等返回其 taskId，
// 不创建新任务、也不重复派发。
func TestRunAutoUpdateNowReturnsActiveTaskID(t *testing.T) {
	taskSvc, db := newTaskService(t)
	loc := fixedZone()
	store := &fakeSettingsStore{rt: settings.RuntimeSettings{AutoUpdate: settings.RuntimeAutoUpdateSettings{
		AppsEnabled:  true,
		PackagesMode: settings.AutoUpdatePackagesModeSecurity,
		ScheduleTime: "04:00",
	}}}
	svc := NewService(taskSvc, store, WithClock(func() time.Time { return at(10, 0, 0, loc) }))
	svc.RegisterTasks(taskSvc)

	// 模拟已有活跃任务（queued）：它与手动运行共享 type:auto_update_run 并发键。
	existing, err := taskSvc.Create(context.Background(), tasks.CreateInput{
		Type:        TaskTypeAutoUpdateRun,
		TriggerType: triggerTypeScheduler,
		Summary:     "Automatic updates",
	})
	if err != nil {
		t.Fatal(err)
	}
	taskID, err := svc.RunAutoUpdateNow(context.Background(), settings.RuntimeAutoUpdateSettings{
		AppsEnabled:  true,
		PackagesMode: settings.AutoUpdatePackagesModeSecurity,
		ScheduleTime: "04:00",
	})
	if err != nil {
		t.Fatal(err)
	}
	if taskID != existing.ID {
		t.Fatalf("manual run task id = %q, want active task id %q", taskID, existing.ID)
	}
	if count := countAutoUpdateRuns(t, db); count != 1 {
		t.Fatalf("auto_update_run rows = %d, want 1 (idempotent)", count)
	}
	latest, err := taskSvc.Get(context.Background(), existing.ID)
	if err != nil {
		t.Fatal(err)
	}
	if latest.Status != tasks.StatusQueued {
		t.Fatalf("idempotent call must not dispatch the active task, status = %q", latest.Status)
	}
}

func waitForTaskTerminal(t *testing.T, taskSvc *tasks.Service, taskID string) tasks.Task {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		task, err := taskSvc.Get(context.Background(), taskID)
		if err == nil && taskFinished(task.Status) {
			return task
		}
		time.Sleep(5 * time.Millisecond)
	}
	task, _ := taskSvc.Get(context.Background(), taskID)
	t.Fatalf("task %s did not reach a terminal status, got %#v", taskID, task)
	return task
}

func waitForLastRunStatus(t *testing.T, store *fakeSettingsStore, status string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if state, ok := store.lastState(); ok && state.LastRunStatus == status {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	state, _ := store.lastState()
	t.Fatalf("last run status did not become %q, got %#v", status, state)
}

func countAutoUpdateRuns(t *testing.T, db *sql.DB) int {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM event_runs WHERE type = ?`, TaskTypeAutoUpdateRun).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}
