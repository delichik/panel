package applications

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"panel/internal/modules/tasks"
	"panel/internal/platform/database/orm"
	id "panel/internal/platform/identity"
)

const (
	defaultDeploymentQueueSize = 256
	defaultDeploymentLeaseTTL  = 3 * time.Minute
)

type PlanScope struct {
	ApplicationIDs      []string
	ServerIDs           []string
	StopServers         []string
	Purge               bool
	Force               bool
	Manual              bool
	TriggerType         string
	TriggerResourceType string
	TriggerResourceID   string
	Reason              string
}

type DeploymentDispatcher interface {
	Start(ctx context.Context) error
	WakePlan(scope PlanScope)
	EnqueueExecute(targetID string)
	EnqueueVerify(targetID string)
	EnqueueAggregate(operationID string)
	Recover(ctx context.Context) error
	Stop(ctx context.Context) error
}

type DeploymentDispatcherOption func(*deploymentDispatcher)

func WithDeploymentDispatcherOwner(owner string) DeploymentDispatcherOption {
	return func(d *deploymentDispatcher) {
		if strings.TrimSpace(owner) != "" {
			d.owner = strings.TrimSpace(owner)
		}
	}
}

func WithDeploymentDispatcherQueueSize(size int) DeploymentDispatcherOption {
	return func(d *deploymentDispatcher) {
		if size > 0 {
			d.planQueue = newPlanWorkQueue(size)
			d.executeQueue = newStringWorkQueue(size)
			d.verifyQueue = newStringWorkQueue(size)
			d.aggregateQueue = newStringWorkQueue(size)
		}
	}
}

func WithDeploymentDispatcherLeaseTTL(ttl time.Duration) DeploymentDispatcherOption {
	return func(d *deploymentDispatcher) {
		if ttl > 0 {
			d.leaseTTL = ttl
		}
	}
}

func NewDeploymentDispatcher(svc *Service, opts ...DeploymentDispatcherOption) DeploymentDispatcher {
	d := &deploymentDispatcher{
		service:        svc,
		owner:          "deployment-dispatcher-" + id.New("worker"),
		leaseTTL:       defaultDeploymentLeaseTTL,
		planQueue:      newPlanWorkQueue(defaultDeploymentQueueSize),
		executeQueue:   newStringWorkQueue(defaultDeploymentQueueSize),
		verifyQueue:    newStringWorkQueue(defaultDeploymentQueueSize),
		aggregateQueue: newStringWorkQueue(defaultDeploymentQueueSize),
	}
	for _, opt := range opts {
		opt(d)
	}
	return d
}

type deploymentDispatcher struct {
	service        *Service
	owner          string
	leaseTTL       time.Duration
	planQueue      *planWorkQueue
	executeQueue   *stringWorkQueue
	verifyQueue    *stringWorkQueue
	aggregateQueue *stringWorkQueue
	mu             sync.Mutex
	cancel         context.CancelFunc
	wg             sync.WaitGroup
}

func (d *deploymentDispatcher) Start(parent context.Context) error {
	if d == nil || d.service == nil {
		return nil
	}
	d.mu.Lock()
	if d.cancel != nil {
		d.mu.Unlock()
		return nil
	}
	ctx, cancel := context.WithCancel(parent)
	d.cancel = cancel
	d.mu.Unlock()
	if err := d.Recover(ctx); err != nil {
		cancel()
		d.mu.Lock()
		d.cancel = nil
		d.mu.Unlock()
		return err
	}
	d.mu.Lock()
	if d.cancel == nil {
		d.mu.Unlock()
		return nil
	}
	d.wg.Add(5)
	go d.planLoop(ctx)
	go d.executeLoop(ctx)
	go d.verifyLoop(ctx)
	go d.aggregateLoop(ctx)
	go d.repairLoop(ctx)
	d.mu.Unlock()
	return nil
}

func (d *deploymentDispatcher) WakePlan(scope PlanScope) {
	if d == nil || d.planQueue == nil {
		return
	}
	d.planQueue.enqueue(scope)
}

func (d *deploymentDispatcher) EnqueueExecute(targetID string) {
	if d == nil || d.executeQueue == nil {
		return
	}
	d.executeQueue.enqueue(targetID)
}

