package diagnostics

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
	"panel/internal/modules/tasks"
)

type fakeTaskRuntimeProvider struct{}

func (fakeTaskRuntimeProvider) TaskRuntime() tasks.RuntimeStats {
	return tasks.RuntimeStats{
		WorkerRunning:     true,
		RegisteredTypes:   12,
		ExecutableTypes:   8,
		PeriodicTypes:     3,
		RunningExecutions: 2,
		Definitions: []tasks.RuntimeDefinitionStats{{
			Type:                    "metrics_collect",
			Hidden:                  true,
			Executable:              true,
			Periodic:                true,
			ConcurrencyPolicy:       tasks.ConcurrencyParallelAllowed,
			PeriodicIntervalSeconds: 5,
		}},
	}
}

func TestSeparateDiagnosticsCollectRuntimeTasksAndSafeDatabaseStatistics(t *testing.T) {
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

	service := NewServiceWithTaskRuntime(fakeTaskRuntimeProvider{}, sources...)
	runtimeSnapshot := service.Runtime()
	taskSnapshot := service.Tasks()
	databaseSnapshots := service.Databases(context.Background())
	if runtimeSnapshot.Process.GoVersion == "" || runtimeSnapshot.Process.CPUCount < 1 || runtimeSnapshot.Process.PID < 1 {
		t.Fatalf("runtime fields not populated: %#v", runtimeSnapshot.Process)
	}
	if len(databaseSnapshots.Databases) != 3 {
		t.Fatalf("databases = %d, want 3", len(databaseSnapshots.Databases))
	}
	if !taskSnapshot.Tasks.WorkerRunning || taskSnapshot.Tasks.RegisteredTypes != 12 || taskSnapshot.Tasks.RunningExecutions != 2 {
		t.Fatalf("task runtime fields not populated: %#v", taskSnapshot.Tasks)
	}
	for _, database := range databaseSnapshots.Databases {
		if !database.Healthy {
			t.Fatalf("database %q unhealthy: %#v", database.Name, database)
		}
		if len(database.Tables) != 1 || database.Tables[0].Name != "visible_items" || database.Tables[0].RowCount != 2 {
			t.Fatalf("unexpected tables for %q: %#v", database.Name, database.Tables)
		}
		if database.TableSizeErrorCode == "" && database.Tables[0].TotalSizeBytes <= 0 {
			t.Fatalf("missing table size for %q: %#v", database.Name, database.Tables[0])
		}
		if database.PageSizeBytes <= 0 || database.PageCount <= 0 || database.FileSizeBytes <= 0 {
			t.Fatalf("missing database sizes for %q: %#v", database.Name, database)
		}
	}
	if len(taskSnapshot.Tasks.Definitions) != 1 || taskSnapshot.Tasks.Definitions[0].Type != "metrics_collect" {
		t.Fatalf("task definitions not populated: %#v", taskSnapshot.Tasks.Definitions)
	}

	payload, err := json.Marshal([]any{runtimeSnapshot, taskSnapshot, databaseSnapshots})
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

	snapshots := NewService(
		DatabaseSource{Name: "app", DB: healthy, Path: path},
		DatabaseSource{Name: "task", DB: failed},
	).Databases(context.Background())

	if !snapshots.Databases[0].Healthy {
		t.Fatalf("healthy database lost: %#v", snapshots.Databases[0])
	}
	if snapshots.Databases[1].Healthy || snapshots.Databases[1].ErrorCode != "database_unavailable" {
		t.Fatalf("failed database not isolated: %#v", snapshots.Databases[1])
	}
}
