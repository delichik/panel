package settings

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	panelerr "panel/internal/platform/errors"
)

func boolPtr(value bool) *bool       { return &value }
func stringPtr(value string) *string { return &value }

// TestRuntimeAutoUpdateDefaults 验证新库默认值：两个开关关、仅安全范围、04:00，
// 且默认值已播种到 runtime_settings（旧库升级走 ON CONFLICT DO NOTHING）。
func TestRuntimeAutoUpdateDefaults(t *testing.T) {
	svc := newTestService(t)
	got := svc.Runtime().AutoUpdate
	want := RuntimeAutoUpdateSettings{
		PackagesMode: AutoUpdatePackagesModeSecurity,
		ScheduleTime: DefaultAutoUpdateScheduleTime,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("default auto-update settings = %#v, want %#v", got, want)
	}
	for key, value := range map[string]string{
		RuntimeSettingAutoUpdateAppsEnabled:     "false",
		RuntimeSettingAutoUpdatePackagesEnabled: "false",
		RuntimeSettingAutoUpdatePackagesMode:    AutoUpdatePackagesModeSecurity,
		RuntimeSettingAutoUpdateScheduleTime:    DefaultAutoUpdateScheduleTime,
	} {
		var stored string
		if err := svc.db.QueryRow(`SELECT value FROM runtime_settings WHERE key=?`, key).Scan(&stored); err != nil {
			t.Fatalf("runtime_settings[%s] not seeded: %v", key, err)
		}
		if stored != value {
			t.Fatalf("runtime_settings[%s] = %q, want %q", key, stored, value)
		}
	}
}

