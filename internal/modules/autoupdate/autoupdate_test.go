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
// RecordAutoUpdateLastRun/RecordAutoUpdateAutoRun 记录每次运行态写入并更新快照
// （等价 settings 服务）。
type fakeSettingsStore struct {
	mu       sync.Mutex
	rt       settings.RuntimeSettings
	states   []settings.RuntimeAutoUpdateRunState
	autoRuns []time.Time
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
	if summary := strings.TrimSpace(state.LastRunSummary); summary != "" {
		f.rt.AutoUpdate.LastRunSummary = summary
	}
	return nil
}

func (f *fakeSettingsStore) RecordAutoUpdateAutoRun(_ context.Context, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.autoRuns = append(f.autoRuns, at)
	f.rt.AutoUpdate.LastAutoRunAt = at.UTC().Format(time.RFC3339Nano)
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

func (f *fakeSettingsStore) autoRunTimes() []time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]time.Time{}, f.autoRuns...)
}

func (f *fakeSettingsStore) lastAutoRunAt() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.rt.AutoUpdate.LastAutoRunAt
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

// fakeApplicationLister 是可变的应用清单：应用镜像刷新阶段会写回最新
// ImageUpdateAvailable，后续应用阶段必须读到刷新后的值。
type fakeApplicationLister struct {
	mu   sync.Mutex
	apps []applications.Application
}

func (f *fakeApplicationLister) List(context.Context) ([]applications.Application, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]applications.Application{}, f.apps...), nil
}

func (f *fakeApplicationLister) markUpdateAvailable(ids ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	wanted := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		wanted[id] = struct{}{}
	}
	for i := range f.apps {
		if _, ok := wanted[f.apps[i].ID]; ok {
			f.apps[i].ImageUpdateAvailable = true
		}
	}
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

// fakeApplicationImageRefresher 模拟 applications.Service.RefreshImageUpdates：
// 刷新时把应用标记为「有可用镜像更新」，并返回 application_image_check 任务 ID。
type fakeApplicationImageRefresher struct {
	t       *testing.T
	taskSvc *tasks.Service
	lister  *fakeApplicationLister
	ids     []string
	err     error
	calls   int
}

func (f *fakeApplicationImageRefresher) RefreshImageUpdates(context.Context) ([]string, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	f.lister.markUpdateAvailable(f.ids...)
	taskIDs := make([]string, 0, len(f.ids))
	for range f.ids {
		task := createFinishedTask(f.t, f.taskSvc, "application_image_check", tasks.StatusCompleted)
		taskIDs = append(taskIDs, task.ID)
	}
	return taskIDs, nil
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

// TestShouldTriggerTriggersAfterScheduleTime 验证触发条件：计划 HH:MM 之前的
// tick 不触发；到达计划时刻后当天任意时间（不再有 2 分钟窗口）都触发一次。
func TestShouldTriggerTriggersAfterScheduleTime(t *testing.T) {
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
		{name: "just after schedule", now: at(4, 0, 30, loc), want: true},
		{name: "long after schedule", now: at(10, 0, 0, loc), want: true},
		{name: "late night same day", now: at(23, 59, 0, loc), want: true},
	}
	for _, tc := range cases {
		if got := shouldTrigger(rt, tc.now); got != tc.want {
			t.Fatalf("%s: shouldTrigger(%s) = %v, want %v", tc.name, tc.now.Format(time.RFC3339), got, tc.want)
		}
	}
	// 非法计划时间不得触发（旧库脏数据不阻塞启动，但也不自动运行）。
	rt.ScheduleTime = "99:99"
	if shouldTrigger(rt, at(10, 0, 0, loc)) {
		t.Fatal("an invalid schedule time must not trigger")
	}
}

