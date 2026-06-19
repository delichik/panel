package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	panelerr "panel/internal/platform/errors"
	id "panel/internal/platform/identity"
)

type Manager struct {
	service *Service
}

func NewManager(service *Service) *Manager {
	return &Manager{service: service}
}

func (m *Manager) Create(ctx context.Context, in CreateInput, trigger Trigger) (Task, bool, error) {
	if m == nil || m.service == nil {
		return Task{}, false, panelerr.Validation("task_service_unavailable", "Task service is unavailable")
	}
	def, ok := m.service.Registry().Definition(in.Type)
	if !ok {
		return Task{}, false, panelerr.Validation("task_type_unregistered", "Task type is not registered")
	}
	if def.BeforeStart != nil {
		shouldRun, err := def.BeforeStart(ctx, in, trigger)
		if err != nil || !shouldRun {
			return Task{}, false, err
		}
	}
	in.ConcurrencyKey = ConcurrencyKeyFor(def, in)
	if in.ConcurrencyKey != "" && def.ConcurrencyPolicy != ConcurrencyParallelAllowed {
		if existing, ok, err := m.service.ExistingActiveByConcurrencyKey(ctx, in.ConcurrencyKey); err != nil {
			return Task{}, false, err
		} else if ok {
			return existing, false, nil
		}
	}
	task, err := m.service.Create(ctx, in)
	return task, true, err
}

func (m *Manager) CreateBatch(ctx context.Context, batch CreateBatchInput, trigger Trigger) (Task, []Task, bool, error) {
	if m == nil || m.service == nil {
		return Task{}, nil, false, panelerr.Validation("task_service_unavailable", "Task service is unavailable")
	}
	inputs := normalizeBatchInputs(batch)
	if len(inputs) == 0 {
		return Task{}, nil, false, nil
	}
	if len(inputs) == 1 {
		task, created, err := m.Create(ctx, inputs[0], trigger)
		return task, nil, created, err
	}
	def, ok := m.service.Registry().Definition(inputs[0].Type)
	if !ok {
		return Task{}, nil, false, panelerr.Validation("task_type_unregistered", "Task type is not registered")
	}
	for _, in := range inputs[1:] {
		if in.Type != inputs[0].Type {
			return Task{}, nil, false, panelerr.Validation("task_batch_type_mismatch", "All batch task inputs must use the same task type")
		}
	}
	inputs, existing, err := m.filterBatchInputs(ctx, def, inputs, trigger)
	if err != nil {
		return Task{}, nil, false, err
	}
	if len(inputs) == 0 {
		return existing, nil, false, nil
	}
	if len(inputs) == 1 {
		task, created, err := m.createAfterBeforeStart(ctx, def, inputs[0])
		return task, nil, created, err
	}
	operationID := firstNonEmpty(batch.OperationID, inputs[0].OperationID, id.New("op"))
	executionMode := batch.ExecutionMode
	if executionMode == "" {
		executionMode = ExecutionModeParallel
	}
	parentInput := CreateInput{
		OperationID:    operationID,
		Type:           inputs[0].Type,
		ExecutionMode:  executionMode,
		ResourceType:   "task_batch",
		ResourceID:     operationID,
		TriggerType:    firstNonEmpty(batch.TriggerType, inputs[0].TriggerType),
		TriggeredBy:    firstNonEmpty(batch.TriggeredBy, inputs[0].TriggeredBy),
		Summary:        firstNonEmpty(batch.Summary, def.Summary, "Running "+inputs[0].Type+" batch"),
		ParamsJSON:     batchParamsJSON(inputs),
		ChildCount:     len(inputs),
		ConcurrencyKey: "batch:" + operationID,
		MetadataJSON:   "{}",
	}
	if parentInput.TriggerType == "" {
		parentInput.TriggerType = trigger.Type
	}
	if parentInput.TriggerType == "" && trigger.Periodic {
		parentInput.TriggerType = "scheduler"
	}
	if existing, ok, err := m.service.ExistingActiveByConcurrencyKey(ctx, parentInput.ConcurrencyKey); err != nil {
		return Task{}, nil, false, err
	} else if ok {
		return existing, nil, false, nil
	}
	tx, err := m.service.db.BeginTx(ctx, nil)
	if err != nil {
		return Task{}, nil, false, err
	}
	defer tx.Rollback()
	parent, err := m.service.createTx(ctx, tx, parentInput, false)
	if err != nil {
		return Task{}, nil, false, err
	}
	children := make([]Task, 0, len(inputs))
	for idx, childInput := range inputs {
		childInput.Type = parent.Type
		childInput.OperationID = operationID
		childInput.ParentTaskID = parent.ID
		childInput.ChildIndex = idx + 1
		childInput.ChildCount = len(inputs)
		childInput.ExecutionMode = ExecutionModeSingle
		if childInput.TriggerType == "" {
			childInput.TriggerType = parent.TriggerType
		}
		if childInput.TriggeredBy == "" {
			childInput.TriggeredBy = parent.TriggeredBy
		}
		childInput.ConcurrencyKey = ConcurrencyKeyFor(def, childInput)
		child, err := m.service.createTx(ctx, tx, childInput, true)
		if err != nil {
			return Task{}, nil, false, err
		}
		children = append(children, child)
	}
	if err := tx.Commit(); err != nil {
		return Task{}, nil, false, err
	}
	return parent, children, true, nil
}

