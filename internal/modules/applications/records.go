package applications

import (
	"context"
	"encoding/json"
	controlplane "panel/internal/orchestrator"
	"panel/internal/platform/database/models"
	"panel/internal/platform/database/orm"
	panelerr "panel/internal/platform/errors"
	"sort"
	"strings"
	"time"
)

// 协调记录：记录页列表/详情直接聚合 AppDB jobs（按 intent_id 分组），不再
// 读取旧 lifecycle 表。一个 intent（一次触发）对应一条协调记录，一个
// application/server Job 对应一个目标。

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

type operationGroup struct {
	IntentID      string
	ApplicationID string
	Jobs          []jobRecordRow
}

type jobRecordRow struct {
	ID                string
	ApplicationID     string
	ServerID          string
	InstanceID        string
	Action            string
	State             string
	Attempt           int
	NextRunAt         *time.Time
	IntentID          string
	TriggerType       string
	TriggerResourceID string
	Reason            string
	DesiredGeneration int
	DesiredSpecHash   string
	LastStage         string
	LastStepsJSON     string
	ErrorCode         string
	ErrorMessage      string
	ErrorDetail       string
	CreatedAt         time.Time
	StartedAt         *time.Time
	FinishedAt        *time.Time
	UpdatedAt         time.Time
}

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
	groups, err := s.operationGroups(ctx, filter)
	if err != nil {
		return OperationRecordListResult{}, err
	}
	items := make([]OperationRecordSummary, 0, len(groups))
	for _, group := range groups {
		summary, err := s.recordSummaryFromJobs(ctx, group)
		if err != nil {
			return OperationRecordListResult{}, err
		}
		if filter.Status != "" && summary.Status != filter.Status {
			continue
		}
		items = append(items, summary)
	}
	total := len(items)
	start := filter.Offset
	if start > total {
		start = total
	}
	end := start + filter.Limit
	if end > total {
		end = total
	}
	items = items[start:end]
	return OperationRecordListResult{Items: items, Total: total, Page: filter.Offset/filter.Limit + 1, PageSize: filter.Limit}, nil
}

func (s *Service) GetApplicationOperationRecord(ctx context.Context, operationID string) (OperationRecordDetail, error) {
	if s == nil {
		return OperationRecordDetail{}, panelerr.Validation("application_operation_service_unavailable", "Application service is unavailable")
	}
	filter := OperationRecordFilter{ApplicationID: "", Limit: 200}
	groups, err := s.operationGroups(ctx, filter)
	if err != nil {
		return OperationRecordDetail{}, err
	}
	for _, group := range groups {
		if group.IntentID != operationID {
			continue
		}
		summary, err := s.recordSummaryFromJobs(ctx, group)
		if err != nil {
			return OperationRecordDetail{}, err
		}
		targets, err := s.recordTargetsFromJobs(ctx, group)
		if err != nil {
			return OperationRecordDetail{}, err
		}
		return OperationRecordDetail{Operation: summary, Targets: targets}, nil
	}
	return OperationRecordDetail{}, panelerr.NotFound("application_operation")
}

// operationGroups 按 intent_id 聚合 AppDB jobs，并按过滤条件排序分页。
func (s *Service) operationGroups(ctx context.Context, filter OperationRecordFilter) ([]operationGroup, error) {
	query := orm.New(s.db).From("jobs")
	// 设施应用不属于面向普通用户的协调记录；这里在 SQL 侧同时排除全部
	// facility 类型与保留 identity，避免详情接口通过子查询遗漏后再暴露。
	query = query.Where(
		"application_id NOT IN (SELECT id FROM applications WHERE kind=? OR id=?)",
		ApplicationKindFacility,
		FacilityProxyApplicationID,
	)
	if strings.TrimSpace(filter.ApplicationID) != "" {
		query = query.Where("application_id=?", strings.TrimSpace(filter.ApplicationID))
	}
	if strings.TrimSpace(filter.Source) != "" {
		query = query.Where("trigger_type=?", strings.TrimSpace(filter.Source))
	}
	if filter.From != nil {
		query = query.Where("created_at >= ?", formatTime(filter.From.UTC()))
	}
	if filter.To != nil {
		query = query.Where("created_at <= ?", formatTime(filter.To.UTC()))
	}
	rows := []models.Job{}
	if err := query.OrderBy("created_at DESC", "id DESC").All(ctx, &rows); err != nil {
		return nil, err
	}
	groups := []operationGroup{}
	index := map[string]int{}
	for _, m := range rows {
		intent := strings.TrimSpace(m.IntentID)
		if intent == "" {
			intent = m.ID
		}
		key := intent + "\x00" + m.ApplicationID
		if idx, ok := index[key]; ok {
			groups[idx].Jobs = append(groups[idx].Jobs, jobRecordRowFromModel(m))
			continue
		}
		index[key] = len(groups)
		groups = append(groups, operationGroup{IntentID: intent, ApplicationID: m.ApplicationID, Jobs: []jobRecordRow{jobRecordRowFromModel(m)}})
	}
	// 最新操作在前；同一 intent 内按服务器聚合。
	sort.SliceStable(groups, func(i, j int) bool {
		return groups[i].latest().After(groups[j].latest())
	})
	return groups, nil
}

