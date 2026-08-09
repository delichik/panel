package applications

import (
	"context"
	"database/sql"
	"strings"
	"time"

	appruntime "panel/internal/modules/applications/runtime"
	"panel/internal/platform/database/orm"
	panelerr "panel/internal/platform/errors"
)

// 协调记录：记录页列表/详情直接从协调库聚合生命周期操作与目标，不落投影表。

type OperationRecordFilter struct {
	ApplicationID string
	Status        string
	Source        string
	From          *time.Time
	To            *time.Time
	Limit         int
	Offset        int
}

type OperationRecordSummary struct {
	OperationID     string     `json:"operationId"`
	ApplicationID   string     `json:"applicationId"`
	ApplicationName string     `json:"applicationName"`
	Action          string     `json:"action"`
	Source          string     `json:"source"`
	TriggeredBy     string     `json:"triggeredBy,omitempty"`
	Status          string     `json:"status"`
	StartedAt       *time.Time `json:"startedAt,omitempty"`
	FinishedAt      *time.Time `json:"finishedAt,omitempty"`
	TargetTotal     int        `json:"targetTotal"`
	TargetSucceeded int        `json:"targetSucceeded"`
	TargetFailed    int        `json:"targetFailed"`
	TargetServers   []string   `json:"targetServers,omitempty"`
	LatestAt        time.Time  `json:"latestAt"`
	FailureSummary  string     `json:"failureSummary,omitempty"`
	CreatedAt       time.Time  `json:"createdAt"`
	UpdatedAt       time.Time  `json:"updatedAt"`
}