func (d *deploymentDispatcher) EnqueueVerify(targetID string) {
	if d == nil || d.verifyQueue == nil {
		return
	}
	d.verifyQueue.enqueue(targetID)
}

func (d *deploymentDispatcher) EnqueueAggregate(operationID string) {
	if d == nil || d.aggregateQueue == nil {
		return
	}
	d.aggregateQueue.enqueue(operationID)
}

func (d *deploymentDispatcher) Recover(ctx context.Context) error {
	if d == nil || d.service == nil || d.service.db == nil {
		return nil
	}
	now := formatTime(time.Now().UTC())
	if err := d.recoverExpiredLeases(ctx, now); err != nil {
		return err
	}
	if err := d.recoverPlannedTargets(ctx, now); err != nil {
		return err
	}
	if err := d.recoverRetryableTargets(ctx, now); err != nil {
		return err
	}
	if err := d.enqueueTargets(ctx, `state IN ('ready') AND (next_run_at='' OR next_run_at<=?)`, []any{now}, d.EnqueueExecute); err != nil {
		return err
	}
	if err := d.enqueueTargets(ctx, `state='verifying'`, nil, d.EnqueueVerify); err != nil {
		return err
	}
	if err := d.enqueueTerminalOperations(ctx); err != nil {
		return err
	}
	// 幽灵锚点清理属于维护性动作，失败不应阻塞恢复主流程。
	_ = d.reconcileStaleTargetAnchors(ctx)
	return nil
}

// reconcileStaleTargetAnchors 清理已经没有存活生命周期目标的目标任务锚点，
// 避免幽灵锚点永久占住服务器的部署并发键。
func (d *deploymentDispatcher) reconcileStaleTargetAnchors(ctx context.Context) error {
	if d == nil || d.service == nil {
		return nil
	}
	return d.service.FailStaleTargetTaskAnchors(ctx)
}

func (d *deploymentDispatcher) Stop(context.Context) error {
	if d == nil {
		return nil
	}
	d.mu.Lock()
	if d.cancel != nil {
		d.cancel()
		d.cancel = nil
	}
	d.mu.Unlock()
	if d.planQueue != nil {
		d.planQueue.close()
	}
	if d.executeQueue != nil {
		d.executeQueue.close()
	}
	if d.verifyQueue != nil {
		d.verifyQueue.close()
	}
	if d.aggregateQueue != nil {
		d.aggregateQueue.close()
	}
	d.wg.Wait()
	return nil
}

func (d *deploymentDispatcher) claimExecuteTarget(ctx context.Context, targetID string) (LifecycleTarget, bool, error) {
	if d == nil || d.service == nil || d.service.db == nil || d.service.tasks == nil {
		return LifecycleTarget{}, false, nil
	}
	target, err := d.service.lifecycleTargetByID(ctx, targetID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return LifecycleTarget{}, false, nil
		}
		return LifecycleTarget{}, false, err
	}
	if target.State != LifecycleTargetStateReady {
		return LifecycleTarget{}, false, nil
	}
	now := time.Now().UTC()
	if target.NextRunAt != nil && target.NextRunAt.After(now) {
		return LifecycleTarget{}, false, nil
	}
	leaseExpiresAt := now.Add(d.leaseTTL)
	res, err := orm.RawExec(ctx, d.service.lifecycleDB(), `UPDATE application_lifecycle_targets
		SET state=?,
			status=?,
			lease_owner=?,
			lease_expires_at=?,
			started_at=COALESCE(started_at, ?),
			updated_at=?
		WHERE id=?
		  AND state=?
		  AND (next_run_at='' OR next_run_at<=?)`,
		LifecycleTargetStateClaimed,
		lifecycleStatusForState(LifecycleTargetStateClaimed),
		d.owner,
		formatTime(leaseExpiresAt),
		formatTime(now),
		formatTime(now),
		target.ID,
		LifecycleTargetStateReady,
		formatTime(now))
	if err != nil {
		return LifecycleTarget{}, false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return LifecycleTarget{}, false, err
	}
	if affected == 0 {
		return LifecycleTarget{}, false, nil
	}
	task, err := d.createClaimTask(ctx, target)
	if err != nil {
		_ = d.markTargetTaskCreateFailed(ctx, target.ID, err)
		return LifecycleTarget{}, false, err
	}
	res, err = orm.RawExec(ctx, d.service.lifecycleDB(), `UPDATE application_lifecycle_targets
		SET lease_owner=?,
			claimed_task_id=?,
			updated_at=?
		WHERE id=?
		  AND state=?
		  AND lease_owner=?`,
		lifecycleTaskLeaseOwner(task.ID),
		task.ID,
		formatTime(time.Now().UTC()),
		target.ID,
		LifecycleTargetStateClaimed,
		d.owner)
	if err != nil {
		_ = d.service.tasks.Cancel(ctx, task.ID, "Deployment target task binding failed")
		_ = d.markTargetTaskCreateFailed(ctx, target.ID, err)
		return LifecycleTarget{}, false, err
	}
	affected, err = res.RowsAffected()
	if err != nil {
		_ = d.service.tasks.Cancel(ctx, task.ID, "Deployment target task binding result was unavailable")
		_ = d.markTargetTaskCreateFailed(ctx, target.ID, err)
		return LifecycleTarget{}, false, err
	}
	if affected == 0 {
		_ = d.service.tasks.Cancel(ctx, task.ID, "Deployment target was already rebound")
		return LifecycleTarget{}, false, nil
	}
	target.State = LifecycleTargetStateClaimed
	target.Status = lifecycleStatusForState(LifecycleTargetStateClaimed)
	target.LeaseOwner = lifecycleTaskLeaseOwner(task.ID)
	target.LeaseExpiresAt = &leaseExpiresAt
	target.ClaimedTaskID = task.ID
	return target, true, nil
}