func TestShouldTriggerRequiresEnabledSwitch(t *testing.T) {
	now := at(4, 0, 30, fixedZone())
	rt := settings.RuntimeAutoUpdateSettings{PackagesMode: settings.AutoUpdatePackagesModeSecurity, ScheduleTime: "04:00"}
	if shouldTrigger(rt, now) {
		t.Fatal("both switches off must not trigger")
	}
	rt.PackagesEnabled = true
	if !shouldTrigger(rt, now) {
		t.Fatal("packages switch on must trigger after the schedule time")
	}
	rt.PackagesEnabled = false
	rt.AppsEnabled = true
	if !shouldTrigger(rt, now) {
		t.Fatal("apps switch on must trigger after the schedule time")
	}
}

// TestShouldTriggerDeduplicatesAutoRunNotManualRun 验证当日去重只认
// lastAutoRunAt（调度自动运行），手动运行写入的 lastRunAt 不抑制当天计划。
func TestShouldTriggerDeduplicatesAutoRunNotManualRun(t *testing.T) {
	loc := fixedZone()
	now := at(10, 0, 0, loc)
	rt := settings.RuntimeAutoUpdateSettings{
		AppsEnabled:  true,
		PackagesMode: settings.AutoUpdatePackagesModeSecurity,
		ScheduleTime: "04:00",
	}
	// 手动运行（同一天、只写 lastRunAt）不得抑制当天的计划自动运行。
	rt.LastRunAt = at(2, 0, 0, loc).UTC().Format(time.RFC3339Nano)
	if !shouldTrigger(rt, now) {
		t.Fatal("a manual run recorded in lastRunAt must not suppress the scheduled run")
	}
	// 调度自动运行已记录（本地同日）→ 不重复。
	rt.LastAutoRunAt = at(4, 0, 0, loc).UTC().Format(time.RFC3339Nano)
	if shouldTrigger(rt, now) {
		t.Fatal("an automatic run already recorded for the local day must not trigger again")
	}
	// 前一个本地日期 → 允许运行。
	rt.LastAutoRunAt = time.Date(2026, 11, 18, 4, 0, 0, 0, loc).UTC().Format(time.RFC3339Nano)
	if !shouldTrigger(rt, now) {
		t.Fatal("a previous-day automatic run must not block today's run")
	}
	// 脏数据视为没有记录（宁可多跑一次也不永久跳过）。
	rt.LastAutoRunAt = "not-a-timestamp"
	if !shouldTrigger(rt, now) {
		t.Fatal("invalid lastAutoRunAt must be treated as no record")
	}
	rt.LastAutoRunAt = ""
	if !shouldTrigger(rt, now) {
		t.Fatal("empty lastAutoRunAt must be treated as no record")
	}
}

// TestTickCreatesOncePerDayAndSurvivesRestart 覆盖调度器：命中计划时间创建一次
// 任务并写 lastAutoRunAt、同一本地日期第二次 tick 去重、重启后仍按持久化
// lastAutoRunAt 去重。
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
	waitForAutoRunRecorded(t, store)
	if runs := store.autoRunTimes(); len(runs) != 1 || !runs[0].Equal(now) {
		t.Fatalf("recorded automatic run times = %#v, want exactly [%s]", runs, now)
	}
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
		t.Fatalf("restart tick must dedupe using persisted lastAutoRunAt (created=%v err=%v)", created, err)
	}
	if count := countAutoUpdateRuns(t, db); count != 1 {
		t.Fatalf("auto_update_run rows after restart tick = %d, want 1", count)
	}
}

