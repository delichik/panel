package tasks

import (
	"context"
	"strings"
	"time"

	panelerr "panel/internal/platform/errors"
)

// DefaultMaxTaskRetries is the safety limit used when a retryable task
// definition does not provide an explicit limit. It counts retries after the
// initial execution, so a task is attempted at most four times in total.
const DefaultMaxTaskRetries = 3

const (
	ConcurrencyParallelAllowed   = "parallel_allowed"
	ConcurrencyResourceExclusive = "resource_exclusive"
	ConcurrencyResourceQueue     = "resource_queue"
	ConcurrencyGlobalExclusive   = "global_exclusive"
	ConcurrencyCustomKey         = "custom_key"
)

const (
	ExecutionModeSingle   = "single"
	ExecutionModeSerial   = "serial"
	ExecutionModeParallel = "parallel"
)

type Trigger struct {
	Type     string
	Manual   bool
	Periodic bool
}

type PeriodicTrigger struct {
	Type                string
	Manual              bool
	TriggerResourceType string
	TriggerResourceID   string
	Payload             any
}

type TaskContext struct {
	Context context.Context
	Task    Task
	Service *Service
}

type Definition struct {
	Type    string
	Summary string
	Hidden  bool
	// Quiet 标记内部例行任务（例如周期 Agent 健康检查）：它的创建与成功流转
	// 属于后台巡检事实，在活动日志中记为 debug 级，默认级别筛选不会展示；
	// 失败/可重试/blocked 仍记为 error，保证故障不会被降噪掩盖。
	// Quiet 只影响活动日志级别，不改变执行、周期、去重或保留语义；
	// 是否在任务中心可见由 Hidden 单独决定。
	Quiet             bool
	AllowRunNow       bool
	AllowRetry        bool
	DisallowCancel    bool
	DefaultMaxRetries int
	StaleQueuedAfter  time.Duration
	ConcurrencyPolicy string
	ConcurrencyKey    func(CreateInput) string
	Execute           func(TaskContext) error
	OnFailure         func(context.Context, Task, error) error
	Periodic          *Periodic
}

type Periodic struct {
	Interval      time.Duration
	CollectInputs func(context.Context, PeriodicTrigger) (CreateBatchInput, bool, error)
}

type Registry struct {
	defs map[string]Definition
}

func NewRegistry() *Registry {
	return &Registry{defs: map[string]Definition{}}
}

func (r *Registry) Register(def Definition) error {
	def.Type = strings.TrimSpace(def.Type)
	if def.Type == "" {
		return panelerr.Validation("task_type_required", "Task type is required")
	}
	if def.ConcurrencyPolicy == "" {
		def.ConcurrencyPolicy = ConcurrencyResourceExclusive
	}
	if def.AllowRetry && def.Execute != nil && def.DefaultMaxRetries <= 0 {
		def.DefaultMaxRetries = DefaultMaxTaskRetries
	}
	if r.defs == nil {
		r.defs = map[string]Definition{}
	}
	if _, exists := r.defs[def.Type]; exists {
		return panelerr.Conflict("task_type_registered", "Task type is already registered")
	}
	r.defs[def.Type] = def
	return nil
}

func (r *Registry) MustRegister(def Definition) {
	if err := r.Register(def); err != nil {
		panic(err)
	}
}

func (r *Registry) Replace(def Definition) {
	def.Type = strings.TrimSpace(def.Type)
	if def.Type == "" {
		return
	}
	if def.ConcurrencyPolicy == "" {
		def.ConcurrencyPolicy = ConcurrencyResourceExclusive
	}
	if def.AllowRetry && def.Execute != nil && def.DefaultMaxRetries <= 0 {
		def.DefaultMaxRetries = DefaultMaxTaskRetries
	}
	if r.defs == nil {
		r.defs = map[string]Definition{}
	}
	r.defs[def.Type] = def
}

func (r *Registry) Definition(taskType string) (Definition, bool) {
	if r == nil {
		return Definition{}, false
	}
	def, ok := r.defs[strings.TrimSpace(taskType)]
	return def, ok
}

func (r *Registry) Types() []string {
	out := make([]string, 0, len(r.defs))
	for taskType := range r.defs {
		out = append(out, taskType)
	}
	return out
}

func ConcurrencyKeyFor(def Definition, in CreateInput) string {
	if strings.TrimSpace(in.ConcurrencyKey) != "" {
		return strings.TrimSpace(in.ConcurrencyKey)
	}
	switch def.ConcurrencyPolicy {
	case ConcurrencyParallelAllowed:
		return ""
	case ConcurrencyGlobalExclusive:
		return "type:" + in.Type
	case ConcurrencyCustomKey:
		if def.ConcurrencyKey != nil {
			return strings.TrimSpace(def.ConcurrencyKey(in))
		}
		return ""
	case ConcurrencyResourceQueue, ConcurrencyResourceExclusive, "":
		if def.ConcurrencyKey != nil {
			if key := strings.TrimSpace(def.ConcurrencyKey(in)); key != "" {
				return key
			}
		}
		resourceType := firstNonEmpty(in.ResourceType, "task")
		resourceID := firstNonEmpty(in.ResourceID, in.ServerID, in.NodeID)
		if resourceID == "" {
			return ""
		}
		return "type:" + in.Type + "|resource:" + resourceType + ":" + resourceID
	default:
		return ""
	}
}

func ErrExecutorUnavailable() error {
	return panelerr.Validation("task_run_now_unsupported", "This task type cannot be run from the task center")
}