func (d *deploymentDispatcher) planLoop(ctx context.Context) {
	defer d.wg.Done()
	for {
		scope, ok := d.planQueue.dequeue()
		if !ok {
			return
		}
		if ctx.Err() != nil {
			return
		}
		_ = d.processPlanScope(ctx, scope)
		if d.planQueue.takeDirty() {
			_ = d.processPlanScope(ctx, PlanScope{TriggerType: "deployment_dispatcher", Reason: "plan_queue_overflow"})
		}
	}
}

func (d *deploymentDispatcher) processPlanScope(ctx context.Context, scope PlanScope) error {
	if d == nil || d.service == nil {
		return nil
	}
	appIDs := uniqueStringItems(scope.ApplicationIDs)
	if len(appIDs) == 0 {
		apps, err := d.service.ListForReconcile(ctx)
		if err != nil {
			return err
		}
		for _, app := range apps {
			if app.Enabled || app.DeletionRequested {
				appIDs = append(appIDs, app.ID)
			}
		}
	}
	for _, appID := range appIDs {
		result, err := d.service.PlanApplicationDeployment(ctx, DeploymentPlanRequest{
			ApplicationID:       appID,
			ServerIDs:           scope.ServerIDs,
			StopServers:         scope.StopServers,
			Purge:               scope.Purge,
			Force:               scope.Force,
			Manual:              scope.Manual,
			TriggerType:         firstNonEmpty(scope.TriggerType, "deployment_dispatcher"),
			TriggerResourceType: scope.TriggerResourceType,
			TriggerResourceID:   scope.TriggerResourceID,
			Reason:              scope.Reason,
		})
		if err != nil {
			return err
		}
		d.enqueuePlanResult(result)
	}
	return nil
}

func (d *deploymentDispatcher) enqueuePlanResult(result DeploymentPlanResult) {
	if d == nil {
		return
	}
	for _, target := range result.CreatedTargets {
		d.EnqueueExecute(target.ID)
	}
	for _, target := range result.SupersededTargets {
		if strings.TrimSpace(target.OperationID) != "" {
			d.EnqueueAggregate(target.OperationID)
		}
	}
	for _, operationID := range result.OperationIDs {
		d.EnqueueAggregate(operationID)
	}
}

func (d *deploymentDispatcher) executeLoop(ctx context.Context) {
	defer d.wg.Done()
	for {
		targetID, ok := d.executeQueue.dequeue()
		if !ok {
			return
		}
		if ctx.Err() != nil {
			return
		}
		_ = d.processExecuteTarget(ctx, targetID)
		if d.executeQueue.takeDirty() {
			_ = d.Recover(ctx)
		}
	}
}

func (d *deploymentDispatcher) processExecuteTarget(ctx context.Context, targetID string) error {
	claimed, ok, err := d.claimExecuteTarget(ctx, targetID)
	if err != nil || !ok {
		return err
	}
	d.runClaimedTask(claimed.ClaimedTaskID)
	return nil
}

