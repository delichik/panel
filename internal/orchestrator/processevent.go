package orchestrator

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"
	"panel/internal/platform/events"
	"panel/internal/platform/logging"
)

// Monitor reason codes persisted in Job diagnostics and process summaries (ENG-EVT-010).
const (
	MonitorReasonDispatchWait           = "dispatch_wait"
	MonitorReasonTransportUnconfirmed   = "transport_unconfirmed"
	MonitorReasonLeaseExpired           = "lease_expired"
	MonitorReasonAwaitingVerifyRunning  = "awaiting_verify_running"
)

func (c *Controller) resolveNames(ctx context.Context, appID, serverID string) (appName, serverName string) {
	appName, serverName = appID, serverID
	if c == nil || c.store == nil || c.store.db == nil {
		return appName, serverName
	}
	_ = c.store.db.QueryRowContext(ctx, `SELECT name FROM applications WHERE id=?`, appID).Scan(&appName)
	_ = c.store.db.QueryRowContext(ctx, `SELECT name FROM servers WHERE id=?`, serverID).Scan(&serverName)
	if strings.TrimSpace(appName) == "" {
		appName = appID
	}
	if strings.TrimSpace(serverName) == "" {
		serverName = serverID
	}
	return appName, serverName
}

func (c *Controller) deployResources(ctx context.Context, job Job) []logging.Resource {
	appName, serverName := c.resolveNames(ctx, job.ApplicationID, job.ServerID)
	return []logging.Resource{
		logging.ResourceRef("application", job.ApplicationID, appName, "target"),
		logging.ResourceRef("server", job.ServerID, serverName, "executor"),
	}
}

func (c *Controller) intentEventID(job Job) string {
	if id := strings.TrimSpace(job.IntentID); id != "" {
		return id
	}
	return strings.TrimSpace(job.ID)
}

func (c *Controller) emitIntentProcess(ctx context.Context, job Job, eventType, summary, state string, hadError, finished bool, fields ...zap.Field) {
	if c == nil || strings.TrimSpace(job.ApplicationID) == "" {
		return
	}
	appName, serverName := c.resolveNames(ctx, job.ApplicationID, job.ServerID)
	title := logging.DeployTitle(job.Action, appName, serverName)
	extras := append([]zap.Field{
		zap.String("jobId", job.ID),
		zap.String("action", job.Action),
		zap.Int("attempt", job.Attempts),
		zap.String("lastStage", job.LastStage),
	}, fields...)
	_ = logging.EmitProcess(ctx, logging.ProcessSpec{
		EventID:     c.intentEventID(job),
		Module:      "orchestrator",
		Origin:      events.OriginProcess,
		OperationID: c.intentEventID(job),
		ExecutionID: strings.TrimSpace(job.ExecutionID),
		Title:       title,
		Summary:     summary,
		State:       state,
		HadError:    hadError,
		Finished:    finished,
		EventType:   eventType,
		Resources:   c.deployResources(ctx, job),
		Fields:      extras,
	})
}

func (c *Controller) emitMonitorEntered(ctx context.Context, job Job, reasonCode, reasonText string) {
	summary := fmt.Sprintf("Entered monitor (%s): %s", reasonCode, strings.TrimSpace(reasonText))
	if stage := strings.TrimSpace(job.LastStage); stage != "" {
		summary = fmt.Sprintf("%s; last stage %s", summary, stage)
	}
	c.emitIntentProcess(ctx, job, "deploy.monitor_entered", summary, logging.ProcessRunning, false, false,
		zap.String("monitorReasonCode", reasonCode))
}

func (c *Controller) emitProgressProjected(ctx context.Context, job Job, stage string) {
	stage = strings.TrimSpace(stage)
	if stage == "" {
		return
	}
	summary := fmt.Sprintf("Progress: %s (attempt %d)", stage, job.Attempts)
	c.emitIntentProcess(ctx, job, "deploy.progress_projected", summary, logging.ProcessRunning, false, false,
		zap.String("stage", stage))
}

func monitorReasonFromMessage(msg string) string {
	msg = strings.ToLower(strings.TrimSpace(msg))
	switch {
	case strings.Contains(msg, "dispatch") || strings.Contains(msg, "awaiting agent progress"):
		return MonitorReasonDispatchWait
	case strings.Contains(msg, "lease expired"):
		return MonitorReasonLeaseExpired
	case strings.Contains(msg, "verify_running"):
		return MonitorReasonAwaitingVerifyRunning
	default:
		return MonitorReasonTransportUnconfirmed
	}
}

func evidenceDeadlineAt(job Job, window time.Duration) *time.Time {
	if job.UpdatedAt.IsZero() || window <= 0 {
		return nil
	}
	t := job.UpdatedAt.UTC().Add(window)
	return &t
}
