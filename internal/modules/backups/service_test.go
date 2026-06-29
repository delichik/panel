package backups

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestStartExportSchedulesContainerRestart(t *testing.T) {
	dir := t.TempDir()
	restarter := &fakeRestarter{supported: true}
	svc := NewService(ArchiveConfig{DataRoot: dir}, WithRestarter(restarter))

	resp, err := svc.StartExport(context.Background(), ExportRequest{Encrypt: true})
	if err != nil {
		t.Fatal(err)
	}
	if resp.ExportID == "" {
		t.Fatal("expected export id")
	}
	if !resp.RestartSupported {
		t.Fatal("expected restart support in response")
	}
	if restarter.calls != 1 {
		t.Fatalf("expected one scheduled restart, got %d", restarter.calls)
	}
	if _, err := os.Stat(filepath.Join(exportPendingDir(dir), "pending.json")); err != nil {
		t.Fatalf("expected pending export marker: %v", err)
	}
}

func TestStartExportDoesNotScheduleRestartWhenUnsupported(t *testing.T) {
	dir := t.TempDir()
	restarter := &fakeRestarter{supported: false}
	svc := NewService(ArchiveConfig{DataRoot: dir}, WithRestarter(restarter))

	resp, err := svc.StartExport(context.Background(), ExportRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if resp.RestartSupported {
		t.Fatal("expected restart support to be false")
	}
	if restarter.calls != 0 {
		t.Fatalf("expected no scheduled restart, got %d", restarter.calls)
	}
}

func TestSavePendingRestoreSchedulesContainerRestart(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "backup.panel-backup")
	sourceRoot := t.TempDir()
	if err := os.MkdirAll(sourceRoot, 0700); err != nil {
		t.Fatal(err)
	}
	raw, _, err := buildArchive(ArchiveConfig{
		DataRoot:     sourceRoot,
		PanelVersion: "test",
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(archive, raw, 0600); err != nil {
		t.Fatal(err)
	}
	restarter := &fakeRestarter{supported: true}
	svc := NewService(ArchiveConfig{DataRoot: dir}, WithRestarter(restarter))

	resp, err := svc.SavePendingRestore(archive, "")
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Pending || !resp.RestartSupported {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if restarter.calls != 1 {
		t.Fatalf("expected one scheduled restart, got %d", restarter.calls)
	}
	if _, err := os.Stat(filepath.Join(pendingDir(dir), "pending.json")); err != nil {
		t.Fatalf("expected pending restore marker: %v", err)
	}
}

type fakeRestarter struct {
	supported bool
	calls     int
}

func (r *fakeRestarter) Supported() bool { return r.supported }
func (r *fakeRestarter) RestartSoon()    { r.calls++ }