func (d *deploymentDispatcher) runClaimedTask(taskID string) {
	taskID = strings.TrimSpace(taskID)
	if d == nil || d.service == nil || d.service.tasks == nil || taskID == "" {
		return
	}
	task, err := d.service.tasks.Get(context.Background(), taskID)
	if err != nil {
		return
	}
	go func() {
		manager := tasks.NewManager(d.service.tasks)
		defer d.service.tasks.FinishExecution(task.ID)
		_ = manager.Run(context.Background(), task)
	}()
}

func (d *deploymentDispatcher) verifyLoop(ctx context.Context) {
	defer d.wg.Done()
	for {
		targetID, ok := d.verifyQueue.dequeue()
		if !ok {
			return
		}
		if ctx.Err() != nil {
			return
		}
		_ = d.processVerifyTarget(ctx, targetID)
		if d.verifyQueue.takeDirty() {
			_ = d.Recover(ctx)
		}
	}
}

func (d *deploymentDispatcher) aggregateLoop(ctx context.Context) {
	defer d.wg.Done()
	for {
		operationID, ok := d.aggregateQueue.dequeue()
		if !ok {
			return
		}
		if ctx.Err() != nil {
			return
		}
		_ = d.processAggregateOperation(ctx, operationID)
		if d.aggregateQueue.takeDirty() {
			_ = d.Recover(ctx)
		}
	}
}

func (d *deploymentDispatcher) repairLoop(ctx context.Context) {
	defer d.wg.Done()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if d.planQueue.takeDirty() {
				_ = d.processPlanScope(ctx, PlanScope{TriggerType: "deployment_dispatcher", Reason: "plan_queue_overflow"})
			}
			_ = d.reconcileStaleTargetAnchors(ctx)
			_ = d.Recover(ctx)
		}
	}
}

func (d *deploymentDispatcher) claimVerifyTarget(ctx context.Context, targetID string) (bool, error) {
	if d == nil || d.service == nil || d.service.db == nil {
		return false, nil
	}
	now := time.Now().UTC()
	res, err := orm.RawExec(ctx, d.service.lifecycleDB(), `UPDATE application_lifecycle_targets
		SET lease_owner=?,
			lease_expires_at=?,
			updated_at=?
		WHERE id=?
		  AND state=?
		  AND (lease_owner='' OR lease_owner=? OR lease_expires_at<=?)`,
		d.owner,
		formatTime(now.Add(time.Minute)),
		formatTime(now),
		targetID,
		LifecycleTargetStateVerifying,
		d.owner,
		formatTime(now))
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	return affected > 0, err
}

func (d *deploymentDispatcher) processVerifyTarget(ctx context.Context, targetID string) error {
	if d == nil || d.service == nil {
		return nil
	}
	ok, err := d.claimVerifyTarget(ctx, targetID)
	if err != nil || !ok {
		return err
	}
	target, err := d.service.lifecycleTargetByID(ctx, targetID)
	if err != nil {
		return err
	}
	err = d.service.verifyLifecycleTargetNow(ctx, targetID)
	d.EnqueueAggregate(target.OperationID)
	if err != nil {
		return err
	}
	return d.service.afterLifecycleTargetVerified(ctx, target)
}

func (d *deploymentDispatcher) processAggregateOperation(ctx context.Context, operationID string) error {
	if d == nil || d.service == nil {
		return nil
	}
	return d.service.finishDeploymentOperationFromTargets(ctx, operationID)
}

