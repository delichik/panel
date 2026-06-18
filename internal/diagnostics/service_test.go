package diagnostics

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestSnapshotCollectsRuntimeAndSafeDatabaseStatistics(t *testing.T) {
	dir := t.TempDir()
	sources := make([]DatabaseSource, 0, 3)
	for _, name := range []string{"app", "task", "metrics"} {
		path := filepath.Join(dir, name+".db")
		db, err := sql.Open("sqlite", path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = db.Close() })
		if _, err := db.Exec(`CREATE TABLE visible_items (id INTEGER PRIMARY KEY, secret TEXT); INSERT INTO visible_items(secret) VALUES ('do-not-leak'), ('hidden-value')`); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, DatabaseSource{Name: name, DB: db, Path: path})
	}

	snapshot := NewService(sources...).Snapshot(context.Background())
	if snapshot.Process.GoVersion == "" || snapshot.Process.CPUCount < 1 || snapshot.Process.PID < 1 {
		t.Fatalf("runtime fields not populated: %#v", snapshot.Process)
	}
	if len(snapshot.Databases) != 3 {
		t.Fatalf("databases = %d, want 3", len(snapshot.Databases))
	}
	for _, database := range snapshot.Databases {
		if !database.Healthy {
			t.Fatalf("database %q unhealthy: %#v", database.Name, database)
		}
		if len(database.Tables) != 1 || database.Tables[0].Name != "visible_items" || database.Tables[0].RowCount != 2 {
			t.Fatalf("unexpected tables for %q: %#v", database.Name, database.Tables)
		}
		if database.PageSizeBytes <= 0 || database.PageCount <= 0 || database.FileSizeBytes <= 0 {
			t.Fatalf("missing database sizes for %q: %#v", database.Name, database)
		}
	}

	payload, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	text := string(payload)
	for _, forbidden := range []string{dir, "do-not-leak", "hidden-value"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("snapshot leaked %q: %s", forbidden, text)
		}
	}
}

func TestSnapshotKeepsHealthyDatabasesWhenOneFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "healthy.db")
	healthy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer healthy.Close()
	if _, err := healthy.Exec(`CREATE TABLE items (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	failed, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "failed.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := failed.Close(); err != nil {
		t.Fatal(err)
	}

	snapshot := NewService(
		DatabaseSource{Name: "app", DB: healthy, Path: path},
		DatabaseSource{Name: "task", DB: failed},
	).Snapshot(context.Background())

	if !snapshot.Databases[0].Healthy {
		t.Fatalf("healthy database lost: %#v", snapshot.Databases[0])
	}
	if snapshot.Databases[1].Healthy || snapshot.Databases[1].ErrorCode != "database_unavailable" {
		t.Fatalf("failed database not isolated: %#v", snapshot.Databases[1])
	}
}
