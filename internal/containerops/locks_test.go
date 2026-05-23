package containerops

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"panel/internal/config"
	"panel/internal/storage"
)

func TestLeaseAcquireHeartbeatExpiryAndRelease(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	locks := NewLeaseService(store.AppDB(), time.Minute)
	ctx := context.Background()
	ok, err := locks.Acquire(ctx, "service", "svc_1", "task_1")
	if err != nil || !ok {
		t.Fatalf("first acquire = %v, %v", ok, err)
	}
	ok, err = locks.Acquire(ctx, "service", "svc_1", "task_2")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("second owner should not acquire live lease")
	}
	if err := locks.Heartbeat(ctx, "service", "svc_1", "task_1"); err != nil {
		t.Fatal(err)
	}
	expired := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)
	if _, err := store.AppDB().ExecContext(ctx, `UPDATE operation_locks SET expires_at=? WHERE scope=? AND resource_id=?`, expired, "service", "svc_1"); err != nil {
		t.Fatal(err)
	}
	ok, err = locks.Acquire(ctx, "service", "svc_1", "task_2")
	if err != nil || !ok {
		t.Fatalf("expired lease should be acquirable = %v, %v", ok, err)
	}
	if err := locks.Release(ctx, "service", "svc_1", "task_2"); err != nil {
		t.Fatal(err)
	}
	ok, err = locks.Acquire(ctx, "service", "svc_1", "task_3")
	if err != nil || !ok {
		t.Fatalf("released lease should be acquirable = %v, %v", ok, err)
	}
}
