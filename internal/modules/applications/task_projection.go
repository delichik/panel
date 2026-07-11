package applications

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"

	"panel/internal/modules/tasks"
)

type deploymentTaskReference struct {
	operationID string
	targetID    string
}

// DecorateDeploymentTasks adds application lifecycle operation and target state to task API rows.
func (s *Service) DecorateDeploymentTasks(ctx context.Context, items []tasks.Task) error {
	if s == nil || len(items) == 0 {
		return nil
	}
	refs := make([]deploymentTaskReference, len(items))
	operationIDs := map[string]struct{}{}
	targetIDs := map[string]struct{}{}
	claimedTaskIDs := map[string]struct{}{}
	taskOperationTargets := map[string][]string{}

	for idx := range items {
		ref := deploymentReferenceFromTask(items[idx])
		refs[idx] = ref
		if ref.operationID != "" {
			operationIDs[ref.operationID] = struct{}{}
		}
		if ref.targetID != "" {
			targetIDs[ref.targetID] = struct{}{}
		}
		if items[idx].ID != "" {
			claimedTaskIDs[items[idx].ID] = struct{}{}
		}
	}

	claimedTargets, err := s.lifecycleTargetsByClaimedTaskIDs(ctx, keysOf(claimedTaskIDs))
	if err != nil {
		return err
	}
	for taskID, target := range claimedTargets {
		if target.ID == "" {
			continue
		}
		targetIDs[target.ID] = struct{}{}
		operationIDs[target.OperationID] = struct{}{}
		for idx := range items {
			if items[idx].ID == taskID {
				if refs[idx].targetID == "" {
					refs[idx].targetID = target.ID
				}
				if refs[idx].operationID == "" {
					refs[idx].operationID = target.OperationID
				}
			}
		}
	}

	targetsByID := map[string]LifecycleTarget{}
	for targetID := range targetIDs {
		target, err := s.lifecycleTargetByID(ctx, targetID)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return err
		}
		targetsByID[target.ID] = target
		if target.OperationID != "" {
			operationIDs[target.OperationID] = struct{}{}
		}
	}

	operationsByID := map[string]LifecycleOperation{}
	for operationID := range operationIDs {
		op, err := s.lifecycleOperationByID(ctx, operationID)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return err
		}
		operationsByID[op.ID] = op
	}

	for idx, ref := range refs {
		if ref.operationID == "" && ref.targetID != "" {
			ref.operationID = targetsByID[ref.targetID].OperationID
			refs[idx] = ref
		}
		if ref.operationID != "" && items[idx].OperationID != "" {
			taskOperationTargets[items[idx].OperationID] = append(taskOperationTargets[items[idx].OperationID], ref.operationID)
		}
	}

	applicationNames, err := s.applicationNamesForOperations(ctx, operationsByID)
	if err != nil {
		return err
	}
	claimedStatuses := s.claimedTaskStatuses(ctx, operationsByID, targetsByID)
	operationProjections := map[string]*tasks.TaskDeploymentOperationProjection{}
	for id, op := range operationsByID {
		projection := s.taskDeploymentOperationProjection(op, applicationNames[op.ApplicationID], claimedStatuses)
		operationProjections[id] = &projection
	}

	for idx := range items {
		ref := refs[idx]
		if ref.operationID == "" && items[idx].OperationID != "" {
			ref.operationID = firstString(taskOperationTargets[items[idx].OperationID])
		}
		var targetProjection *tasks.TaskDeploymentTargetProjection
		if target, ok := targetsByID[ref.targetID]; ok {
			projected := taskDeploymentTargetProjection(target, applicationNames[target.ApplicationID], claimedStatuses[target.ClaimedTaskID])
			targetProjection = &projected
			if ref.operationID == "" {
				ref.operationID = target.OperationID
			}
		}
		operationProjection := operationProjections[ref.operationID]
		if operationProjection == nil && targetProjection == nil {
			continue
		}
		items[idx].Deployment = &tasks.TaskDeploymentProjection{
			Operation: operationProjection,
			Target:    targetProjection,
		}
	}
	return nil
}

func deploymentReferenceFromTask(task tasks.Task) deploymentTaskReference {
	var ref deploymentTaskReference
	read := func(raw string) {
		if strings.TrimSpace(raw) == "" {
			return
		}
		var payload struct {
			LifecycleOperationID string `json:"lifecycleOperationId"`
			LifecycleTargetID    string `json:"lifecycleTargetId"`
		}
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			return
		}
		if ref.operationID == "" {
			ref.operationID = strings.TrimSpace(payload.LifecycleOperationID)
		}
		if ref.targetID == "" {
			ref.targetID = strings.TrimSpace(payload.LifecycleTargetID)
		}
	}
	read(task.MetadataJSON)
	read(task.ParamsJSON)
	return ref
}

func (s *Service) lifecycleOperationByID(ctx context.Context, operationID string) (LifecycleOperation, error) {
	row := s.lifecycleDB().QueryRowContext(ctx, `SELECT id,application_id,type,status,task_id,generation,spec_hash,trigger,error,created_at,started_at,finished_at,updated_at
		FROM application_lifecycle_operations WHERE id=?`, operationID)
	op, err := scanLifecycleOperation(row)
	if err != nil {
		return LifecycleOperation{}, err
	}
	targets, err := s.lifecycleTargets(ctx, op.ID)
	if err != nil {
		return LifecycleOperation{}, err
	}
	op.Targets = targets
	return op, nil
}

