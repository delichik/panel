package applications

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"panel/internal/platform/database/orm"
	id "panel/internal/platform/identity"
)

// finishTargetRunningStages closes every still-running stage row of a target.
// exceptStage (optional) keeps one stage open, e.g. the stage that is about to
// start or the stage that just failed. This prevents earlier steps from
// remaining "running" forever in operation records once a target moves on.
func (s *Service) finishTargetRunningStages(ctx context.Context, targetID, status string, finishedAt *time.Time, exceptStage string) error {
	if s == nil || strings.TrimSpace(targetID) == "" {
		return nil
	}
	finish := time.Now().UTC()
	if finishedAt != nil {
		finish = *finishedAt
	}
	exceptStage = strings.TrimSpace(exceptStage)
	_, err := orm.RawExec(ctx, s.lifecycleDB(), `UPDATE application_target_stages
		SET status=?, finished_at=COALESCE(finished_at, ?), updated_at=?
		WHERE target_id=? AND status='running' AND (?='' OR stage<>?)`,
		strings.TrimSpace(status), formatTime(finish), formatTime(finish), targetID, exceptStage, exceptStage)
	return err
}

// recordTargetStage upserts one step row into application_target_stages for a
// lifecycle target. The same (target, stage) pair is updated in place so a
// stage that starts and later finishes keeps its original started_at.
func (s *Service) recordTargetStage(ctx context.Context, targetID, stage, status, detail string, startedAt, finishedAt *time.Time) error {
	if s == nil || strings.TrimSpace(targetID) == "" || strings.TrimSpace(stage) == "" {
		return nil
	}
	var operationID, applicationID, serverID string
	err := s.lifecycleDB().QueryRowContext(ctx,
		`SELECT operation_id, application_id, server_id FROM application_lifecycle_targets WHERE id=?`, targetID).
		Scan(&operationID, &applicationID, &serverID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return err
	}
	now := time.Now().UTC()
	start := startedAt
	if start == nil || start.IsZero() {
		start = &now
	}
	_, err = orm.RawExec(ctx, s.lifecycleDB(), `INSERT INTO application_target_stages(id, operation_id, target_id, application_id, server_id, stage, status, detail, started_at, finished_at, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(target_id, stage) DO UPDATE SET
			status=excluded.status,
			detail=CASE WHEN excluded.detail <> '' THEN excluded.detail ELSE application_target_stages.detail END,
			started_at=COALESCE(application_target_stages.started_at, excluded.started_at),
			finished_at=COALESCE(excluded.finished_at, application_target_stages.finished_at),
			updated_at=excluded.updated_at`,
		id.New("atst"), operationID, targetID, applicationID, serverID, strings.TrimSpace(stage), strings.TrimSpace(status),
		strings.TrimSpace(detail), formatTime(*start), optionalTimeString(finishedAt), formatTime(now), formatTime(now))
	return err
}