// TestTickBackfillsMissedWindowSameDay 验证错过计划时刻（面板重启/休眠/刚设置
// 时间）后，当天剩余时间仍会补跑一次，而不是直接等到次日。
func TestTickBackfillsMissedWindowSameDay(t *testing.T) {
	taskSvc, db := newTaskService(t)
	loc := fixedZone()
	store := &fakeSettingsStore{rt: settings.RuntimeSettings{AutoUpdate: settings.RuntimeAutoUpdateSettings{
		AppsEnabled:  true,
		PackagesMode: settings.AutoUpdatePackagesModeSecurity,
		ScheduleTime: "04:00",
	}}}
	svc := NewService(taskSvc, store,
		WithClock(func() time.Time { return at(10, 0, 0, loc) }),
		WithPhaseTimeouts(time.Second, time.Second, time.Second, time.Second),
		WithWaitPollInterval(5*time.Millisecond))
	svc.RegisterTasks(taskSvc)

	task, created, err := svc.tick(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("a missed schedule window must still be backfilled later the same day")
	}
	waitForTaskTerminal(t, taskSvc, task.ID)
	if count := countAutoUpdateRuns(t, db); count != 1 {
		t.Fatalf("auto_update_run rows = %d, want 1", count)
	}
	// 当天已自动运行过 → 当天剩余时间不再重复。
	if _, created, err := svc.tick(context.Background()); err != nil || created {
		t.Fatalf("same-day tick after backfill must dedupe (created=%v err=%v)", created, err)
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

// TestTickDispatchesExistingFailedRetryableTask 验证调度 tick 命中已有
// failed_retryable 协调任务（并发键去重、created=false）时补派发一次，
// 避免失败任务永久卡住当天的自动更新。
func TestTickDispatchesExistingFailedRetryableTask(t *testing.T) {
	taskSvc, db := newTaskService(t)
	registerTestTaskTypes(t, taskSvc, "package_refresh", "package_upgrade_security")
	loc := fixedZone()
	now := at(4, 0, 30, loc)
	store := &fakeSettingsStore{rt: settings.RuntimeSettings{AutoUpdate: settings.RuntimeAutoUpdateSettings{
		PackagesEnabled: true,
		PackagesMode:    settings.AutoUpdatePackagesModeSecurity,
		ScheduleTime:    "04:00",
	}}}
	packageMaintainer := &fakePackageMaintainer{t: t, taskSvc: taskSvc}
	svc := NewService(taskSvc, store,
		WithServerLister(fakeServerLister{servers: []server.Server{testServer("srv-a", "a")}}),
		WithPackageMaintainer(packageMaintainer),
		WithClock(func() time.Time { return now }),
		WithPhaseTimeouts(time.Second, time.Second, time.Second, time.Second),
		WithWaitPollInterval(5*time.Millisecond),
	)
	svc.RegisterTasks(taskSvc)

	existing, _, err := tasks.NewManager(taskSvc).Create(context.Background(), tasks.CreateInput{
		Type:        TaskTypeAutoUpdateRun,
		Status:      tasks.StatusFailedRetryable,
		TriggerType: triggerTypeScheduler,
		Summary:     "Automatic updates",
		ParamsJSON:  paramsFor(store.Runtime().AutoUpdate, triggerTypeScheduler),
	}, tasks.Trigger{Type: triggerTypeScheduler, Periodic: true})
	if err != nil {
		t.Fatal(err)
	}
	if existing.Status != tasks.StatusFailedRetryable {
		t.Fatalf("created existing task status = %q, want failed_retryable", existing.Status)
	}

	task, created, err := svc.tick(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Fatal("tick must reuse the existing active task instead of creating a new one")
	}
	if task.ID != existing.ID {
		t.Fatalf("tick task id = %q, want existing id %q", task.ID, existing.ID)
	}
	latest := waitForTaskTerminal(t, taskSvc, existing.ID)
	if latest.Status != tasks.StatusCompleted {
		t.Fatalf("redriven task status = %q, want completed; error=%q", latest.Status, latest.Error)
	}
	waitForAutoRunRecorded(t, store)
	if count := countAutoUpdateRuns(t, db); count != 1 {
		t.Fatalf("auto_update_run rows = %d, want 1", count)
	}
	if calls := packageMaintainer.upgradeCalls(); len(calls) != 1 || calls[0] != "security:srv-a" {
		t.Fatalf("package upgrade calls = %#v, want [security:srv-a]", calls)
	}
}

// TestManualRunDoesNotSuppressScheduledRun 覆盖核心缺陷修复：手动「立即运行一次」
// 只写 lastRunAt，不得写 lastAutoRunAt；当天稍后的计划自动运行仍必须触发。
func TestManualRunDoesNotSuppressScheduledRun(t *testing.T) {
	taskSvc, db := newTaskService(t)
	registerTestTaskTypes(t, taskSvc, "package_refresh", "package_upgrade_security")
	loc := fixedZone()
	now := at(2, 0, 0, loc)
	store := &fakeSettingsStore{rt: settings.RuntimeSettings{AutoUpdate: settings.RuntimeAutoUpdateSettings{
		PackagesEnabled: true,
		PackagesMode:    settings.AutoUpdatePackagesModeSecurity,
		ScheduleTime:    "04:00",
	}}}
	packageMaintainer := &fakePackageMaintainer{t: t, taskSvc: taskSvc}
	svc := NewService(taskSvc, store,
		WithServerLister(fakeServerLister{servers: []server.Server{testServer("srv-a", "a")}}),
		WithPackageMaintainer(packageMaintainer),
		WithClock(func() time.Time { return now }),
		WithPhaseTimeouts(time.Second, time.Second, time.Second, time.Second),
		WithWaitPollInterval(5*time.Millisecond),
	)
	svc.RegisterTasks(taskSvc)

	taskID, err := svc.RunAutoUpdateNow(context.Background(), store.Runtime().AutoUpdate)
	if err != nil {
		t.Fatal(err)
	}
	if taskID == "" {
		t.Fatal("manual run must return the created task id")
	}
	waitForTaskTerminal(t, taskSvc, taskID)
	waitForLastRunStatus(t, store, RunStatusCompleted)
	if runs := store.autoRunTimes(); len(runs) != 0 {
		t.Fatalf("manual run must not record lastAutoRunAt, got %#v", runs)
	}
	if last, _ := store.lastState(); last.LastRunSummary == "" {
		t.Fatal("manual run must record an end-of-run summary")
	}

	// 当天计划时间到达：手动运行不抑制计划自动运行。
	svc.now = func() time.Time { return at(4, 0, 30, loc) }
	task, created, err := svc.tick(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("a manual run must not suppress the scheduled run later the same day")
	}
	waitForTaskTerminal(t, taskSvc, task.ID)
	waitForAutoRunRecorded(t, store)
	if count := countAutoUpdateRuns(t, db); count != 2 {
		t.Fatalf("auto_update_run rows = %d, want 2 (manual + scheduled)", count)
	}
	// 计划运行之后同一天不再重复。
	if _, created, err := svc.tick(context.Background()); err != nil || created {
		t.Fatalf("same-day scheduled tick must dedupe (created=%v err=%v)", created, err)
	}
}

// TestRunAutoUpdateNowDispatchesExistingQueuedTask 验证手动运行遇到已存在但未
// 运行中的任务（queued）时补派发，而不是静默返回一个永远不会执行的 taskId。
func TestRunAutoUpdateNowDispatchesExistingQueuedTask(t *testing.T) {
	taskSvc, db := newTaskService(t)
	registerTestTaskTypes(t, taskSvc, "package_refresh", "package_upgrade_security")
	loc := fixedZone()
	store := &fakeSettingsStore{rt: settings.RuntimeSettings{AutoUpdate: settings.RuntimeAutoUpdateSettings{
		PackagesEnabled: true,
		PackagesMode:    settings.AutoUpdatePackagesModeSecurity,
		ScheduleTime:    "04:00",
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

	// 模拟调度刚创建、尚未派发的 queued 任务：与手动运行共享并发键。
	existing, err := taskSvc.Create(context.Background(), tasks.CreateInput{
		Type:        TaskTypeAutoUpdateRun,
		TriggerType: triggerTypeScheduler,
		Summary:     "Automatic updates",
	})
	if err != nil {
		t.Fatal(err)
	}
	taskID, err := svc.RunAutoUpdateNow(context.Background(), settings.RuntimeAutoUpdateSettings{
		PackagesEnabled: true,
		PackagesMode:    settings.AutoUpdatePackagesModeSecurity,
		ScheduleTime:    "04:00",
	})
	if err != nil {
		t.Fatal(err)
	}
	if taskID != existing.ID {
		t.Fatalf("manual run task id = %q, want existing task id %q", taskID, existing.ID)
	}
	latest := waitForTaskTerminal(t, taskSvc, existing.ID)
	if latest.Status != tasks.StatusCompleted {
		t.Fatalf("queued existing task status = %q, want completed; error=%q", latest.Status, latest.Error)
	}
	if count := countAutoUpdateRuns(t, db); count != 1 {
		t.Fatalf("auto_update_run rows = %d, want 1 (no duplicate task)", count)
	}
	if calls := packageMaintainer.upgradeCalls(); len(calls) != 1 {
		t.Fatalf("package upgrade calls = %#v, want exactly 1 dispatch", calls)
	}
}

// TestRunAutoUpdateNowReturnsRunningTaskID 验证已有运行中的协调任务时手动运行
// 幂等返回其 ID，不重复派发。
func TestRunAutoUpdateNowReturnsRunningTaskID(t *testing.T) {
	taskSvc, db := newTaskService(t)
	loc := fixedZone()
	store := &fakeSettingsStore{rt: settings.RuntimeSettings{AutoUpdate: settings.RuntimeAutoUpdateSettings{
		AppsEnabled:  true,
		PackagesMode: settings.AutoUpdatePackagesModeSecurity,
		ScheduleTime: "04:00",
	}}}
	svc := NewService(taskSvc, store, WithClock(func() time.Time { return at(10, 0, 0, loc) }))
	svc.RegisterTasks(taskSvc)

	running, err := taskSvc.Create(context.Background(), tasks.CreateInput{
		Type:        TaskTypeAutoUpdateRun,
		Status:      tasks.StatusRunning,
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
	if taskID != running.ID {
		t.Fatalf("manual run task id = %q, want running task id %q", taskID, running.ID)
	}
	if count := countAutoUpdateRuns(t, db); count != 1 {
		t.Fatalf("auto_update_run rows = %d, want 1 (idempotent)", count)
	}
	latest, err := taskSvc.Get(context.Background(), running.ID)
	if err != nil {
		t.Fatal(err)
	}
	if latest.Status != tasks.StatusRunning {
		t.Fatalf("idempotent call must not restart the running task, status = %q", latest.Status)
	}
}

// TestRunAutoUpdateTaskRefreshesApplicationImagesBeforeAppPhase 覆盖核心缺陷修复：
// 刷新阶段必须请求应用镜像 digest 刷新，应用阶段才能使用刷新后的
// ImageUpdateAvailable 筛到应用。
func TestRunAutoUpdateTaskRefreshesApplicationImagesBeforeAppPhase(t *testing.T) {
	taskSvc, _ := newTaskService(t)
	registerTestTaskTypes(t, taskSvc, "application_image_check", "application_image_upgrade_all")
	store := &fakeSettingsStore{rt: settings.RuntimeSettings{AutoUpdate: settings.RuntimeAutoUpdateSettings{
		AppsEnabled:  true,
		PackagesMode: settings.AutoUpdatePackagesModeSecurity,
		ScheduleTime: "04:00",
	}}}
	appLister := &fakeApplicationLister{apps: []applications.Application{
		{ID: "app-1", Name: "one", Enabled: true, AutoUpdateEnabled: true, ImageUpdateAvailable: false},
		{ID: "app-2", Name: "two", Enabled: true, AutoUpdateEnabled: true, ImageUpdateAvailable: false},
	}}
	appRefresher := &fakeApplicationImageRefresher{
		t: t, taskSvc: taskSvc, lister: appLister,
		ids: []string{"app-1", "app-2"},
	}
	appUpgrader := &fakeApplicationUpgrader{t: t, taskSvc: taskSvc, taskStatus: tasks.StatusCompleted}
	svc := NewService(taskSvc, store,
		WithApplicationLister(appLister),
		WithApplicationUpgrader(appUpgrader),
		WithApplicationImageRefresher(appRefresher),
		WithClock(func() time.Time { return at(4, 0, 30, fixedZone()) }),
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
	if appRefresher.calls != 1 {
		t.Fatalf("application image refresh calls = %d, want 1", appRefresher.calls)
	}
	// 刷新后 ImageUpdateAvailable 才为 true，应用阶段因此筛到两个应用。
	if len(appUpgrader.ids) != 2 || appUpgrader.ids[0] != "app-1" || appUpgrader.ids[1] != "app-2" {
		t.Fatalf("application ids passed to upgrader = %#v, want [app-1 app-2]", appUpgrader.ids)
	}
	state, ok := store.lastState()
	if !ok || state.LastRunStatus != RunStatusCompleted {
		t.Fatalf("last run state = %#v, want completed", state)
	}
	if !strings.Contains(state.LastRunSummary, "2 application image update(s) planned") {
		t.Fatalf("last run summary = %q, want planned application count", state.LastRunSummary)
	}
}

// TestRunAutoUpdateTaskApplicationRefreshFailureDoesNotBlock 验证应用镜像刷新失败
// 只记日志：任务仍完成，应用阶段使用未刷新的缓存（筛不到更新），不阻断软件包阶段。
func TestRunAutoUpdateTaskApplicationRefreshFailureDoesNotBlock(t *testing.T) {
	taskSvc, _ := newTaskService(t)
	registerTestTaskTypes(t, taskSvc, "application_image_upgrade_all", "package_refresh", "package_upgrade_security")
	store := &fakeSettingsStore{rt: settings.RuntimeSettings{AutoUpdate: settings.RuntimeAutoUpdateSettings{
		AppsEnabled:     true,
		PackagesEnabled: true,
		PackagesMode:    settings.AutoUpdatePackagesModeSecurity,
		ScheduleTime:    "04:00",
	}}}
	appLister := &fakeApplicationLister{apps: []applications.Application{
		{ID: "app-1", Name: "one", Enabled: true, AutoUpdateEnabled: true, ImageUpdateAvailable: false},
	}}
	appRefresher := &fakeApplicationImageRefresher{
		t: t, taskSvc: taskSvc, lister: appLister,
		ids: []string{"app-1"},
		err: errors.New("registry unreachable"),
	}
	appUpgrader := &fakeApplicationUpgrader{t: t, taskSvc: taskSvc, taskStatus: tasks.StatusCompleted}
	packageMaintainer := &fakePackageMaintainer{t: t, taskSvc: taskSvc}
	svc := NewService(taskSvc, store,
		WithServerLister(fakeServerLister{servers: []server.Server{testServer("srv-a", "a")}}),
		WithApplicationLister(appLister),
		WithApplicationUpgrader(appUpgrader),
		WithApplicationImageRefresher(appRefresher),
		WithPackageMaintainer(packageMaintainer),
		WithClock(func() time.Time { return at(4, 0, 30, fixedZone()) }),
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
		t.Fatalf("task status = %q, want completed despite refresh failure; error=%q", latest.Status, latest.Error)
	}
	if len(appUpgrader.ids) != 0 {
		t.Fatalf("stale cache must not plan upgrades, got %#v", appUpgrader.ids)
	}
	if calls := packageMaintainer.upgradeCalls(); len(calls) != 1 || calls[0] != "security:srv-a" {
		t.Fatalf("package phase must continue after refresh failure, calls = %#v", calls)
	}
	state, _ := store.lastState()
	if !strings.Contains(state.LastRunSummary, "0 application image update(s) planned") {
		t.Fatalf("last run summary = %q, want zero planned applications", state.LastRunSummary)
	}
}

// TestRunAutoUpdateTaskDisabledCompletes 验证执行时重新读取设置：两开关都关时
// 任务直接完成并写 disabled 运行态与摘要。
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
	if state.LastRunSummary == "" {
		t.Fatal("disabled run must record a summary")
	}
}

// TestRunAutoUpdateTaskSerialPhasesContinueOnFailure 覆盖：对象级开关过滤、
// 阶段失败继续、软件包逐服务器串行且单台失败不跳过其余、运行态 running→failed
// 与失败摘要。
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
	appLister := &fakeApplicationLister{apps: []applications.Application{
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
	if !strings.Contains(state.LastRunSummary, "failed") {
		t.Fatalf("failed run summary = %q, want a failure description", state.LastRunSummary)
	}
	// 该任务由调度链路创建（Test 直接建任务，无 manual 参数）→ 记 lastAutoRunAt。
	if runs := store.autoRunTimes(); len(runs) != 1 || !runs[0].Equal(at(4, 0, 30, loc)) {
		t.Fatalf("scheduled run must record lastAutoRunAt once, got %#v", runs)
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
	state, _ := store.lastState()
	if state.LastRunStatus != RunStatusCompleted {
		t.Fatalf("last run status = %q, want completed", state.LastRunStatus)
	}
	if !strings.Contains(state.LastRunSummary, "1 server(s) processed (all)") {
		t.Fatalf("last run summary = %q, want package processing summary", state.LastRunSummary)
	}
}

// TestRunAutoUpdateNowUsesManualTriggerAndTemporaryOverrides 覆盖手动「立即运行
// 一次」：跳过计划时间窗口与当日去重（当前设置已关且当日已运行过仍创建任务）、
// 任务 TriggerType=user、ParamsJSON.trigger=manual、执行阶段使用任务参数中的
// 有效设置（全量升级），写 lastRun（running→completed）但不写 lastAutoRunAt。
func TestRunAutoUpdateNowUsesManualTriggerAndTemporaryOverrides(t *testing.T) {
	taskSvc, db := newTaskService(t)
	registerTestTaskTypes(t, taskSvc, "package_refresh", "package_upgrade_security", "package_upgrade_all")
	loc := fixedZone()
	store := &fakeSettingsStore{rt: settings.RuntimeSettings{AutoUpdate: settings.RuntimeAutoUpdateSettings{
		PackagesMode:  settings.AutoUpdatePackagesModeSecurity,
		ScheduleTime:  "04:00",
		LastRunAt:     at(2, 0, 0, loc).UTC().Format(time.RFC3339Nano),
		LastAutoRunAt: at(3, 0, 0, loc).UTC().Format(time.RFC3339Nano),
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
	// 手动运行不得写 lastAutoRunAt（不能吞掉当天的计划运行）。
	if runs := store.autoRunTimes(); len(runs) != 0 {
		t.Fatalf("manual run must not record lastAutoRunAt, got %#v", runs)
	}
	if count := countAutoUpdateRuns(t, db); count != 1 {
		t.Fatalf("auto_update_run rows = %d, want 1", count)
	}
}

func waitForTaskTerminal(t *testing.T, taskSvc *tasks.Service, taskID string) tasks.Task {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		task, err := taskSvc.Get(context.Background(), taskID)
		// failed_retryable 是「等待重试」而非终态：补派发的任务会从该状态重新
		// 进入 running，因此必须等到 completed/failed/blocked/cancelled 再断言。
		if err == nil && settledStatus(task.Status) {
			return task
		}
		time.Sleep(5 * time.Millisecond)
	}
	task, _ := taskSvc.Get(context.Background(), taskID)
	t.Fatalf("task %s did not reach a terminal status, got %#v", taskID, task)
	return task
}

func settledStatus(status string) bool {
	switch status {
	case tasks.StatusCompleted, tasks.StatusFailed, tasks.StatusBlocked, tasks.StatusCancelled:
		return true
	default:
		return false
	}
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

func waitForAutoRunRecorded(t *testing.T, store *fakeSettingsStore) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if store.lastAutoRunAt() != "" {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("lastAutoRunAt was not recorded for a scheduled run")
}

func countAutoUpdateRuns(t *testing.T, db *sql.DB) int {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM event_runs WHERE type = ?`, TaskTypeAutoUpdateRun).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}
