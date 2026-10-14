package orchestrator

import (
	"panel/internal/platform/reconciletrace"

	"go.uber.org/zap"
)

// traceJobEvent 输出一条按 job/execution/intent 串联的协调追踪日志。追踪
// 默认关闭（reconciletrace.SetEnabled(false) 时零开销返回），仅在运行时
// 设置 reconcile.trace 开启后才输出，用于定位 claim、RuntimeReconcile、
// 观测写回、Job 终态与租约恢复等控制面事件。
func traceJobEvent(event string, job Job, extra ...zap.Field) {
	fields := []zap.Field{
		zap.String("job_id", job.ID),
		zap.String("application_id", job.ApplicationID),
		zap.String("server_id", job.ServerID),
		zap.String("instance_id", job.InstanceID),
		zap.String("action", job.Action),
		zap.String("state", job.State),
		zap.String("intent_id", job.IntentID),
		zap.String("execution_id", job.ExecutionID),
	}
	fields = append(fields, extra...)
	reconciletrace.Trace(event, fields...)
}
