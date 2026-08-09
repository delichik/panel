package applications

import (
	"context"
	"log"
	"time"

	"panel/internal/platform/database/orm"
)

// StageCleanupSettings 控制协调库步骤日志的保留策略。
type StageCleanupSettings struct {
	RetentionDays int
	Schedule      string
}

// StageCleanupWorker 定期清理 application_target_stages 中超过保留期的步骤日志。
// 生命周期操作/目标是 durable 协调事实，不参与清理。
type StageCleanupWorker struct {
	service  *Service
	settings func() StageCleanupSettings
	cancel   context.CancelFunc
}

func NewStageCleanupWorker(service *Service, settings func() StageCleanupSettings) *StageCleanupWorker {
	return &StageCleanupWorker{service: service, settings: settings}
}

func (w *StageCleanupWorker) Start(parent context.Context) {
	if w == nil || w.service == nil || w.settings == nil || w.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(parent)
	w.cancel = cancel
	go w.loop(ctx)
}

func (w *StageCleanupWorker) Stop() {
	if w == nil || w.cancel == nil {
		return
	}
	w.cancel()
	w.cancel = nil
}

func (w *StageCleanupWorker) loop(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	var lastRun time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			settings := w.settings()
			if time.Since(lastRun) < stageCleanupInterval(settings.Schedule) {
				continue
			}
			deleted, err := w.service.CleanupStages(ctx, settings.RetentionDays)
			if err != nil {
				log.Printf("coordination stage cleanup failed: %v", err)
				continue
			}
			lastRun = time.Now()
			if deleted > 0 {
				log.Printf("coordination stage cleanup deleted=%d", deleted)
			}
		}
	}
}

func stageCleanupInterval(schedule string) time.Duration {
	switch schedule {
	case "hourly":
		return time.Hour
	case "weekly":
		return 7 * 24 * time.Hour
	default:
		return 24 * time.Hour
	}
}

// CleanupStages 删除超过保留期的目标步骤日志，返回删除行数。
func (s *Service) CleanupStages(ctx context.Context, retentionDays int) (int, error) {
	if s == nil || retentionDays < 1 {
		return 0, nil
	}
	cutoff := formatTime(time.Now().UTC().AddDate(0, 0, -retentionDays))
	res, err := orm.RawExec(ctx, s.lifecycleDB(), `DELETE FROM application_target_stages WHERE created_at < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	affected, _ := res.RowsAffected()
	return int(affected), nil
}
