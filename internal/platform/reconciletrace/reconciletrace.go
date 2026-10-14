// Package reconciletrace 提供协调（reconcile）全链路的可开关追踪日志。
//
// 追踪覆盖从触发源（scheduler / agent_report / 用户操作）、漂移判定、plan
// 决策、dispatcher 执行、部署各阶段到最终结果的完整链路。默认关闭，由
// settings 模块的运行时设置 `reconcile.trace` 控制：启动加载与设置更新时
// 通过 SetEnabled 同步。开启后每个关键事件输出一条结构化日志（统一携带
// trace="reconcile" 与 event 字段），用于定位协调器反复执行、部署反复失败
// 等问题；关闭时 Trace 零开销返回，不产生任何日志。
package reconciletrace

import (
	"sync/atomic"

	"panel/internal/platform/logging"

	"go.uber.org/zap"
)

// enabled 是协调追踪开关（默认关闭）。
var enabled atomic.Bool

// SetEnabled 设置协调追踪开关。
func SetEnabled(value bool) {
	enabled.Store(value)
}

// Enabled 返回协调追踪开关状态。
func Enabled() bool {
	return enabled.Load()
}

// Trace 在开关开启时输出一条协调追踪日志；关闭时零开销返回。
// event 是事件名（如 drift_detected、plan_result、target_claimed、
// deploy_step_failed），fields 是事件字段（统一带上 app_id/server_id/
// target_id/operation_id 等用于串联一次协调）。
func Trace(event string, fields ...zap.Field) {
	if !enabled.Load() {
		return
	}
	all := make([]zap.Field, 0, len(fields)+2)
	all = append(all, zap.String("trace", "reconcile"), zap.String("event", event))
	all = append(all, fields...)
	logging.L().Info("reconcile trace", all...)
}
