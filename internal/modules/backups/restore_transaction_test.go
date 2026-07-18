package backups

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"panel/internal/platform/config"

	"golang.org/x/crypto/bcrypt"
)

func TestRestoreRejectsDefaultConfigCredentialWhenDatabaseIsUnavailable(t *testing.T) {
	cfg := maintenanceTestConfig(t)
	writeEncryptedPendingRestore(t, cfg.DataRoot, "archive-password")
	hash, err := bcrypt.GenerateFromPassword([]byte("admin"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	cfg.AdminUsername = "admin"
	cfg.AdminPasswordHash = string(hash)
	cfg.AppDatabase = filepath.Join(t.TempDir(), "missing", "app.db")
	if _, err := NewRestoreApp(cfg); err == nil {
		t.Fatal("default admin/admin config must not unlock restore when app.db and pending credential are unavailable")
	}
}

func TestRestoreAllowsExplicitNonDefaultConfigCredentialForLegacyPending(t *testing.T) {
	cfg := maintenanceTestConfig(t)
	writeEncryptedPendingRestore(t, cfg.DataRoot, "archive-password")
	cfg.AppDatabase = filepath.Join(t.TempDir(), "missing", "app.db")
	app, err := NewRestoreApp(cfg)
	if err != nil {
		t.Fatalf("explicit non-default recovery credential should be usable: %v", err)
	}
	if token := loginMaintenance(t, app.Handler(), "admin", "password"); token == "" {
		t.Fatal("explicit recovery credential did not produce a maintenance session")
	}
}

func TestRestoreTransactionPartialFailureRollsBackOriginalData(t *testing.T) {
	cfg := maintenanceTestConfig(t)
	writeTextFile(t, filepath.Join(cfg.DataRoot, "identity.txt"), "old")
	extracted := extractedRestoreFixture(t, "new")
	state, err := prepareRestoreTransaction(cfg, extracted)
	if err != nil {
		t.Fatal(err)
	}
	err = applyRestoreTransaction(cfg.DataRoot, state, func(step string) error {
		if step == "after_swap_0" {
			return errors.New("injected apply failure")
		}
		return nil
	})
	if err == nil {
		t.Fatal("expected injected apply failure")
	}
	if got := readTextFile(t, filepath.Join(cfg.DataRoot, "identity.txt")); got != "old" {
		t.Fatalf("rollback restored %q, want old", got)
	}
	recovered, err := readRestoreTransactionState(cfg.DataRoot)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Phase != "rolled_back" {
		t.Fatalf("transaction phase = %q, want rolled_back", recovered.Phase)
	}
}

func TestRestoreTransactionStagesExternalDatabaseOnTargetVolumeAndRollsBack(t *testing.T) {
	cfg := maintenanceTestConfig(t)
	writeTextFile(t, filepath.Join(cfg.DataRoot, "identity.txt"), "old-root")
	externalDir := t.TempDir()
	cfg.AppDatabase = filepath.Join(externalDir, "app.db")
	writeTextFile(t, cfg.AppDatabase, "old-db")
	extracted := extractedRestoreFixture(t, "new-root")
	writeTextFile(t, filepath.Join(extracted, "databases", "app.db"), "new-db")
	state, err := prepareRestoreTransaction(cfg, extracted)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Targets) != 2 || filepath.Dir(state.Targets[1].StagedPath) != externalDir || filepath.Dir(state.Targets[1].BackupPath) != externalDir {
		t.Fatalf("external target was not staged beside its destination: %+v", state.Targets)
	}
	err = applyRestoreTransaction(cfg.DataRoot, state, func(step string) error {
		if step == "after_swap_1" {
			return errors.New("fail after external swap")
		}
		return nil
	})
	if err == nil {
		t.Fatal("expected injected external swap failure")
	}
	if got := readTextFile(t, filepath.Join(cfg.DataRoot, "identity.txt")); got != "old-root" {
		t.Fatalf("data root rollback = %q", got)
	}
	if got := readTextFile(t, cfg.AppDatabase); got != "old-db" {
		t.Fatalf("external database rollback = %q", got)
	}
}

func TestRestoreStartupRecoversInterruptedSwapAndKeepsMediaRetryable(t *testing.T) {
	cfg := maintenanceTestConfig(t)
	writeTextFile(t, filepath.Join(cfg.DataRoot, "identity.txt"), "old")
	writeEncryptedPendingRestore(t, cfg.DataRoot, "archive-password")
	attachPendingCredential(t, cfg)
	if err := ensureRestoreTransactionMedia(cfg.DataRoot); err != nil {
		t.Fatal(err)
	}
	extracted := extractedRestoreFixture(t, "new")
	state, err := prepareRestoreTransaction(cfg, extracted)
	if err != nil {
		t.Fatal(err)
	}
	if err := applyRestoreTransaction(cfg.DataRoot, state, func(step string) error {
		if step == "after_swap_0" {
			return errSimulatedRestoreCrash
		}
		return nil
	}); !errors.Is(err, errSimulatedRestoreCrash) {
		t.Fatalf("simulated crash error = %v", err)
	}
	if got := readTextFile(t, filepath.Join(cfg.DataRoot, "identity.txt")); got != "new" {
		t.Fatalf("pre-recovery swapped data = %q, want new", got)
	}

	app, err := NewRestoreApp(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got := readTextFile(t, filepath.Join(cfg.DataRoot, "identity.txt")); got != "old" {
		t.Fatalf("startup recovery restored %q, want old", got)
	}
	status := app.currentStatus()
	if status.Phase != PhaseFailed || !status.Retryable || !status.Capabilities.CanRetry || !status.Capabilities.CanClearPending {
		t.Fatalf("recovered status should be retryable after rollback: %+v", status)
	}
	if !pathExists(filepath.Join(restoreTransactionMediaDir(cfg.DataRoot), "pending.json")) || !pathExists(filepath.Join(restoreTransactionMediaDir(cfg.DataRoot), "restore.panel-backup")) {
		t.Fatal("protected restore media was lost during restart recovery")
	}
}

func TestRestoreRollbackFailureBlocksClearAndNormalRestart(t *testing.T) {
	cfg := maintenanceTestConfig(t)
	writeTextFile(t, filepath.Join(cfg.DataRoot, "identity.txt"), "new")
	writeEncryptedPendingRestore(t, cfg.DataRoot, "archive-password")
	attachPendingCredential(t, cfg)
	if err := ensureRestoreTransactionMedia(cfg.DataRoot); err != nil {
		t.Fatal(err)
	}
	state := restoreTransactionState{
		SchemaVersion: restoreTransactionSchemaVersion,
		Phase:         "applying",
		Targets: []restoreSwapTarget{{
			TargetPath: cfg.DataRoot, StagedPath: filepath.Join(restoreTransactionDir(cfg.DataRoot), "missing-stage"),
			BackupPath: filepath.Join(restoreTransactionDir(cfg.DataRoot), "missing-backup"), OriginalExisted: true, State: "swapped",
		}},
	}
	if err := writeRestoreTransactionState(cfg.DataRoot, state); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(restoreTransactionMediaDir(cfg.DataRoot), "pending.json")); err != nil {
		t.Fatal(err)
	}
	app, err := NewRestoreApp(cfg)
	if err != nil {
		t.Fatal(err)
	}
	status := app.currentStatus()
	if status.Phase != PhaseFailed || !status.ClearPendingBlocked || status.Capabilities.CanClearPending || status.Capabilities.CanRetry {
		t.Fatalf("rollback failure must block recovery actions: %+v", status)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/restore/clear-pending", bytes.NewBufferString(`{"expectedRevision":`+jsonNumber(status.Revision)+`}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	app.clearPendingAPI(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("clear-pending during unresolved rollback status = %d", rec.Code)
	}
	if !pathExists(restoreTransactionStatePath(cfg.DataRoot)) || !pathExists(filepath.Join(restoreTransactionMediaDir(cfg.DataRoot), "restore.panel-backup")) {
		t.Fatal("blocked rollback must retain transaction state and remaining protected media")
	}
}

func TestRestoreRecoveryStateForcesMaintenanceEvenWithoutPendingMarker(t *testing.T) {
	cfg := maintenanceTestConfig(t)
	if RestoreRecoveryRequired(cfg.DataRoot) {
		t.Fatal("fresh data root unexpectedly requires recovery")
	}
	state := restoreTransactionState{SchemaVersion: restoreTransactionSchemaVersion, Phase: "rolled_back"}
	if err := writeRestoreTransactionState(cfg.DataRoot, state); err != nil {
		t.Fatal(err)
	}
	if !RestoreRecoveryRequired(cfg.DataRoot) || !PendingRestoreExists(cfg.DataRoot) {
		t.Fatal("transaction state must force restore maintenance independently of pending marker")
	}
}

func TestProtectedMediaWithoutStateForcesRecoveryAndWinsOverNewPending(t *testing.T) {
	cfg := maintenanceTestConfig(t)
	writeEncryptedPendingRestore(t, cfg.DataRoot, "archive-password")
	attachPendingCredential(t, cfg)
	protected, err := readPending(cfg.DataRoot)
	if err != nil {
		t.Fatal(err)
	}
	protected.Manifest.PanelVersion = "protected-media"
	protectedRaw, _ := json.MarshalIndent(protected, "", "  ")
	if err := os.WriteFile(filepath.Join(pendingDir(cfg.DataRoot), "pending.json"), protectedRaw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := ensureRestoreTransactionMedia(cfg.DataRoot); err != nil {
		t.Fatal(err)
	}
	if pathExists(restoreTransactionStatePath(cfg.DataRoot)) {
		t.Fatal("test requires media-moved/pre-state crash window")
	}
	if !RestoreRecoveryRequired(cfg.DataRoot) {
		t.Fatal("protected media without state must force restore recovery")
	}
	newArchive := []byte("new pending")
	newMarker := pendingRestore{
		ArchiveFilename: "new.panel-backup", ArchiveSHA256: digestBytes(newArchive), ArchiveSize: int64(len(newArchive)),
		Manifest:        Manifest{PanelVersion: "new-pending"},
		MaintenanceAuth: &maintenanceCredential{Username: cfg.AdminUsername, PasswordHash: cfg.AdminPasswordHash},
	}
	newMarkerRaw, _ := json.Marshal(newMarker)
	if err := publishPendingRestore(cfg.DataRoot, newMarker.ArchiveFilename, newArchive, newMarkerRaw, nil); err != nil {
		t.Fatal(err)
	}
	resolved, err := readPending(cfg.DataRoot)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Manifest.PanelVersion != "protected-media" {
		t.Fatalf("new pending displaced unresolved protected media: %+v", resolved.Manifest)
	}
}

func TestRestoreRollbackRemovesNewTargetCreatedBeforeStateFlush(t *testing.T) {
	cfg := maintenanceTestConfig(t)
	newTarget := filepath.Join(t.TempDir(), "new-external.db")
	writeTextFile(t, newTarget, "new")
	state := restoreTransactionState{
		SchemaVersion: restoreTransactionSchemaVersion,
		Phase:         "applying",
		Targets: []restoreSwapTarget{{
			TargetPath: newTarget, BackupPath: filepath.Join(restoreTransactionDir(cfg.DataRoot), "missing-backup"),
			OriginalExisted: false, State: "backup_moved",
		}},
	}
	if err := rollbackRestoreTransaction(cfg.DataRoot, state); err != nil {
		t.Fatal(err)
	}
	if pathExists(newTarget) {
		t.Fatal("rollback left a newly created target after interrupted state flush")
	}
}

func TestRestoreRollbackRecoveryRecognizesCompletedRenameBeforeStateFlush(t *testing.T) {
	cfg := maintenanceTestConfig(t)
	target := filepath.Join(cfg.DataRoot, "identity.txt")
	writeTextFile(t, target, "old")
	state := restoreTransactionState{
		SchemaVersion: restoreTransactionSchemaVersion,
		Phase:         "rolling_back",
		Targets: []restoreSwapTarget{{
			TargetPath: target, BackupPath: filepath.Join(restoreTransactionDir(cfg.DataRoot), "already-moved-back"),
			OriginalExisted: true, State: "rollback_renaming",
		}},
	}
	if err := writeRestoreTransactionState(cfg.DataRoot, state); err != nil {
		t.Fatal(err)
	}
	outcome := recoverRestoreTransaction(cfg.DataRoot)
	if !outcome.RolledBack || outcome.RollbackBlocked || outcome.Err != nil {
		t.Fatalf("idempotent rollback recovery outcome = %+v", outcome)
	}
	if got := readTextFile(t, target); got != "old" {
		t.Fatalf("idempotent rollback changed restored original to %q", got)
	}
}

func extractedRestoreFixture(t *testing.T, identity string) string {
	t.Helper()
	root := t.TempDir()
	writeTextFile(t, filepath.Join(root, "dataRoot", "identity.txt"), identity)
	return root
}

func attachPendingCredential(t *testing.T, cfg config.Config) {
	t.Helper()
	marker, err := readPending(cfg.DataRoot)
	if err != nil {
		t.Fatal(err)
	}
	marker.MaintenanceAuth = &maintenanceCredential{Username: cfg.AdminUsername, PasswordHash: cfg.AdminPasswordHash}
	raw, err := json.MarshalIndent(marker, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pendingDir(cfg.DataRoot), "pending.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
}

func writeTextFile(t *testing.T, path, value string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value), 0600); err != nil {
		t.Fatal(err)
	}
}

func readTextFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func jsonNumber(value uint64) string {
	raw, _ := json.Marshal(value)
	return string(raw)
}