func jobRecordRowFromModel(m models.Job) jobRecordRow {
	return jobRecordRow{
		ID:                m.ID,
		ApplicationID:     m.ApplicationID,
		ServerID:          m.ServerID,
		InstanceID:        m.InstanceID,
		Action:            m.Action,
		State:             m.State,
		Attempt:           m.Attempts,
		NextRunAt:         m.NextRunAt,
		IntentID:          m.IntentID,
		TriggerType:       m.TriggerType,
		TriggerResourceID: m.TriggerResourceID,
		Reason:            m.Reason,
		DesiredGeneration: m.DesiredGeneration,
		DesiredSpecHash:   m.DesiredSpecHash,
		LastStage:         m.LastStage,
		LastStepsJSON:     string(jsonBytes(m.LastStepsJSON)),
		ErrorCode:         m.ErrorCode,
		ErrorMessage:      m.ErrorMessage,
		ErrorDetail:       m.ErrorDetail,
		CreatedAt:         m.CreatedAt,
		StartedAt:         m.StartedAt,
		FinishedAt:        m.FinishedAt,
		UpdatedAt:         m.UpdatedAt,
	}
}

func jsonBytes(value []map[string]any) []byte {
	if value == nil {
		return nil
	}
	out, _ := json.Marshal(value)
	return out
}

func (g operationGroup) latest() time.Time {
	latest := g.Jobs[0].CreatedAt
	for _, job := range g.Jobs {
		if job.UpdatedAt.After(latest) {
			latest = job.UpdatedAt
		}
	}
	return latest
}

func (s *Service) recordSummaryFromJobs(ctx context.Context, group operationGroup) (OperationRecordSummary, error) {
	appName := strings.TrimSpace(group.ApplicationID)
	if app, err := s.Get(ctx, group.ApplicationID); err == nil {
		appName = strings.TrimSpace(firstNonEmpty(app.Name, app.ID))
	}
	total := len(group.Jobs)
	succeeded := 0
	failed := 0
	active := false
	var firstCreated, latest time.Time
	var firstError string
	servers := []string{}
	actions := map[string]bool{}
	for _, job := range group.Jobs {
		if firstCreated.IsZero() || job.CreatedAt.Before(firstCreated) {
			firstCreated = job.CreatedAt
		}
		if job.UpdatedAt.After(latest) {
			latest = job.UpdatedAt
		}
		actions[job.Action] = true
		if job.State == controlplane.JobSucceeded {
			succeeded++
		}
		if job.State == controlplane.JobFailed || job.State == controlplane.JobCancelled {
			failed++
		}
		if job.State == controlplane.JobPending || job.State == controlplane.JobRunning || job.State == controlplane.JobFailedRetryable {
			active = true
		}
		if firstError == "" && strings.TrimSpace(job.ErrorMessage) != "" {
			firstError = job.ErrorMessage
		}
		if serverName := s.recordServerName(ctx, job.ServerID); serverName != "" {
			servers = append(servers, serverName)
		}
	}
	summary := OperationRecordSummary{
		OperationID:     group.IntentID,
		ApplicationID:   group.ApplicationID,
		ApplicationName: appName,
		Action:          firstOperationAction(actions),
		Source:          firstNonEmpty(group.Jobs[0].TriggerType, "system"),
		TriggeredBy:     firstNonEmpty(group.Jobs[0].Reason, group.Jobs[0].TriggerResourceID),
		Status:          operationRecordStatusFromJobs(group.Jobs),
		TargetTotal:     total,
		TargetSucceeded: succeeded,
		TargetFailed:    failed,
		TargetServers:   uniqueStringItems(servers),
		LatestAt:        latest,
		FailureSummary:  firstError,
		CreatedAt:       firstCreated,
		UpdatedAt:       latest,
	}
	for _, job := range group.Jobs {
		if job.StartedAt != nil && (summary.StartedAt == nil || job.StartedAt.Before(*summary.StartedAt)) {
			summary.StartedAt = job.StartedAt
		}
		if job.FinishedAt != nil && (summary.FinishedAt == nil || job.FinishedAt.After(*summary.FinishedAt)) {
			summary.FinishedAt = job.FinishedAt
		}
	}
	if !active {
		// 全部终态：completed/failed 事件语义已由 Job 终态表达。
		_ = summary
	}
	return summary, nil
}

func (s *Service) recordServerName(ctx context.Context, serverID string) string {
	if s.servers == nil {
		return serverID
	}
	if srv, err := s.servers.Get(ctx, serverID); err == nil {
		return strings.TrimSpace(firstNonEmpty(srv.Name, srv.ID))
	}
	return serverID
}

