package applications

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"go.uber.org/zap"

	"panel/internal/platform/activitylog"
	panelerr "panel/internal/platform/errors"
	"panel/internal/platform/i18n"
	id "panel/internal/platform/identity"
	"panel/internal/platform/logcontexts"
	"panel/internal/platform/logging"
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
	resources := []activitylog.Resource{{Type: "application", ID: app.ID, Name: app.Name, Generation: app.Generation}}
	operationID := old.OperationID
	eventType := "deployment.planning_recovered"
	hadError := false
	fields := []zap.Field{
		zap.String("action", "apply"),
		zap.String("trigger", firstNonEmpty(req.TriggerType, "system")),
		zap.String("stage", "planning"),
		zap.Int("configVersion", app.Version),
		zap.String("applicationId", app.ID),
	}
	msg := eventType
	if planErr != nil {
		operationID = issue.OperationID
		eventType = "operation.failed"
		hadError = true
		msg = issue.Message
		if msg == "" {
			msg = eventType
		}
		fields = append(fields,
			zap.String("phase", "ended"),
			zap.String("result", "failed"),
			zap.String("errorCode", issue.Code),
			zap.String("field", issue.Field),
			zap.String("fileName", issue.FileName),
			zap.Bool("retryable", issue.Retryable),
		)
	}
	fields = append(fields, zap.String("eventType", eventType))
	logCtx := logging.Ensure(ctx, logging.Context{Module: "applications", OperationID: operationID})
	if hadError {
		logging.From(logCtx).Error(msg, fields...)
	} else {
		logging.From(logCtx).Info(msg, fields...)
	}
	result := ""
	if hadError {
		result = "failed"
	}
	_ = logging.RecordContext(logCtx, logging.ContextMeta{
		Origin:        logcontexts.OriginProcess,
		Title:         eventType,
		Summary:       msg,
		Result:        result,
		HadError:      hadError,
		Finished:      hadError,
		ActorJSON:     logging.MarshalActorJSON(map[string]string{"kind": actor.Kind, "id": actor.ID, "name": actor.Name}),
		ResourcesJSON: logging.MarshalResourcesJSON(resources),
	})
	return tx.Commit()
}