func (d *deploymentDispatcher) createClaimTask(ctx context.Context, target LifecycleTarget) (tasks.Task, error) {
	removeApplicationData := false
	if app, err := d.service.Get(ctx, target.ApplicationID); err == nil {
		removeApplicationData = app.DeletionRequested
	}
	params, err := json.Marshal(deployTaskParams{
		AppID:                 target.ApplicationID,
		ServerID:              target.ServerID,
		LifecycleOperationID:  target.OperationID,
		LifecycleTargetID:     target.ID,
		Generation:            target.DesiredGeneration,
		SpecHash:              target.DesiredSpecHash,
		Action:                firstNonEmpty(target.Action, LifecycleTargetActionApply),
		Purge:                 target.Action == LifecycleTargetActionPurge,
		RemoveApplicationData: target.Action == LifecycleTargetActionPurge && removeApplicationData,
	})
	if err != nil {
		return tasks.Task{}, err
	}
	metadata, err := json.Marshal(map[string]any{
		"applicationId":        target.ApplicationID,
		"serverId":             target.ServerID,
		"action":               firstNonEmpty(target.Action, LifecycleTargetActionApply),
		"generation":           target.DesiredGeneration,
		"specHash":             target.DesiredSpecHash,
		"lifecycleOperationId": target.OperationID,
		"lifecycleTargetId":    target.ID,
	})
	if err != nil {
		return tasks.Task{}, err
	}
	nextRunAt := time.Now().UTC().Add(d.leaseTTL)
	task, _, err := tasks.NewManager(d.service.tasks).Create(ctx, tasks.CreateInput{
		Type:         targetTaskTypeForAction(firstNonEmpty(target.Action, LifecycleTargetActionApply)),
		ServerID:     target.ServerID,
		ResourceType: "application",
		ResourceID:   target.ApplicationID,
		ParamsJSON:   string(params),
		MetadataJSON: string(metadata),
		Summary:      "Claiming application deployment target",
		Status:       tasks.StatusScheduled,
		NextRunAt:    &nextRunAt,
	}, tasks.Trigger{Type: "deployment_dispatcher"})
	return task, err
}

func (d *deploymentDispatcher) markTargetTaskCreateFailed(ctx context.Context, targetID string, cause error) error {
	if d == nil || d.service == nil || d.service.db == nil {
		return nil
	}
	message := "Application target task could not be created"
	if cause != nil && strings.TrimSpace(cause.Error()) != "" {
		message = cause.Error()
	}
	nowTime := time.Now().UTC()
	nextRunAt := formatTime(nowTime.Add(lifecycleExecutionRetryDelay(ctx, d.service.lifecycleDB(), targetID)))
	now := formatTime(nowTime)
	_, err := orm.RawExec(ctx, d.service.lifecycleDB(), `UPDATE application_lifecycle_targets
		SET state=?,
			status=?,
			error=?,
			error_code=?,
			error_message=?,
			error_detail=?,
			stage=?,
			attempt=attempt+1,
			next_run_at=?,
			lease_owner='',
			lease_expires_at='',
			updated_at=?
		WHERE id=?
		  AND state IN ('planned','ready','claimed','failed_retryable')
		  AND (state<>'claimed' OR lease_owner=?)`,
		LifecycleTargetStateFailedRetryable,
		lifecycleStatusForState(LifecycleTargetStateFailedRetryable),
		message,
		"task_create_failed",
		"Application target task could not be created",
		message,
		"task_create",
		nextRunAt,
		now,
		targetID,
		d.owner)
	return err
}

func (d *deploymentDispatcher) recoverPlannedTargets(ctx context.Context, now string) error {
	_, err := orm.RawExec(ctx, d.service.lifecycleDB(), `UPDATE application_lifecycle_targets
		SET state=?,
			status=?,
			updated_at=?
		WHERE state=?
		  AND (next_run_at='' OR next_run_at<=?)`,
		LifecycleTargetStateReady,
		lifecycleStatusForState(LifecycleTargetStateReady),
		now,
		LifecycleTargetStatePlanned,
		now)
	return err
}

func (d *deploymentDispatcher) recoverRetryableTargets(ctx context.Context, now string) error {
	_, err := orm.RawExec(ctx, d.service.lifecycleDB(), `UPDATE application_lifecycle_targets
		SET state=?,
			status=?,
			updated_at=?
		WHERE state=?
		  AND next_run_at<>''
		  AND next_run_at<=?`,
		LifecycleTargetStateReady,
		lifecycleStatusForState(LifecycleTargetStateReady),
		now,
		LifecycleTargetStateFailedRetryable,
		now)
	return err
}

