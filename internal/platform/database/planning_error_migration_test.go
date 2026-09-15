package database

import (
	"path/filepath"
	"testing"

	"panel/internal/platform/config"
)

func TestOpenAddsPlanningErrorColumnWithoutChangingExistingApplication(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.LogDatabase = filepath.Join(dir, "log.db")
	cfg.CoordinationDatabase = filepath.Join(dir, "coordination.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	store, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppDB().Exec(`INSERT INTO applications(id,name,spec_yaml,job_id,version,created_at,updated_at) VALUES('existing','existing','name: existing','panel-existing',7,'2026-09-01T00:00:00Z','2026-09-01T00:00:00Z'); ALTER TABLE applications DROP COLUMN planning_error_json`); err != nil {
		store.Close()
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var name, diagnostic string
	var version int
	if err := store.AppDB().QueryRow(`SELECT name,version,planning_error_json FROM applications WHERE id='existing'`).Scan(&name, &version, &diagnostic); err != nil {
		t.Fatal(err)
	}
	if name != "existing" || version != 7 || diagnostic != "" {
		t.Fatalf("migration changed existing application: %s %d %s", name, version, diagnostic)
	}
}