func (m *Manager) createAfterBeforeStart(ctx context.Context, def Definition, in CreateInput) (Task, bool, error) {
	in.ConcurrencyKey = ConcurrencyKeyFor(def, in)
	if in.ConcurrencyKey != "" && def.ConcurrencyPolicy != ConcurrencyParallelAllowed {
		if existing, ok, err := m.service.ExistingActiveByConcurrencyKey(ctx, in.ConcurrencyKey); err != nil {
			return Task{}, false, err
		} else if ok {
			return existing, false, nil
		}
	}
	task, err := m.service.Create(ctx, in)
	return task, true, err
}

func (m *Manager) filterBatchInputs(ctx context.Context, def Definition, inputs []CreateInput, trigger Trigger) ([]CreateInput, Task, error) {
	out := make([]CreateInput, 0, len(inputs))
	var firstExisting Task
	for _, in := range inputs {
		if def.BeforeStart != nil {
			shouldRun, err := def.BeforeStart(ctx, in, trigger)
			if err != nil {
				return nil, Task{}, err
			}
			if !shouldRun {
				continue
			}
		}
		in.ConcurrencyKey = ConcurrencyKeyFor(def, in)
		if in.ConcurrencyKey != "" && def.ConcurrencyPolicy != ConcurrencyParallelAllowed {
			existing, ok, err := m.service.ExistingActiveByConcurrencyKey(ctx, in.ConcurrencyKey)
			if err != nil {
				return nil, Task{}, err
			}
			if ok {
				if firstExisting.ID == "" {
					firstExisting = existing
				}
				continue
			}
		}
		out = append(out, in)
	}
	return out, firstExisting, nil
}

func (m *Manager) Run(ctx context.Context, task Task) error {
	if m == nil || m.service == nil {
		return panelerr.Validation("task_service_unavailable", "Task service is unavailable")
	}
	def, ok := m.service.Registry().Definition(task.Type)
	if !ok {
		return panelerr.Validation("task_type_unregistered", "Task type is not registered")
	}
	if def.Execute == nil {
		return panelerr.Validation("task_executor_unregistered", "Task executor is not registered")
	}
	if m.service.HasRunningExecution(task.ID) {
		return nil
	}
	claimed, err := m.service.claimExecution(ctx, task.ID)
	if err != nil {
		return err
	}
	if !claimed {
		return nil
	}
	if task.ChildCount > 0 && task.ParentTaskID == "" {
		return m.RunParent(ctx, task)
	}
	runCtx := m.service.ExecutionContext(task.ID)
	if ctx != nil {
		if deadline, ok := ctx.Deadline(); ok {
			var cancel context.CancelFunc
			runCtx, cancel = context.WithDeadline(runCtx, deadline)
			defer cancel()
		}
	}
	err = def.Execute(TaskContext{Context: runCtx, Task: task, Service: m.service})
	if err != nil {
		latest, latestErr := m.service.Get(ctx, task.ID)
		if latestErr == nil && isTerminalStatus(latest.Status) {
			return nil
		}
		if def.OnFailure != nil {
			_ = def.OnFailure(ctx, task, err)
		}
		if task.MaxRetries > 0 {
			return m.service.FailRetryable(ctx, task.ID, err)
		}
		return m.service.Fail(ctx, task.ID, err)
	}
	if def.OnComplete != nil {
		if hookErr := def.OnComplete(ctx, task); hookErr != nil {
			return m.service.Fail(ctx, task.ID, hookErr)
		}
	}
	latest, latestErr := m.service.Get(ctx, task.ID)
	if latestErr == nil && isTerminalStatus(latest.Status) {
		return nil
	}
	summary := task.Summary
	if def.Summary != "" {
		summary = def.Summary
	}
	return m.service.Complete(ctx, task.ID, summary)
}

