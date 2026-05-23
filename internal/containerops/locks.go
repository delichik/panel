package containerops

import (
	"context"
	"database/sql"
	"time"
)

type LeaseService struct {
	db  *sql.DB
	ttl time.Duration
}

func NewLeaseService(db *sql.DB, ttl time.Duration) *LeaseService {
	if ttl <= 0 {
		ttl = time.Minute
	}
	return &LeaseService{db: db, ttl: ttl}
}

func (s *LeaseService) Acquire(ctx context.Context, scope, resourceID, ownerTaskID string) (bool, error) {
	now := time.Now().UTC()
	expires := now.Add(s.ttl)
	res, err := s.db.ExecContext(ctx, `INSERT INTO operation_locks(scope,resource_id,owner_task_id,expires_at,heartbeat_at) VALUES(?,?,?,?,?)
		ON CONFLICT(scope,resource_id) DO UPDATE SET owner_task_id=excluded.owner_task_id,expires_at=excluded.expires_at,heartbeat_at=excluded.heartbeat_at
		WHERE operation_locks.owner_task_id=excluded.owner_task_id OR operation_locks.expires_at<=?`,
		scope, resourceID, ownerTaskID, expires.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if err != nil {
		return false, err
	}
	affected, _ := res.RowsAffected()
	return affected > 0, nil
}

func (s *LeaseService) Heartbeat(ctx context.Context, scope, resourceID, ownerTaskID string) error {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `UPDATE operation_locks SET expires_at=?, heartbeat_at=? WHERE scope=? AND resource_id=? AND owner_task_id=?`,
		now.Add(s.ttl).Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), scope, resourceID, ownerTaskID)
	return err
}

func (s *LeaseService) Release(ctx context.Context, scope, resourceID, ownerTaskID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM operation_locks WHERE scope=? AND resource_id=? AND owner_task_id=?`, scope, resourceID, ownerTaskID)
	return err
}