func (s *Service) recordTargetsFromJobs(ctx context.Context, group operationGroup) ([]OperationRecordTarget, error) {
	instanceRows, err := s.recordInstanceRows(ctx, group.Jobs)
	if err != nil {
		return nil, err
	}
	out := make([]OperationRecordTarget, 0, len(group.Jobs))
	for _, job := range group.Jobs {
		target := OperationRecordTarget{
			ID:                job.ID,
			OperationID:       group.IntentID,
			ApplicationID:     job.ApplicationID,
			ServerID:          job.ServerID,
			ServerName:        s.recordServerName(ctx, job.ServerID),
			Action:            job.Action,
			State:             job.State,
			Status:            operationRecordTargetStatus(job.State),
			Stage:             job.LastStage,
			Attempt:           job.Attempt,
			NextRunAt:         job.NextRunAt,
			ContainerName:     "",
			DesiredState:      desiredStateForAction(job.Action),
			DesiredGeneration: job.DesiredGeneration,
			DesiredSpecHash:   job.DesiredSpecHash,
			ErrorCode:         job.ErrorCode,
			ErrorMessage:      job.ErrorMessage,
			ErrorDetail:       job.ErrorDetail,
			CreatedAt:         job.CreatedAt,
			StartedAt:         job.StartedAt,
			FinishedAt:        job.FinishedAt,
			UpdatedAt:         job.UpdatedAt,
			Stages:            stagesFromJobSteps(job),
		}
		if inst, ok := instanceRows[job.InstanceID]; ok {
			target.ContainerName = strings.TrimSpace(firstNonEmpty(inst.ContainerName, inst.ObservedContainerName))
			target.ObservedState = inst.ObservedState
			target.ObservedExitCode = ""
			target.ObservedError = inst.LastError
			target.ObservedGeneration = inst.ObservedGeneration
			target.ObservedSpecHash = inst.ObservedSpecHash
			target.ObservedImage = inst.ObservedImageDigest
			target.ObservedAt = inst.ObservedAt
		}
		out = append(out, target)
	}
	return out, nil
}

func (s *Service) recordInstanceRows(ctx context.Context, jobs []jobRecordRow) (map[string]models.ApplicationInstance, error) {
	out := map[string]models.ApplicationInstance{}
	if len(jobs) == 0 {
		return out, nil
	}
	ids := make([]any, 0, len(jobs))
	for _, job := range jobs {
		if job.InstanceID != "" {
			ids = append(ids, job.InstanceID)
		}
	}
	if len(ids) == 0 {
		return out, nil
	}
	var rows []models.ApplicationInstance
	if err := orm.New(s.db).From("application_instances").AndIn("id", ids).All(ctx, &rows); err != nil {
		return out, err
	}
	for _, row := range rows {
		out[row.ID] = row
	}
	return out, nil
}

func operationRecordStatusFromJobs(jobs []jobRecordRow) string {
	hasActive, hasFailed, hasSucceeded, hasCancelled := false, false, false, false
	for _, job := range jobs {
		switch job.State {
		case controlplane.JobSucceeded:
			hasSucceeded = true
		case controlplane.JobFailed:
			hasFailed = true
		case controlplane.JobCancelled:
			hasCancelled = true
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
	if hasCancelled {
		return "cancelled"
	}
	if hasSucceeded {
		return "succeeded"
	}
	return "queued"
}

func operationRecordTargetStatus(state string) string {
	switch state {
	case controlplane.JobSucceeded:
		return "succeeded"
	case controlplane.JobFailedRetryable, controlplane.JobFailed:
		return "failed"
	case controlplane.JobCancelled:
		return "cancelled"
	case controlplane.JobPending:
		return "queued"
	case controlplane.JobRunning:
		return "running"
	default:
		return "queued"
	}
}

func desiredStateForAction(action string) string {
	switch action {
	case controlplane.ActionStop:
		return controlplane.DesiredStopped
	case controlplane.ActionPurge:
		return controlplane.DesiredPurged
	default:
		return controlplane.DesiredRunning
	}
}

func firstOperationAction(actions map[string]bool) string {
	for _, action := range []string{controlplane.ActionPurge, controlplane.ActionStop, controlplane.ActionApply} {
		if actions[action] {
			return action
		}
	}
	return controlplane.ActionApply
}

func stagesFromJobSteps(job jobRecordRow) []OperationRecordStage {
	if strings.TrimSpace(job.LastStepsJSON) == "" || job.LastStepsJSON == "null" || job.LastStepsJSON == "[]" {
		return nil
	}
	var steps []controlplane.Step
	if err := json.Unmarshal([]byte(job.LastStepsJSON), &steps); err != nil {
		return nil
	}
	stages := make([]OperationRecordStage, 0, len(steps))
	for idx, step := range steps {
		stages = append(stages, OperationRecordStage{
			ID:     job.ID + ":" + step.Name,
			Stage:  step.Name,
			Status: stepStatus(step.Status),
			Detail: step.Detail,
		})
		_ = idx
	}
	return stages
}

func stepStatus(status string) string {
	switch strings.TrimSpace(status) {
	case "succeeded", "success", "ok":
		return "succeeded"
	case "failed", "error":
		return "failed"
	case "running", "started", "":
		return "running"
	default:
		return status
	}
}