func (m *Manager) CreateAndRun(ctx context.Context, in CreateInput, trigger Trigger) (Task, bool, error) {
	task, created, err := m.Create(ctx, in, trigger)
	if err != nil || !created {
		return task, created, err
	}
	go func() {
		defer m.service.FinishExecution(task.ID)
		_ = m.Run(context.Background(), task)
	}()
	return task, true, nil
}

func (m *Manager) CreateBatchAndRun(ctx context.Context, batch CreateBatchInput, trigger Trigger) (Task, bool, error) {
	parent, _, created, err := m.CreateBatch(ctx, batch, trigger)
	if err != nil || !created {
		return parent, created, err
	}
	go func() {
		defer m.service.FinishExecution(parent.ID)
		_ = m.Run(context.Background(), parent)
	}()
	return parent, true, nil
}

func (m *Manager) RunParent(ctx context.Context, parent Task) error {
	children, err := m.service.Children(ctx, parent.ID)
	if err != nil {
		return err
	}
	if len(children) == 0 {
		return m.service.Complete(ctx, parent.ID, parent.Summary)
	}
	if err := m.service.Start(ctx, parent.ID); err != nil {
		return err
	}
	err = m.RunChildren(ctx, parent, children)
	if err != nil {
		return m.service.Fail(ctx, parent.ID, err)
	}
	return m.service.Complete(ctx, parent.ID, parent.Summary)
}

func (m *Manager) RunChildren(ctx context.Context, parent Task, children []Task) error {
	if parent.ExecutionMode == ExecutionModeParallel {
		return m.runChildrenParallel(ctx, children)
	}
	var joined error
	for _, child := range children {
		if err := m.Run(ctx, child); err != nil {
			joined = errors.Join(joined, err)
		}
		m.service.FinishExecution(child.ID)
	}
	return joined
}

func normalizeBatchInputs(batch CreateBatchInput) []CreateInput {
	out := make([]CreateInput, 0, len(batch.Inputs))
	for _, in := range batch.Inputs {
		if in.Type == "" {
			in.Type = batch.Type
		}
		if in.OperationID == "" {
			in.OperationID = batch.OperationID
		}
		if in.TriggerType == "" {
			in.TriggerType = batch.TriggerType
		}
		if in.TriggeredBy == "" {
			in.TriggeredBy = batch.TriggeredBy
		}
		if in.Summary == "" {
			in.Summary = batch.Summary
		}
		out = append(out, in)
	}
	return out
}

func batchParamsJSON(inputs []CreateInput) string {
	type child struct {
		ServerID     string `json:"serverId,omitempty"`
		ResourceType string `json:"resourceType,omitempty"`
		ResourceID   string `json:"resourceId,omitempty"`
		ParamsJSON   string `json:"paramsJson,omitempty"`
		Summary      string `json:"summary,omitempty"`
	}
	children := make([]child, 0, len(inputs))
	for _, in := range inputs {
		children = append(children, child{ServerID: in.ServerID, ResourceType: in.ResourceType, ResourceID: in.ResourceID, ParamsJSON: in.ParamsJSON, Summary: in.Summary})
	}
	data, err := json.Marshal(map[string]any{"children": children})
	if err != nil {
		return "{}"
	}
	return string(data)
}

func (m *Manager) runChildrenParallel(ctx context.Context, children []Task) error {
	var wg sync.WaitGroup
	errs := make(chan error, len(children))
	for _, child := range children {
		child := child
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer m.service.FinishExecution(child.ID)
			if err := m.Run(ctx, child); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	var joined error
	for err := range errs {
		joined = errors.Join(joined, err)
	}
	return joined
}
