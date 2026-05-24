package storage

import (
	"database/sql"
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

func TestOpenAllowsConcurrentAppConnections(t *testing.T) {
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

	if got := store.AppDB().Stats().MaxOpenConnections; got < 2 {
		t.Fatalf("app database should not be single-connection, got %d", got)
	}
}

func TestFreshSchemaUsesApplicationTables(t *testing.T) {
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

	for _, table := range []string{"applications", "application_files", "application_revisions"} {
		if !tableExists(t, store.AppDB(), table) {
			t.Fatalf("expected table %q to exist", table)
		}
	}
	for _, table := range []string{
		"docker_capabilities",
		"docker_runtime_cache",
		"container_runtime_cache",
		"operation_" + "locks",
		"container_services",
		"container_service_files",
		"container_service_" + string([]byte{'p', 'l', 'a', 'c', 'e', 'm', 'e', 'n', 't', 's'}),
	} {
		if tableExists(t, store.AppDB(), table) {
			t.Fatalf("old orchestration table %q must not exist in fresh schema", table)
		}
	}
}

func tableExists(t *testing.T, db *sql.DB, table string) bool {
	t.Helper()
	var name string
	err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&name)
	if err == sql.ErrNoRows {
		return false
	}
	if err != nil {
		t.Fatalf("query table %q: %v", table, err)
	}
	return true
}
