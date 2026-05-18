package storage

import (
	"path/filepath"
	"testing"

	"panel/internal/config"
)

func TestOpenCreatesSeparateSchemas(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	store, err := Open(cfg)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	if _, err := store.AppDB().Exec(`INSERT INTO tasks(id,type,status,created_at) VALUES('task_test','x','queued','now')`); err != nil {
		t.Fatalf("app schema missing tasks table: %v", err)
	}
	if _, err := store.MetricsDB().Exec(`INSERT INTO metrics_snapshots(server_id,time,cpu_usage_percent,memory_used_bytes,memory_total_bytes,disk_used_bytes,disk_total_bytes,network_rx_bps,network_tx_bps) VALUES('srv','now',1,2,3,4,5,6,7)`); err != nil {
		t.Fatalf("metrics schema missing snapshots table: %v", err)
	}
	if _, err := store.AppDB().Exec(`INSERT INTO metrics_snapshots(server_id,time,cpu_usage_percent,memory_used_bytes,memory_total_bytes,disk_used_bytes,disk_total_bytes,network_rx_bps,network_tx_bps) VALUES('srv','now',1,2,3,4,5,6,7)`); err == nil {
		t.Fatal("metrics table must not exist in app database")
	}
}