// TestRuntimeAutoUpdateSwitchesCanBeTurnedOff 覆盖"关闭开关必须真正落库"：
// 分区负载用指针表达显式 false，绕过 Update 的"零值 = 保留现值"规则。
func TestRuntimeAutoUpdateSwitchesCanBeTurnedOff(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	got, err := svc.UpdateAutoUpdate(ctx, RuntimeAutoUpdateUpdate{
		AppsEnabled:     boolPtr(true),
		PackagesEnabled: boolPtr(true),
		PackagesMode:    stringPtr(AutoUpdatePackagesModeAll),
		ScheduleTime:    stringPtr("05:30"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.AutoUpdate.AppsEnabled || !got.AutoUpdate.PackagesEnabled ||
		got.AutoUpdate.PackagesMode != AutoUpdatePackagesModeAll || got.AutoUpdate.ScheduleTime != "05:30" {
		t.Fatalf("unexpected auto-update settings: %#v", got.AutoUpdate)
	}

	got, err = svc.UpdateAutoUpdate(ctx, RuntimeAutoUpdateUpdate{
		AppsEnabled:     boolPtr(false),
		PackagesEnabled: boolPtr(false),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.AutoUpdate.AppsEnabled || got.AutoUpdate.PackagesEnabled {
		t.Fatalf("switches must be persisted as false, got %#v", got.AutoUpdate)
	}
	if got.AutoUpdate.PackagesMode != AutoUpdatePackagesModeAll || got.AutoUpdate.ScheduleTime != "05:30" {
		t.Fatalf("omitted fields changed: %#v", got.AutoUpdate)
	}
	reloaded, err := NewService(svc.db, svc.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(reloaded.Runtime().AutoUpdate, got.AutoUpdate) {
		t.Fatalf("persisted auto-update settings = %#v, want %#v", reloaded.Runtime().AutoUpdate, got.AutoUpdate)
	}
}

// TestRuntimeAutoUpdateOmitsKeepCurrent 验证省略字段沿用现值（自动保存只提交
// 改动字段时不得把其它字段清零）。
func TestRuntimeAutoUpdateOmitsKeepCurrent(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	if _, err := svc.UpdateAutoUpdate(ctx, RuntimeAutoUpdateUpdate{
		AppsEnabled:  boolPtr(true),
		PackagesMode: stringPtr(AutoUpdatePackagesModeAll),
		ScheduleTime: stringPtr("06:15"),
	}); err != nil {
		t.Fatal(err)
	}
	got, err := svc.UpdateAutoUpdate(ctx, RuntimeAutoUpdateUpdate{PackagesEnabled: boolPtr(true)})
	if err != nil {
		t.Fatal(err)
	}
	if !got.AutoUpdate.AppsEnabled || !got.AutoUpdate.PackagesEnabled ||
		got.AutoUpdate.PackagesMode != AutoUpdatePackagesModeAll || got.AutoUpdate.ScheduleTime != "06:15" {
		t.Fatalf("omitted fields changed: %#v", got.AutoUpdate)
	}
	// 显式空串仍视为"保留现值"，避免前端清空控件导致校验失败。
	got, err = svc.UpdateAutoUpdate(ctx, RuntimeAutoUpdateUpdate{PackagesMode: stringPtr(""), ScheduleTime: stringPtr("  ")})
	if err != nil {
		t.Fatal(err)
	}
	if got.AutoUpdate.PackagesMode != AutoUpdatePackagesModeAll || got.AutoUpdate.ScheduleTime != "06:15" {
		t.Fatalf("empty strings must keep current values: %#v", got.AutoUpdate)
	}
}

// TestRuntimeAutoUpdateValidationErrors 验证新错误码稳定且失败整笔不生效。
func TestRuntimeAutoUpdateValidationErrors(t *testing.T) {
	cases := []struct {
		name  string
		input RuntimeAutoUpdateUpdate
		code  string
	}{
		{name: "invalid schedule hour", input: RuntimeAutoUpdateUpdate{ScheduleTime: stringPtr("24:00")}, code: "invalid_auto_update_schedule_time"},
		{name: "invalid schedule minute", input: RuntimeAutoUpdateUpdate{ScheduleTime: stringPtr("04:60")}, code: "invalid_auto_update_schedule_time"},
		{name: "invalid schedule shape", input: RuntimeAutoUpdateUpdate{ScheduleTime: stringPtr("4:00")}, code: "invalid_auto_update_schedule_time"},
		{name: "invalid mode", input: RuntimeAutoUpdateUpdate{PackagesMode: stringPtr("partial")}, code: "invalid_auto_update_packages_mode"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, baseline := newSeededRuntimeService(t)
			_, err := svc.UpdateAutoUpdate(context.Background(), tc.input)
			if err == nil {
				t.Fatal("expected a validation error")
			}
			var domainErr *panelerr.Error
			if !errors.As(err, &domainErr) {
				t.Fatalf("error is not a domain error: %v", err)
			}
			if domainErr.Code != tc.code || domainErr.HTTPStatus != http.StatusUnprocessableEntity {
				t.Fatalf("error = %#v, want code %q with 422", domainErr, tc.code)
			}
			if !reflect.DeepEqual(svc.Runtime(), baseline) {
				t.Fatalf("rejected update changed runtime snapshot: %#v, want %#v", svc.Runtime(), baseline)
			}
			reloaded, err := NewService(svc.db, svc.cfg)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(reloaded.Runtime(), baseline) {
				t.Fatalf("rejected update changed persistent settings: %#v, want %#v", reloaded.Runtime(), baseline)
			}
		})
	}
}

// TestRuntimeAutoUpdateOldDatabaseSeedsDefaults 模拟旧库缺少新键（升级前的
// runtime_settings）：启动时 ensureDefaultRuntimeSettings 必须播种默认值。
func TestRuntimeAutoUpdateOldDatabaseSeedsDefaults(t *testing.T) {
	svc := newTestService(t)
	if _, err := svc.db.Exec(`DELETE FROM runtime_settings WHERE key IN (?,?,?,?)`,
		RuntimeSettingAutoUpdateAppsEnabled, RuntimeSettingAutoUpdatePackagesEnabled,
		RuntimeSettingAutoUpdatePackagesMode, RuntimeSettingAutoUpdateScheduleTime); err != nil {
		t.Fatal(err)
	}
	upgraded, err := NewService(svc.db, svc.cfg)
	if err != nil {
		t.Fatal(err)
	}
	got := upgraded.Runtime().AutoUpdate
	want := RuntimeAutoUpdateSettings{
		PackagesMode: AutoUpdatePackagesModeSecurity,
		ScheduleTime: DefaultAutoUpdateScheduleTime,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("old database defaults = %#v, want %#v", got, want)
	}
}

// TestRuntimeAutoUpdateNormalizesValues 验证枚举小写归一以及旧库非法值静默归一
// （load 不得因脏数据阻止启动）。
func TestRuntimeAutoUpdateNormalizesValues(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	got, err := svc.UpdateAutoUpdate(ctx, RuntimeAutoUpdateUpdate{
		PackagesMode: stringPtr(" ALL "),
		ScheduleTime: stringPtr(" 04:05 "),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.AutoUpdate.PackagesMode != AutoUpdatePackagesModeAll || got.AutoUpdate.ScheduleTime != "04:05" {
		t.Fatalf("values were not normalized: %#v", got.AutoUpdate)
	}

	// 手工写入旧库非法值：启动时静默回退到默认值而不是启动失败。
	for key, value := range map[string]string{
		RuntimeSettingAutoUpdatePackagesMode: "weird-mode",
		RuntimeSettingAutoUpdateScheduleTime: "99:99",
		RuntimeSettingAutoUpdateAppsEnabled:  "true",
	} {
		if _, err := svc.db.Exec(`INSERT INTO runtime_settings(key,value,updated_at) VALUES(?,?,?)
			ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			t.Fatal(err)
		}
	}
	reloaded, err := NewService(svc.db, svc.cfg)
	if err != nil {
		t.Fatalf("invalid legacy auto-update values must not block startup: %v", err)
	}
	autoUpdate := reloaded.Runtime().AutoUpdate
	if autoUpdate.PackagesMode != AutoUpdatePackagesModeSecurity || autoUpdate.ScheduleTime != DefaultAutoUpdateScheduleTime {
		t.Fatalf("invalid legacy values were not normalized: %#v", autoUpdate)
	}
	if !autoUpdate.AppsEnabled {
		t.Fatalf("valid legacy value appsEnabled=true was lost: %#v", autoUpdate)
	}
}

// TestRuntimeAutoUpdateLastRunIsReadOnlyAndSurvivesPartitionSaves 验证 lastRun*/
// lastAutoRun*/lastRunSummary 只读运行态：RecordAutoUpdateLastRun 与
// RecordAutoUpdateAutoRun 可跨重启读回，普通分区保存/自动更新分区保存都不能
// 覆盖它们。
func TestRuntimeAutoUpdateLastRunIsReadOnlyAndSurvivesPartitionSaves(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	runAt := time.Date(2026, 11, 19, 4, 0, 0, 0, time.UTC)
	summary := "3 application image update(s) planned; 5 server(s) processed (security)"
	if err := svc.RecordAutoUpdateLastRun(ctx, RuntimeAutoUpdateRunState{
		LastRunAt:      runAt,
		LastRunStatus:  "completed",
		LastRunTaskID:  "task-auto-1",
		LastRunSummary: summary,
	}); err != nil {
		t.Fatal(err)
	}
	autoRunAt := time.Date(2026, 11, 19, 4, 0, 1, 0, time.UTC)
	if err := svc.RecordAutoUpdateAutoRun(ctx, autoRunAt); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.UpdateGeneral(ctx, RuntimeGeneralUpdate{LogLevel: "warn"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateAutoUpdate(ctx, RuntimeAutoUpdateUpdate{AppsEnabled: boolPtr(true)}); err != nil {
		t.Fatal(err)
	}
	got := svc.AutoUpdateLastRun()
	if !got.LastRunAt.Equal(runAt) || got.LastRunStatus != "completed" || got.LastRunTaskID != "task-auto-1" {
		t.Fatalf("last run state was clobbered in memory: %#v", got)
	}
	if !got.LastAutoRunAt.Equal(autoRunAt) || got.LastRunSummary != summary {
		t.Fatalf("read-only auto-run state was clobbered in memory: %#v", got)
	}

	reloaded, err := NewService(svc.db, svc.cfg)
	if err != nil {
		t.Fatal(err)
	}
	persisted := reloaded.AutoUpdateLastRun()
	if !persisted.LastRunAt.Equal(runAt) || persisted.LastRunStatus != "completed" || persisted.LastRunTaskID != "task-auto-1" {
		t.Fatalf("last run state was clobbered in persistence: %#v", persisted)
	}
	if !persisted.LastAutoRunAt.Equal(autoRunAt) || persisted.LastRunSummary != summary {
		t.Fatalf("read-only auto-run state was clobbered in persistence: %#v", persisted)
	}
	// 消费端读 RuntimeSettings 也应看到同样的只读运行态。
	runtime := reloaded.Runtime().AutoUpdate
	if runtime.LastRunStatus != "completed" || runtime.LastRunTaskID != "task-auto-1" ||
		runtime.LastAutoRunAt == "" || runtime.LastRunSummary != summary {
		t.Fatalf("runtime snapshot is missing run state: %#v", runtime)
	}
}

// TestRuntimeAutoUpdateRunningWriteKeepsPreviousSummary 验证开始执行（running）
// 时不会清空上一次结束摘要：摘要只在非空时覆盖。
func TestRuntimeAutoUpdateRunningWriteKeepsPreviousSummary(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	runAt := time.Date(2026, 11, 19, 4, 0, 0, 0, time.UTC)
	if err := svc.RecordAutoUpdateLastRun(ctx, RuntimeAutoUpdateRunState{
		LastRunAt:      runAt,
		LastRunStatus:  "completed",
		LastRunTaskID:  "task-auto-1",
		LastRunSummary: "Automatic updates completed",
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.RecordAutoUpdateLastRun(ctx, RuntimeAutoUpdateRunState{
		LastRunAt:     runAt.Add(time.Hour),
		LastRunStatus: "running",
		LastRunTaskID: "task-auto-2",
	}); err != nil {
		t.Fatal(err)
	}
	got := svc.Runtime().AutoUpdate
	if got.LastRunStatus != "running" || got.LastRunTaskID != "task-auto-2" {
		t.Fatalf("running state was not persisted: %#v", got)
	}
	if got.LastRunSummary != "Automatic updates completed" {
		t.Fatalf("running write must keep the previous summary, got %q", got.LastRunSummary)
	}
}

// TestRuntimeAutoUpdateManualRunDoesNotWriteLastAutoRunAt 锁定语义边界：只有
// RecordAutoUpdateAutoRun 会写 lastAutoRunAt，RecordAutoUpdateLastRun（手动运行
// 也调用）绝不写该键。
func TestRuntimeAutoUpdateManualRunDoesNotWriteLastAutoRunAt(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	runAt := time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)
	if err := svc.RecordAutoUpdateLastRun(ctx, RuntimeAutoUpdateRunState{
		LastRunAt:     runAt,
		LastRunStatus: "completed",
		LastRunTaskID: "task-manual-1",
	}); err != nil {
		t.Fatal(err)
	}
	if got := svc.Runtime().AutoUpdate.LastAutoRunAt; got != "" {
		t.Fatalf("manual lastRun write must not set lastAutoRunAt, got %q", got)
	}
	if got := svc.AutoUpdateLastRun().LastRunAt; !got.Equal(runAt) {
		t.Fatalf("lastRunAt = %s, want %s", got, runAt)
	}
}

// TestRuntimeAutoUpdateReadOnlyJSONShape 锁定 GET 响应里新增只读运行态字段，
// 供设置页展示「最近一次自动运行」与「最近一次运行摘要」。
func TestRuntimeAutoUpdateReadOnlyJSONShape(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	if err := svc.RecordAutoUpdateLastRun(ctx, RuntimeAutoUpdateRunState{
		LastRunAt:      time.Date(2026, 11, 19, 4, 0, 0, 0, time.UTC),
		LastRunStatus:  "completed",
		LastRunTaskID:  "task-auto-1",
		LastRunSummary: "Automatic updates completed",
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.RecordAutoUpdateAutoRun(ctx, time.Date(2026, 11, 19, 4, 0, 1, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(svc.Runtime())
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(decoded["autoUpdate"], &fields); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"lastRunAt", "lastRunStatus", "lastRunTaskId", "lastAutoRunAt", "lastRunSummary"} {
		if _, ok := fields[key]; !ok {
			t.Fatalf("autoUpdate JSON is missing read-only field %q: %s", key, decoded["autoUpdate"])
		}
	}
}

// TestRuntimeAutoUpdatePartitionHandler 验证新增分区端点：返回完整
// RuntimeSettings、只改本分区、并写入持久化。
func TestRuntimeAutoUpdatePartitionHandler(t *testing.T) {
	svc, baseline := newSeededRuntimeService(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/settings/runtime/auto-update",
		strings.NewReader(`{"appsEnabled":true,"packagesEnabled":true,"packagesMode":"all","scheduleTime":"06:15"}`))
	NewHandler(svc).UpdateAutoUpdate(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	envelope := decodeRuntimeEnvelope(t, rec)
	if envelope.Error != nil {
		t.Fatalf("unexpected error response: %#v", envelope.Error)
	}
	want := baseline
	want.AutoUpdate = RuntimeAutoUpdateSettings{
		AppsEnabled:     true,
		PackagesEnabled: true,
		PackagesMode:    AutoUpdatePackagesModeAll,
		ScheduleTime:    "06:15",
	}
	if !reflect.DeepEqual(envelope.Data, runtimeSettingsJSONRoundTrip(t, want)) {
		t.Fatalf("response settings = %#v, want %#v", envelope.Data, want)
	}
	if !reflect.DeepEqual(svc.Runtime(), want) {
		t.Fatalf("in-memory settings = %#v, want %#v", svc.Runtime(), want)
	}
	assertCompleteRuntimeSettings(t, envelope.Data)
	reloaded, err := NewService(svc.db, svc.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(reloaded.Runtime(), want) {
		t.Fatalf("persisted settings = %#v, want %#v", reloaded.Runtime(), want)
	}
}

// TestRuntimeAutoUpdatePartitionJSONShape 锁定 API 形态：autoUpdate 下四个可编辑
// 字段 + 三个只读 lastRun 字段，避免前后端契约漂移。
func TestRuntimeAutoUpdatePartitionJSONShape(t *testing.T) {
	svc := newTestService(t)
	raw, err := json.Marshal(svc.Runtime())
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	autoUpdate, ok := decoded["autoUpdate"]
	if !ok {
		t.Fatalf("runtime settings JSON is missing autoUpdate: %s", raw)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(autoUpdate, &fields); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"appsEnabled", "packagesEnabled", "packagesMode", "scheduleTime"} {
		if _, ok := fields[key]; !ok {
			t.Fatalf("autoUpdate JSON is missing %q: %s", key, autoUpdate)
		}
	}
}

func TestParseAutoUpdateScheduleTime(t *testing.T) {
	cases := []struct {
		input  string
		hour   int
		minute int
		ok     bool
	}{
		{input: "04:00", hour: 4, minute: 0, ok: true},
		{input: "23:59", hour: 23, minute: 59, ok: true},
		{input: " 04:05 ", hour: 4, minute: 5, ok: true},
		{input: "24:00", ok: false},
		{input: "4:00", ok: false},
		{input: "04:60", ok: false},
		{input: "", ok: false},
	}
	for _, tc := range cases {
		hour, minute, ok := ParseAutoUpdateScheduleTime(tc.input)
		if ok != tc.ok || (ok && (hour != tc.hour || minute != tc.minute)) {
			t.Fatalf("ParseAutoUpdateScheduleTime(%q) = (%d,%d,%v), want (%d,%d,%v)", tc.input, hour, minute, ok, tc.hour, tc.minute, tc.ok)
		}
	}
}

// fakeAutoUpdateRunner 记录 settings 服务交给 autoupdate 模块的本次有效设置。
// 手动「立即运行一次」的路由/校验在 settings 侧，任务创建在执行侧。
type fakeAutoUpdateRunner struct {
	taskID string
	err    error
	calls  []RuntimeAutoUpdateSettings
}

func (f *fakeAutoUpdateRunner) RunAutoUpdateNow(_ context.Context, rt RuntimeAutoUpdateSettings) (string, error) {
	f.calls = append(f.calls, rt)
	if f.err != nil {
		return "", f.err
	}
	return f.taskID, nil
}

// autoUpdateRunEnvelope 是手动立即运行端点响应信封的最小表示。
type autoUpdateRunEnvelope struct {
	Data struct {
		TaskID string `json:"taskId"`
	} `json:"data"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func decodeAutoUpdateRunEnvelope(t *testing.T, rec *httptest.ResponseRecorder) autoUpdateRunEnvelope {
	t.Helper()
	var envelope autoUpdateRunEnvelope
	if err := json.NewDecoder(rec.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode auto-update run response: %v", err)
	}
	return envelope
}

// TestRunAutoUpdateNowDisabledReturnsStableCode 验证两开关都关时手动立即运行
// 返回 422 auto_update_disabled，且不创建任务（payload 可选，空请求体同样生效）。
func TestRunAutoUpdateNowDisabledReturnsStableCode(t *testing.T) {
	svc := newTestService(t)
	runner := &fakeAutoUpdateRunner{taskID: "task-manual"}
	svc.SetAutoUpdateRunner(runner)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/settings/runtime/auto-update/run", nil)
	NewHandler(svc).RunAutoUpdateNow(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body = %s", rec.Code, rec.Body.String())
	}
	envelope := decodeAutoUpdateRunEnvelope(t, rec)
	if envelope.Error == nil || envelope.Error.Code != "auto_update_disabled" {
		t.Fatalf("error = %#v, want code auto_update_disabled", envelope.Error)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("disabled run must not create a task, runner calls = %#v", runner.calls)
	}
	if got := svc.Runtime().AutoUpdate; got.AppsEnabled || got.PackagesEnabled {
		t.Fatalf("disabled run must not change settings: %#v", got)
	}
}

// TestRunAutoUpdateNowAppliesTemporaryOverrides 验证 payload 覆盖只作用于本次
// 运行：交给 runner 的是叠加后的有效设置，且不写回 runtime_settings。
func TestRunAutoUpdateNowAppliesTemporaryOverrides(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	if _, err := svc.UpdateAutoUpdate(ctx, RuntimeAutoUpdateUpdate{
		PackagesEnabled: boolPtr(true),
		PackagesMode:    stringPtr(AutoUpdatePackagesModeSecurity),
		ScheduleTime:    stringPtr("04:00"),
	}); err != nil {
		t.Fatal(err)
	}
	runner := &fakeAutoUpdateRunner{taskID: "task-manual-1"}
	svc.SetAutoUpdateRunner(runner)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/settings/runtime/auto-update/run",
		strings.NewReader(`{"appsEnabled":true,"packagesMode":" ALL "}`))
	NewHandler(svc).RunAutoUpdateNow(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body = %s", rec.Code, rec.Body.String())
	}
	if envelope := decodeAutoUpdateRunEnvelope(t, rec); envelope.Error != nil || envelope.Data.TaskID != "task-manual-1" {
		t.Fatalf("run response = %#v, want taskId task-manual-1", envelope)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("runner calls = %#v, want exactly 1", runner.calls)
	}
	got := runner.calls[0]
	want := RuntimeAutoUpdateSettings{
		AppsEnabled:     true,
		PackagesEnabled: true,
		PackagesMode:    AutoUpdatePackagesModeAll,
		ScheduleTime:    "04:00",
	}
	if got != want {
		t.Fatalf("effective settings for manual run = %#v, want %#v", got, want)
	}
	// 临时覆盖不落库。
	persisted := svc.Runtime().AutoUpdate
	if persisted.AppsEnabled || persisted.PackagesMode != AutoUpdatePackagesModeSecurity {
		t.Fatalf("temporary overrides leaked into settings: %#v", persisted)
	}
	reloaded, err := NewService(svc.db, svc.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.Runtime().AutoUpdate; got.AppsEnabled || got.PackagesMode != AutoUpdatePackagesModeSecurity {
		t.Fatalf("temporary overrides were persisted: %#v", got)
	}
}

// TestRunAutoUpdateNowOverridesCanEnableDisabledRun 验证当前两开关都关时，payload
// 覆盖打开任一开关即可手动运行（覆盖是本次运行的有效设置，不受持久化值限制）。
func TestRunAutoUpdateNowOverridesCanEnableDisabledRun(t *testing.T) {
	svc := newTestService(t)
	runner := &fakeAutoUpdateRunner{taskID: "task-manual-2"}
	svc.SetAutoUpdateRunner(runner)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/settings/runtime/auto-update/run",
		strings.NewReader(`{"appsEnabled":true}`))
	NewHandler(svc).RunAutoUpdateNow(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body = %s", rec.Code, rec.Body.String())
	}
	if len(runner.calls) != 1 || !runner.calls[0].AppsEnabled || runner.calls[0].PackagesEnabled {
		t.Fatalf("runner calls = %#v, want apps enabled only", runner.calls)
	}
}

// TestRunAutoUpdateNowRejectsInvalidPackagesMode 验证覆盖的 packagesMode 走与
// 分区 PUT 相同的稳定错误码，且失败不创建任务。
func TestRunAutoUpdateNowRejectsInvalidPackagesMode(t *testing.T) {
	svc := newTestService(t)
	runner := &fakeAutoUpdateRunner{taskID: "task-manual"}
	svc.SetAutoUpdateRunner(runner)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/settings/runtime/auto-update/run",
		strings.NewReader(`{"packagesEnabled":true,"packagesMode":"partial"}`))
	NewHandler(svc).RunAutoUpdateNow(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body = %s", rec.Code, rec.Body.String())
	}
	envelope := decodeAutoUpdateRunEnvelope(t, rec)
	if envelope.Error == nil || envelope.Error.Code != "invalid_auto_update_packages_mode" {
		t.Fatalf("error = %#v, want code invalid_auto_update_packages_mode", envelope.Error)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("rejected override must not create a task, runner calls = %#v", runner.calls)
	}
}

// TestRunAutoUpdateNowWithoutRunnerIsUnavailable 验证尚未完成装配（runner 未注入）
// 时返回 503 而不是 panic 或静默成功。
func TestRunAutoUpdateNowWithoutRunnerIsUnavailable(t *testing.T) {
	svc := newTestService(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/settings/runtime/auto-update/run", nil)
	NewHandler(svc).RunAutoUpdateNow(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body = %s", rec.Code, rec.Body.String())
	}
	if envelope := decodeAutoUpdateRunEnvelope(t, rec); envelope.Error == nil || envelope.Error.Code != "auto_update_unavailable" {
		t.Fatalf("error = %#v, want code auto_update_unavailable", envelope.Error)
	}
}
