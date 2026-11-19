package autoupdate

import (
	"encoding/json"
	"strings"
	"time"

	"panel/internal/modules/settings"
)

const (
	// TaskTypeAutoUpdateRun 是设置驱动的每日自动更新协调任务（TASK-PER-003）。
	// 它全局互斥、不可取消、允许重试，串行执行刷新/应用/软件包三个阶段。
	TaskTypeAutoUpdateRun = "auto_update_run"

	// triggerTypeScheduler 是调度触发的任务 TriggerType 与 ParamsJSON.trigger 值。
	triggerTypeScheduler = "scheduler"
	// triggerTypeUser 是手动「立即运行一次」写入任务行的 TriggerType（审计可见）。
	triggerTypeUser = "user"
	// triggerTypeManual 是手动作业 ParamsJSON.trigger 的值：执行阶段据此判定
	// 「使用创建时持久化的有效设置、跳过计划窗口与当日去重」。
	triggerTypeManual = "manual"

	// 只读运行态取值，持久化到 autoUpdate.lastRunStatus。
	RunStatusRunning   = "running"
	RunStatusCompleted = "completed"
	RunStatusFailed    = "failed"
	RunStatusDisabled  = "disabled"

	// DefaultTickInterval 是设置驱动调度器的 tick 间隔。不能用
	// tasks.StartDailyAtLoop：它的 hour/minute 在构造时冻结，无法热更新用户
	// 可编辑的执行时间；30 秒 tick 读取热设置即可，重启后也最多延迟一个 tick。
	DefaultTickInterval = 30 * time.Second
	// DefaultScheduleWindow 是计划时间后的触发容忍窗口。错过整个窗口（例如
	// Panel 当天未运行）不补跑，等次日计划时间；窗口内重启仍会补触发一次。
	DefaultScheduleWindow = 2 * time.Minute

	defaultRefreshTimeout      = 10 * time.Minute
	defaultAppsTimeout         = 15 * time.Minute
	defaultReverseProxyTimeout = 30 * time.Minute
	defaultPackagesTimeout     = 60 * time.Minute
	defaultWaitPollInterval    = time.Second
)

// autoUpdateParams 持久化到任务 ParamsJSON，便于重启恢复与审计。
type autoUpdateParams struct {
	AppsEnabled     bool   `json:"appsEnabled"`
	PackagesEnabled bool   `json:"packagesEnabled"`
	PackagesMode    string `json:"packagesMode"`
	ScheduleTime    string `json:"scheduleTime"`
	Trigger         string `json:"trigger"`
}

func paramsFor(rt settings.RuntimeAutoUpdateSettings, trigger string) string {
	raw, err := json.Marshal(autoUpdateParams{
		AppsEnabled:     rt.AppsEnabled,
		PackagesEnabled: rt.PackagesEnabled,
		PackagesMode:    rt.PackagesMode,
		ScheduleTime:    rt.ScheduleTime,
		Trigger:         trigger,
	})
	if err != nil {
		return "{}"
	}
	return string(raw)
}

// parseParams 解析任务 ParamsJSON；空值或非法 JSON 视为无法识别（返回 false），
// 调用方按调度触发的既有语义回退到执行时读取设置。
func parseParams(raw string) (autoUpdateParams, bool) {
	var params autoUpdateParams
	if strings.TrimSpace(raw) == "" {
		return params, false
	}
	if err := json.Unmarshal([]byte(raw), &params); err != nil {
		return params, false
	}
	return params, true
}