type OperationRecordStage struct {
	ID         string     `json:"id"`
	Stage      string     `json:"stage"`
	Status     string     `json:"status"`
	Detail     string     `json:"detail,omitempty"`
	StartedAt  *time.Time `json:"startedAt,omitempty"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
}

type OperationRecordTarget struct {
	ID                 string                 `json:"id"`
	OperationID        string                 `json:"operationId,omitempty"`
	ApplicationID      string                 `json:"applicationId,omitempty"`
	ServerID           string                 `json:"serverId,omitempty"`
	ServerName         string                 `json:"serverName,omitempty"`
	Action             string                 `json:"action,omitempty"`
	State              string                 `json:"state,omitempty"`
	Status             string                 `json:"status"`
	Stage              string                 `json:"stage,omitempty"`
	Attempt            int                    `json:"attempt,omitempty"`
	NextRunAt          *time.Time             `json:"nextRunAt,omitempty"`
	ClaimedTaskID      string                 `json:"claimedTaskId,omitempty"`
	ContainerName      string                 `json:"containerName,omitempty"`
	DesiredState       string                 `json:"desiredState,omitempty"`
	DesiredGeneration  int                    `json:"desiredGeneration,omitempty"`
	DesiredSpecHash    string                 `json:"desiredSpecHash,omitempty"`
	ObservedState      string                 `json:"observedState,omitempty"`
	ObservedExitCode   string                 `json:"observedExitCode,omitempty"`
	ObservedError      string                 `json:"observedError,omitempty"`
	ObservedGeneration int                    `json:"observedGeneration,omitempty"`
	ObservedSpecHash   string                 `json:"observedSpecHash,omitempty"`
	ObservedImage      string                 `json:"observedImage,omitempty"`
	ObservedAt         *time.Time             `json:"observedAt,omitempty"`
	ErrorCode          string                 `json:"errorCode,omitempty"`
	ErrorMessage       string                 `json:"errorMessage,omitempty"`
	ErrorDetail        string                 `json:"errorDetail,omitempty"`
	CreatedAt          time.Time              `json:"createdAt"`
	StartedAt          *time.Time             `json:"startedAt,omitempty"`
	FinishedAt         *time.Time             `json:"finishedAt,omitempty"`
	UpdatedAt          time.Time              `json:"updatedAt"`
	Stages             []OperationRecordStage `json:"stages,omitempty"`
}

type OperationRecordDetail struct {
	Operation OperationRecordSummary  `json:"operation"`
	Targets   []OperationRecordTarget `json:"targets"`
}

type OperationRecordListResult struct {
	Items    []OperationRecordSummary `json:"items"`
	Total    int                      `json:"total"`
	Page     int                      `json:"page"`
	PageSize int                      `json:"pageSize"`
}

// ListApplicationOperationRecords 分页查询协调记录，读取时聚合生命周期表。
func (s *Service) ListApplicationOperationRecords(ctx context.Context, filter OperationRecordFilter) (OperationRecordListResult, error) {
	if s == nil {
		return OperationRecordListResult{}, panelerr.Validation("application_operation_service_unavailable", "Application service is unavailable")
	}
	if filter.Limit <= 0 || filter.Limit > 200 {
		filter.Limit = 50
	}
	if filter.Offset < 0 {
		filter.Offset = 0
	}
	q := orm.New(s.lifecycleDB()).From("application_lifecycle_operations")
	appendRecordFilter(q, filter)
	total, err := q.Count(ctx)
	if err != nil {
		return OperationRecordListResult{}, err
	}
	q = orm.New(s.lifecycleDB()).From("application_lifecycle_operations")
	appendRecordFilter(q, filter)
	q.OrderBy("created_at DESC", "id DESC").Limit(filter.Limit).Offset(filter.Offset)
	var ops []LifecycleOperation
	if err := q.All(ctx, &ops); err != nil {
		return OperationRecordListResult{}, err
	}
	items := make([]OperationRecordSummary, 0, len(ops))
	for _, op := range ops {
		summary, err := s.recordSummary(ctx, op)
		if err != nil {
			return OperationRecordListResult{}, err
		}
		items = append(items, summary)
	}
	return OperationRecordListResult{Items: items, Total: int(total), PageSize: filter.Limit, Page: filter.Offset/filter.Limit + 1}, nil
}

// GetApplicationOperationRecord 返回一条协调记录详情（操作 + 目标 + 步骤日志）。
func (s *Service) GetApplicationOperationRecord(ctx context.Context, operationID string) (OperationRecordDetail, error) {
	if s == nil {
		return OperationRecordDetail{}, panelerr.Validation("application_operation_service_unavailable", "Application service is unavailable")
	}
	op, err := s.lifecycleOperationByID(ctx, operationID)
	if err != nil {
		if err == sql.ErrNoRows {
			return OperationRecordDetail{}, panelerr.NotFound("application_operation")
		}
		return OperationRecordDetail{}, err
	}
	summary, err := s.recordSummary(ctx, op)
	if err != nil {
		return OperationRecordDetail{}, err
	}
	targets := append([]LifecycleTarget(nil), op.Targets...)
	targets = s.mergeConsistentServers(ctx, op, targets)
	stages, err := s.stagesByOperation(ctx, operationID)
	if err != nil {
		return OperationRecordDetail{}, err
	}
	targetDTOs := make([]OperationRecordTarget, 0, len(targets))
	for _, target := range targets {
		targetDTOs = append(targetDTOs, operationRecordTargetDTO(target, stages[target.ID]))
	}
	return OperationRecordDetail{Operation: summary, Targets: targetDTOs}, nil
}

func appendRecordFilter(q *orm.Query, filter OperationRecordFilter) {
	if strings.TrimSpace(filter.ApplicationID) != "" {
		q.And("application_id = ?", strings.TrimSpace(filter.ApplicationID))
	}
	if value := operationStatusFilterValue(filter.Status); value != "" {
		q.And("status = ?", value)
	}
	if strings.TrimSpace(filter.Source) != "" {
		q.And("trigger = ?", strings.TrimSpace(filter.Source))
	}
	if filter.From != nil {
		q.And("created_at >= ?", formatTime(filter.From.UTC()))
	}
	if filter.To != nil {
		q.And("created_at <= ?", formatTime(filter.To.UTC()))
	}
}

// operationStatusFilterValue 把展示状态映射为生命周期操作状态（列表过滤用）。
func operationStatusFilterValue(display string) string {
	switch strings.TrimSpace(display) {
	case "queued":
		return LifecycleStatusPending
	case "running":
		return LifecycleStatusDeploying
	case "succeeded":
		return LifecycleStatusDeployed
	case "partial_failed":
		return LifecycleStatusPartiallyDeployed
	case "failed":
		return LifecycleStatusFailed
	case "cancelled", "superseded":
		return LifecycleStatusSuperseded
	default:
		return ""
	}
}

func (s *Service) recordSummary(ctx context.Context, op LifecycleOperation) (OperationRecordSummary, error) {
	targets, err := s.lifecycleTargets(ctx, op.ID)
	if err != nil {
		return OperationRecordSummary{}, err
	}
	appName := strings.TrimSpace(op.ApplicationID)
	if app, err := s.Get(ctx, op.ApplicationID); err == nil {
		appName = strings.TrimSpace(firstNonEmpty(app.Name, app.ID))
	}
	summary := operationRecordSummary(op, targets, appName)
	servers, err := s.recordServerNames(ctx, op, targets)
	if err != nil {
		return OperationRecordSummary{}, err
	}
	summary.TargetServers = servers
	return summary, nil
}

func operationRecordSummary(op LifecycleOperation, targets []LifecycleTarget, appName string) OperationRecordSummary {
	summary := OperationRecordSummary{
		OperationID:     op.ID,
		ApplicationID:   op.ApplicationID,
		ApplicationName: appName,
		Action:          lifecycleOperationAction(op),
		Source:          firstNonEmpty(op.Trigger, "system"),
		TriggeredBy:     op.Trigger,
		Status:          operationRecordStatus(op, targets),
		StartedAt:       op.StartedAt,
		FinishedAt:      op.FinishedAt,
		TargetTotal:     len(targets),
		TargetSucceeded: countLifecycleTargets(targets, LifecycleTargetStateSucceeded),
		TargetFailed:    countLifecycleTargets(targets, LifecycleTargetStateFailed, LifecycleTargetStateFailedRetryable, LifecycleTargetStateCancelled),
		LatestAt:        op.UpdatedAt,
		FailureSummary:  firstTargetError(targets),
		CreatedAt:       op.CreatedAt,
		UpdatedAt:       op.UpdatedAt,
	}
	for _, target := range targets {
		if target.UpdatedAt.After(summary.LatestAt) {
			summary.LatestAt = target.UpdatedAt
		}
	}
	return summary
}

func operationRecordStatus(op LifecycleOperation, targets []LifecycleTarget) string {
	switch op.Status {
	case LifecycleStatusPending:
		return "queued"
	case LifecycleStatusDeploying:
		return "running"
	case LifecycleStatusDeployed:
		return "succeeded"
	case LifecycleStatusPartiallyDeployed:
		return "partial_failed"
	case LifecycleStatusFailed:
		return "failed"
	case LifecycleStatusSuperseded:
		return "superseded"
	}
	hasActive, hasFailed, hasSucceeded := false, false, false
	for _, target := range targets {
		switch target.State {
		case LifecycleTargetStateSucceeded:
			hasSucceeded = true
		case LifecycleTargetStateFailed, LifecycleTargetStateFailedRetryable:
			hasFailed = true
		case LifecycleTargetStateCancelled, LifecycleTargetStateSuperseded:
		default:
			hasActive = true
		}
	}
	if hasActive {
		return "running"
	}
	if hasFailed && hasSucceeded {
		return "partial_failed"
	}
	if hasFailed {
		return "failed"
	}
	if hasSucceeded {
		return "succeeded"
	}
	return "queued"
}

func lifecycleTargetRecordStatus(target LifecycleTarget) string {
	switch strings.TrimSpace(target.State) {
	case "consistent":
		return "consistent"
	case LifecycleTargetStateSucceeded:
		return "succeeded"
	case LifecycleTargetStateFailedRetryable, LifecycleTargetStateFailed:
		return "failed"
	case LifecycleTargetStateCancelled:
		return "cancelled"
	case LifecycleTargetStateSuperseded:
		return "superseded"
	case LifecycleTargetStatePlanned, LifecycleTargetStateReady:
		return "queued"
	default:
		return "running"
	}
}

func firstTargetError(targets []LifecycleTarget) string {
	for _, target := range targets {
		if value := strings.TrimSpace(firstNonEmpty(target.ErrorMessage, target.ErrorDetail, target.Error)); value != "" {
			return value
		}
	}
	return ""
}

// recordServerNames 返回记录涉及的服务器显示名（去重，含“一致”的期望服务器）。
func (s *Service) recordServerNames(ctx context.Context, op LifecycleOperation, targets []LifecycleTarget) ([]string, error) {
	seen := map[string]struct{}{}
	names := make([]string, 0, len(targets))
	add := func(id, name string) {
		key := strings.TrimSpace(firstNonEmpty(name, id))
		if key == "" {
			return
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		names = append(names, key)
	}
	for _, target := range targets {
		add(target.ServerID, target.ServerName)
	}
	if app, err := s.Get(ctx, op.ApplicationID); err == nil {
		for _, serverID := range app.DeploymentServers {
			name := strings.TrimSpace(serverID)
			if s.servers != nil {
				if srv, err := s.servers.Get(ctx, serverID); err == nil {
					name = strings.TrimSpace(firstNonEmpty(srv.Name, srv.ID))
				}
			}
			add(serverID, name)
		}
	}
	return names, nil
}

// mergeConsistentServers 把期望部署服务器中“没有目标”的服务器补成一致行。
func (s *Service) mergeConsistentServers(ctx context.Context, op LifecycleOperation, targets []LifecycleTarget) []LifecycleTarget {
	app, err := s.Get(ctx, op.ApplicationID)
	if err != nil {
		return targets
	}
	hasTarget := map[string]struct{}{}
	for _, target := range targets {
		hasTarget[target.ServerID] = struct{}{}
	}
	out := targets
	for _, serverID := range app.DeploymentServers {
		if _, ok := hasTarget[serverID]; ok {
			continue
		}
		serverName := strings.TrimSpace(serverID)
		if s.servers != nil {
			if srv, err := s.servers.Get(ctx, serverID); err == nil {
				serverName = strings.TrimSpace(firstNonEmpty(srv.Name, srv.ID))
			}
		}
		out = append(out, LifecycleTarget{
			ID:                "expected-" + serverID,
			OperationID:       op.ID,
			ApplicationID:     op.ApplicationID,
			ServerID:          serverID,
			ServerName:        serverName,
			State:             "consistent",
			Status:            "consistent",
			DesiredState:      appruntime.DesiredRunning,
			DesiredGeneration: op.Generation,
			DesiredSpecHash:   op.SpecHash,
			CreatedAt:         op.CreatedAt,
			UpdatedAt:         op.UpdatedAt,
		})
	}
	return out
}

// targetStageRow 是 application_target_stages 的字符串行映射：started_at /
// finished_at 在库中以空串表示“未开始/未结束”，不能直接用 time.Time 扫描。
type targetStageRow struct {
	ID         string  `orm:"column:id"`
	TargetID   string  `orm:"column:target_id"`
	Stage      string  `orm:"column:stage"`
	Status     string  `orm:"column:status"`
	Detail     string  `orm:"column:detail"`
	StartedAt  *string `orm:"column:started_at"`
	FinishedAt *string `orm:"column:finished_at"`
}

// stagesByOperation 读取一次操作内所有目标的步骤日志，按目标分组、按开始时间排序。
func (s *Service) stagesByOperation(ctx context.Context, operationID string) (map[string][]OperationRecordStage, error) {
	var rows []targetStageRow
	if err := orm.New(s.lifecycleDB()).From("application_target_stages").
		Where("operation_id = ?", strings.TrimSpace(operationID)).
		OrderBy("started_at ASC", "created_at ASC", "id ASC").
		All(ctx, &rows); err != nil {
		return nil, err
	}
	out := map[string][]OperationRecordStage{}
	for _, row := range rows {
		out[row.TargetID] = append(out[row.TargetID], OperationRecordStage{
			ID:         row.ID,
			Stage:      row.Stage,
			Status:     row.Status,
			Detail:     row.Detail,
			StartedAt:  parseOptionalStringTimePtr(row.StartedAt),
			FinishedAt: parseOptionalStringTimePtr(row.FinishedAt),
		})
	}
	return out, nil
}

func operationRecordTargetDTO(target LifecycleTarget, stages []OperationRecordStage) OperationRecordTarget {
	return OperationRecordTarget{
		ID:                 target.ID,
		OperationID:        target.OperationID,
		ApplicationID:      target.ApplicationID,
		ServerID:           target.ServerID,
		ServerName:         target.ServerName,
		Action:             target.Action,
		State:              target.State,
		Status:             lifecycleTargetRecordStatus(target),
		Stage:              target.Stage,
		Attempt:            target.Attempt,
		NextRunAt:          target.NextRunAt,
		ClaimedTaskID:      target.ClaimedTaskID,
		ContainerName:      target.ContainerName,
		DesiredState:       target.DesiredState,
		DesiredGeneration:  target.DesiredGeneration,
		DesiredSpecHash:    target.DesiredSpecHash,
		ObservedState:      target.ObservedState,
		ObservedExitCode:   target.ObservedExitCode,
		ObservedError:      target.ObservedError,
		ObservedGeneration: target.ObservedGeneration,
		ObservedSpecHash:   target.ObservedSpecHash,
		ObservedImage:      target.ObservedImage,
		ObservedAt:         target.ObservedAt,
		ErrorCode:          target.ErrorCode,
		ErrorMessage:       target.ErrorMessage,
		ErrorDetail:        target.ErrorDetail,
		CreatedAt:          target.CreatedAt,
		StartedAt:          target.StartedAt,
		FinishedAt:         target.FinishedAt,
		UpdatedAt:          target.UpdatedAt,
		Stages:             stages,
	}
}