func (d *deploymentDispatcher) recoverExpiredLeases(ctx context.Context, now string) error {
	nowTime := time.Now().UTC()
	nextRunAt := formatTime(nowTime.Add(withLifecycleRetryJitter(10 * time.Second)))
	_, err := orm.RawExec(ctx, d.service.lifecycleDB(), `UPDATE application_lifecycle_targets
		SET state=CASE
				WHEN stage='' OR stage='waiting_server_queue' THEN 'ready'
				ELSE 'failed_retryable'
			END,
			status=CASE
				WHEN stage='' OR stage='waiting_server_queue' THEN 'pending'
				ELSE 'failed'
			END,
			lease_owner='',
			lease_expires_at='',
			error_code=CASE WHEN stage='' OR stage='waiting_server_queue' THEN error_code ELSE 'lease_lost' END,
			error_message=CASE WHEN stage='' OR stage='waiting_server_queue' THEN error_message ELSE 'Deployment worker lease expired' END,
			error_detail=CASE WHEN stage='' OR stage='waiting_server_queue' THEN error_detail ELSE 'Panel recovered an expired deployment target lease during startup or repair scan' END,
			attempt=CASE WHEN stage='' OR stage='waiting_server_queue' THEN attempt ELSE attempt+1 END,
			next_run_at=CASE WHEN stage='' OR stage='waiting_server_queue' THEN next_run_at ELSE ? END,
			updated_at=?
		WHERE state IN ('claimed','preparing','applying','stopping','purging')
		  AND lease_expires_at<>'' AND lease_expires_at<=?`,
		nextRunAt, now, now)
	return err
}

func (d *deploymentDispatcher) enqueueTargets(ctx context.Context, predicate string, args []any, enqueue func(string)) error {
	var targetIDs []string
	if err := orm.New(d.service.lifecycleDB()).From("application_lifecycle_targets").Where(predicate, args...).Pluck(ctx, "id", &targetIDs); err != nil {
		return err
	}
	for _, targetID := range targetIDs {
		enqueue(targetID)
	}
	return nil
}

func (d *deploymentDispatcher) enqueueTerminalOperations(ctx context.Context) error {
	var operationIDs []string
	if err := orm.New(d.service.lifecycleDB()).From("application_lifecycle_targets").Where("state IN (?,?,?,?)", "succeeded", "failed", "superseded", "cancelled").Distinct().Pluck(ctx, "operation_id", &operationIDs); err != nil {
		return err
	}
	for _, operationID := range operationIDs {
		d.EnqueueAggregate(operationID)
	}
	return nil
}

type stringWorkQueue struct {
	ch     chan string
	mu     sync.Mutex
	closed bool
	dirty  bool
	seen   map[string]struct{}
}

func newStringWorkQueue(size int) *stringWorkQueue {
	return &stringWorkQueue{ch: make(chan string, size), seen: map[string]struct{}{}}
}

func (q *stringWorkQueue) enqueue(id string) {
	id = strings.TrimSpace(id)
	if id == "" || q == nil {
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return
	}
	if _, exists := q.seen[id]; exists {
		return
	}
	select {
	case q.ch <- id:
		q.seen[id] = struct{}{}
	default:
		q.dirty = true
	}
}

func (q *stringWorkQueue) dequeue() (string, bool) {
	if q == nil {
		return "", false
	}
	id, ok := <-q.ch
	if !ok {
		return "", false
	}
	q.mu.Lock()
	delete(q.seen, id)
	q.mu.Unlock()
	return id, true
}

func (q *stringWorkQueue) close() {
	if q == nil {
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return
	}
	q.closed = true
	close(q.ch)
}

func (q *stringWorkQueue) takeDirty() bool {
	if q == nil {
		return false
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	dirty := q.dirty
	q.dirty = false
	return dirty
}

type planWorkQueue struct {
	ch     chan PlanScope
	mu     sync.Mutex
	closed bool
	dirty  bool
}

func newPlanWorkQueue(size int) *planWorkQueue {
	return &planWorkQueue{ch: make(chan PlanScope, size)}
}

func (q *planWorkQueue) enqueue(scope PlanScope) {
	if q == nil {
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return
	}
	select {
	case q.ch <- scope:
	default:
		q.dirty = true
	}
}

func (q *planWorkQueue) dequeue() (PlanScope, bool) {
	if q == nil {
		return PlanScope{}, false
	}
	scope, ok := <-q.ch
	if !ok {
		return PlanScope{}, false
	}
	return scope, true
}

func (q *planWorkQueue) close() {
	if q == nil {
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return
	}
	q.closed = true
	close(q.ch)
}

func (q *planWorkQueue) takeDirty() bool {
	if q == nil {
		return false
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	dirty := q.dirty
	q.dirty = false
	return dirty
}