func (s *Service) lifecycleTargetsByClaimedTaskIDs(ctx context.Context, taskIDs []string) (map[string]LifecycleTarget, error) {
	out := map[string]LifecycleTarget{}
	for _, taskID := range taskIDs {
		row := s.lifecycleDB().QueryRowContext(ctx, `SELECT id,operation_id,application_id,server_id,action,state,status,target_key,desired_state,desired_generation,desired_spec_hash,priority,attempt,next_run_at,lease_owner,lease_expires_at,claimed_task_id,instance_id,container_name,container_id,stage,error,error_code,error_message,error_detail,created_at,started_at,finished_at,updated_at
			FROM application_lifecycle_targets WHERE claimed_task_id=? ORDER BY updated_at DESC, created_at DESC, id DESC LIMIT 1`, taskID)
		target, err := scanLifecycleTarget(row)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return nil, err
		}
		out[taskID] = target
	}
	return out, nil
}

func (s *Service) applicationNamesForOperations(ctx context.Context, operations map[string]LifecycleOperation) (map[string]string, error) {
	names := map[string]string{}
	for _, op := range operations {
		if op.ApplicationID == "" {
			continue
		}
		if _, ok := names[op.ApplicationID]; ok {
			continue
		}
		app, err := s.Get(ctx, op.ApplicationID)
		if err != nil {
			names[op.ApplicationID] = op.ApplicationID
			continue
		}
		names[op.ApplicationID] = strings.TrimSpace(firstNonEmpty(app.Name, app.ID))
	}
	return names, nil
}

func (s *Service) claimedTaskStatuses(ctx context.Context, operations map[string]LifecycleOperation, targets map[string]LifecycleTarget) map[string]string {
	statuses := map[string]string{}
	collect := func(taskID string) {
		taskID = strings.TrimSpace(taskID)
		if taskID == "" {
			return
		}
		if _, ok := statuses[taskID]; ok {
			return
		}
		if s.tasks == nil {
			statuses[taskID] = ""
			return
		}
		task, err := s.tasks.Get(ctx, taskID)
		if err != nil {
			statuses[taskID] = ""
			return
		}
		statuses[taskID] = task.Status
	}
	for _, target := range targets {
		collect(target.ClaimedTaskID)
	}
	for _, op := range operations {
		for _, target := range op.Targets {
			collect(target.ClaimedTaskID)
		}
	}
	return statuses
}

func (s *Service) taskDeploymentOperationProjection(op LifecycleOperation, appName string, claimedStatuses map[string]string) tasks.TaskDeploymentOperationProjection {
	targets := make([]tasks.TaskDeploymentTargetProjection, 0, len(op.Targets))
	for _, target := range op.Targets {
		targets = append(targets, taskDeploymentTargetProjection(target, appName, claimedStatuses[target.ClaimedTaskID]))
	}
	return tasks.TaskDeploymentOperationProjection{
		ID:              op.ID,
		ApplicationID:   op.ApplicationID,
		ApplicationName: appName,
		Type:            op.Type,
		Status:          op.Status,
		Trigger:         op.Trigger,
		Generation:      op.Generation,
		SpecHash:        op.SpecHash,
		Error:           op.Error,
		Targets:         targets,
		CreatedAt:       op.CreatedAt,
		StartedAt:       op.StartedAt,
		FinishedAt:      op.FinishedAt,
		UpdatedAt:       op.UpdatedAt,
	}
}

func taskDeploymentTargetProjection(target LifecycleTarget, appName, claimedStatus string) tasks.TaskDeploymentTargetProjection {
	return tasks.TaskDeploymentTargetProjection{
		ID:                target.ID,
		OperationID:       target.OperationID,
		ApplicationID:     target.ApplicationID,
		ApplicationName:   appName,
		ServerID:          target.ServerID,
		ServerName:        target.ServerName,
		Action:            target.Action,
		State:             target.State,
		Status:            target.Status,
		Stage:             target.Stage,
		Attempt:           target.Attempt,
		NextRunAt:         target.NextRunAt,
		ClaimedTaskID:     target.ClaimedTaskID,
		ClaimedTaskStatus: claimedStatus,
		InstanceID:        target.InstanceID,
		ContainerName:     target.ContainerName,
		ContainerID:       target.ContainerID,
		DesiredState:      target.DesiredState,
		DesiredGeneration: target.DesiredGeneration,
		DesiredSpecHash:   target.DesiredSpecHash,
		ErrorCode:         target.ErrorCode,
		ErrorMessage:      firstNonEmpty(target.ErrorMessage, target.Error),
		ErrorDetail:       target.ErrorDetail,
		CreatedAt:         target.CreatedAt,
		StartedAt:         target.StartedAt,
		FinishedAt:        target.FinishedAt,
		UpdatedAt:         target.UpdatedAt,
	}
}

func keysOf(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, value)
		}
	}
	return out
}

func firstString(values []string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
