package applications

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"panel/internal/platform/activitylog"
	panelerr "panel/internal/platform/errors"
	"panel/internal/platform/i18n"
	id "panel/internal/platform/identity"
)

// DeploymentPlanningError is independent of container state and execution Jobs:
// a rejected plan has no remote execution to report as failed.
type DeploymentPlanningError struct {
	Code          string    `json:"code"`
	Message       string    `json:"message"`
	Field         string    `json:"field,omitempty"`
	FileName      string    `json:"fileName,omitempty"`
	Retryable     bool      `json:"retryable"`
	OperationID   string    `json:"operationId"`
	OccurredAt    time.Time `json:"occurredAt"`
	ConfigVersion int       `json:"configVersion"`
}

func decodePlanningError(raw string) *DeploymentPlanningError {
	var issue DeploymentPlanningError
	if raw == "" || json.Unmarshal([]byte(raw), &issue) != nil || issue.Code == "" {
		return nil
	}
	// A validation message may identify a specific field. Keep it alongside the
	// translated generic error instead of discarding that actionable detail.
	translated := i18n.Translate(issue.Code, issue.Message)
	if issue.Code == "application_invalid" && translated != issue.Message {
		issue.Message = translated + ": " + issue.Message
	} else {
		issue.Message = translated
	}
	return &issue
}

func (s *Service) recordPlanningOutcome(ctx context.Context, app Application, req DeploymentPlanRequest, planErr error) error {
	var previous string
	err := s.db.QueryRowContext(ctx, `SELECT planning_error_json FROM applications WHERE id=? AND version=?`, app.ID, app.Version).Scan(&previous)
	if errors.Is(err, sql.ErrNoRows) {
		return nil // A concurrent edit superseded this attempt.
	}
	if err != nil {
		return err
	}
	var old DeploymentPlanningError
	_ = json.Unmarshal([]byte(previous), &old)
	var issue DeploymentPlanningError
	next := ""
	if planErr != nil {
		issue = DeploymentPlanningError{Code: "application_deployment_plan_failed", Message: "Unable to prepare deployment", Retryable: true, ConfigVersion: app.Version}
		var typed *panelerr.Error
		if errors.As(planErr, &typed) {
			issue.Code, issue.Message = typed.Code, activitylog.Redact(typed.Message)
			issue.Retryable = typed.HTTPStatus >= 500
			// Only these diagnostic fields are public. Never persist arbitrary
			// error details, rendered specifications, environment or file content.
			if field, ok := typed.Details["field"].(string); ok {
				issue.Field = activitylog.Redact(field)
			}
			if name, ok := typed.Details["fileName"].(string); ok {
				issue.FileName = activitylog.Redact(name)
			}
		}
		if !req.Manual && old.ConfigVersion == issue.ConfigVersion && old.Code == issue.Code && old.Message == issue.Message && old.Field == issue.Field && old.FileName == issue.FileName && old.Retryable == issue.Retryable {
			return nil
		}
		issue.OperationID, issue.OccurredAt = id.New("intent"), time.Now().UTC()
		raw, err := json.Marshal(issue)
		if err != nil {
			return err
		}
		next = string(raw)
	} else if previous == "" {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	changed, err := tx.ExecContext(ctx, `UPDATE applications SET planning_error_json=? WHERE id=? AND version=? AND planning_error_json=?`, next, app.ID, app.Version, previous)
	if err != nil {
		return err
	}
	count, err := changed.RowsAffected()
	if err != nil || count == 0 {
		return err
	}
	actor := activitylog.ActorFromContext(ctx)
	if actor.Kind == "" {
		actor = activitylog.Actor{Kind: "controller", ID: "application"}
	}
	event := activitylog.EventInput{
		Domain: "application", Action: "apply", Kind: "lifecycle", Actor: actor,
		Trigger: firstNonEmpty(req.TriggerType, "system"), SourceID: "panel",
		Resources: []activitylog.Resource{{Type: "application", ID: app.ID, Name: app.Name, Generation: app.Generation}},
	}
	if planErr != nil {
		event.EventType, event.Level, event.OperationID = "operation.failed", "error", issue.OperationID
		event.Data = map[string]any{"phase": "ended", "result": "failed", "stage": "planning", "errorCode": issue.Code, "error": issue.Message, "field": issue.Field, "fileName": issue.FileName, "retryable": issue.Retryable, "configVersion": app.Version}
	} else {
		event.EventType, event.Level, event.OperationID = "deployment.planning_recovered", "info", old.OperationID
		event.Data = map[string]any{"stage": "planning", "configVersion": app.Version}
	}
	if _, err := activitylog.AppendTx(ctx, tx, []activitylog.EventInput{event}); err != nil {
		return err
	}
	return tx.Commit()
}
