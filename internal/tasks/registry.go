package tasks

import (
	"context"
	"strings"
	"time"

	"panel/internal/panelerr"
)

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

type TaskContext struct {
	Context context.Context
	Task    Task
	Service *Service
}

func (tc TaskContext) Advance(stage, message string) error {
	return tc.Service.Advance(tc.Context, tc.Task.ID, stage, message)
}

func (tc TaskContext) Log(stream, line string) error {
	return tc.Service.AppendLog(tc.Context, tc.Task.ID, stream, line)
}

func (tc TaskContext) Step(in StepInput) (Step, error) {
	return tc.Service.UpsertStep(tc.Context, tc.Task.ID, in)
}

type Definition struct {
	Type              string
	Summary           string
	Hidden            bool
	AllowRunNow       bool
	AllowRetry        bool
	DefaultMaxRetries int
	ConcurrencyPolicy string
	ConcurrencyKey    func(CreateInput) string
	Validate          func(context.Context, CreateInput) error
	BeforeStart       func(context.Context, CreateInput, Trigger) (bool, error)
	Execute           func(TaskContext) error
	OnComplete        func(context.Context, Task) error
	OnFailure         func(context.Context, Task, error) error
	Periodic          *Periodic
}

type Periodic struct {
	Interval      time.Duration
	CollectInputs func(context.Context) (CreateBatchInput, bool, error)
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
