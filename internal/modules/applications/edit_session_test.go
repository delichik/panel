package applications

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"panel/internal/modules/tasks"
	panelerr "panel/internal/platform/errors"
)

func TestApplicationArchiveRejectsEmptyDirectoryFlood(t *testing.T) {
	var raw bytes.Buffer
	zw := zip.NewWriter(&raw)
	for i := 0; i <= applicationArchiveMaxFiles; i++ {
		if _, err := zw.CreateHeader(&zip.FileHeader{Name: fmt.Sprintf("d%05d/", i), Method: zip.Store}); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(raw.Bytes()), int64(raw.Len()))
	if err != nil {
		t.Fatal(err)
	}
	_, err = extractApplicationZip(zr, int64(raw.Len()))
	assertPanelErrorCode(t, err, "application_file_archive_limits_exceeded")
}

func TestApplicationTarRejectsEmptyDirectoryFlood(t *testing.T) {
	var raw bytes.Buffer
	tw := tar.NewWriter(&raw)
	for i := 0; i <= applicationArchiveMaxFiles; i++ {
		if err := tw.WriteHeader(&tar.Header{Name: fmt.Sprintf("d%05d/", i), Typeflag: tar.TypeDir, Mode: 0o755}); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	_, err := extractApplicationTar(tar.NewReader(bytes.NewReader(raw.Bytes())), int64(raw.Len()))
	assertPanelErrorCode(t, err, "application_file_archive_limits_exceeded")
}

func TestApplicationTarRejectsIgnoredHeaderFlood(t *testing.T) {
	var raw bytes.Buffer
	tw := tar.NewWriter(&raw)
	for i := 0; i <= applicationArchiveMaxFiles; i++ {
		if err := tw.WriteHeader(&tar.Header{Name: fmt.Sprintf("l%05d", i), Typeflag: tar.TypeSymlink, Linkname: "target", Mode: 0o777}); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	_, err := extractApplicationTar(tar.NewReader(bytes.NewReader(raw.Bytes())), int64(raw.Len()))
	assertPanelErrorCode(t, err, "application_file_archive_limits_exceeded")
}

func TestApplicationEditSessionPersistsAndRecovers(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	session, err := svc.BeginEditSession(ctx, "admin", BeginEditSessionInput{ClientDraftKey: "new-web", Draft: &SaveInput{Name: "web", SpecYAML: "name: web\nimage: nginx\n"}})
	if err != nil {
		t.Fatal(err)
	}
	if session.State != EditSessionStateActive || session.Revision != 1 {
		t.Fatalf("session = %#v", session)
	}
	session, err = svc.PatchEditSession(ctx, "admin", session.ID, PatchEditSessionInput{Revision: session.Revision, Draft: SaveInput{Name: "web-2", SpecYAML: "name: web-2\nimage: nginx\n"}})
	if err != nil {
		t.Fatal(err)
	}
	if session.Revision != 2 || session.Draft.Name != "web-2" {
		t.Fatalf("patched session = %#v", session)
	}
	session, err = svc.PutEditSessionFile(ctx, "admin", session.ID, "file-config", "file-op-1", EditSessionFileInput{
		Revision:          session.Revision,
		ClientOperationID: "file-op-1",
		Path:              "config-app.conf",
		Kind:              ApplicationFileKindTemplate,
		ContentBase64:     base64.StdEncoding.EncodeToString([]byte("hello")),
	})
	if err != nil {
		t.Fatal(err)
	}
	if session.Revision != 3 || len(session.Files) != 1 || session.Files[0].SHA256 == "" {
		t.Fatalf("session files = %#v", session)
	}
	if err := svc.DiscardEditSession(ctx, "admin", session.ID); err != nil {
		t.Fatal(err)
	}
	discarded, err := svc.GetEditSession(ctx, "admin", session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if discarded.State != EditSessionStateDiscarded {
		t.Fatalf("discarded state = %q", discarded.State)
	}
}

func TestApplicationEditSessionReadsFileWithoutChangingRevision(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	session, err := svc.BeginEditSession(ctx, "admin", BeginEditSessionInput{Draft: &SaveInput{Name: "web", SpecYAML: "name: web\nimage: nginx\n"}})
	if err != nil {
		t.Fatal(err)
	}
	session, err = svc.PutEditSessionFile(ctx, "admin", session.ID, "file-config", "file-read-1", EditSessionFileInput{
		Revision: session.Revision, ClientOperationID: "file-read-1", Path: "config-app.conf", Kind: ApplicationFileKindTemplate,
		ContentType: "text/plain", ContentBase64: base64.StdEncoding.EncodeToString([]byte("hello {{ name }}\n")),
	})
	if err != nil {
		t.Fatal(err)
	}
	session, err = svc.PutEditSessionFile(ctx, "admin", session.ID, "file-empty", "file-read-empty", EditSessionFileInput{
		Revision: session.Revision, ClientOperationID: "file-read-empty", Path: "config-empty.conf", Kind: ApplicationFileKindTemplate,
		ContentType: "text/plain", ContentBase64: base64.StdEncoding.EncodeToString(nil),
	})
	if err != nil {
		t.Fatal(err)
	}
	revision := session.Revision
	file, err := svc.GetEditSessionFile(ctx, "admin", session.ID, "file-config")
	if err != nil {
		t.Fatal(err)
	}
	if file.ContentBase64 != base64.StdEncoding.EncodeToString([]byte("hello {{ name }}\n")) || file.Path != "config-app.conf" || file.Kind != ApplicationFileKindTemplate {
		t.Fatalf("file = %#v", file)
	}
	refreshed, err := svc.GetEditSession(ctx, "admin", session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.Revision != revision {
		t.Fatalf("read changed revision: before=%d after=%d", revision, refreshed.Revision)
	}
	empty, err := svc.GetEditSessionFile(ctx, "admin", session.ID, "file-empty")
	if err != nil {
		t.Fatal(err)
	}
	if empty.ContentBase64 != "" || empty.Size != 0 {
		t.Fatalf("empty file = %#v", empty)
	}
	_, err = svc.GetEditSessionFile(ctx, "other-owner", session.ID, "file-config")
	assertPanelErrorCode(t, err, "not_found")
}

func TestApplicationEditSessionReadRejectsBlobMetadataMismatch(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	session, err := svc.BeginEditSession(ctx, "admin", BeginEditSessionInput{Draft: &SaveInput{Name: "web", SpecYAML: "name: web\nimage: nginx\n"}})
	if err != nil {
		t.Fatal(err)
	}
	session, err = svc.PutEditSessionFile(ctx, "admin", session.ID, "file-config", "file-integrity-1", EditSessionFileInput{
		Revision: session.Revision, ClientOperationID: "file-integrity-1", Path: "config-app.conf", Kind: ApplicationFileKindTemplate,
		ContentBase64: base64.StdEncoding.EncodeToString([]byte("content")),
	})
	if err != nil {
		t.Fatal(err)
	}
	file := session.Files[0]
	if _, err := svc.db.Exec(`UPDATE application_edit_session_files SET size=? WHERE session_id=? AND file_key=?`, file.Size+1, session.ID, file.FileKey); err != nil {
		t.Fatal(err)
	}
	_, err = svc.GetEditSessionFile(ctx, "admin", session.ID, file.FileKey)
	assertPanelErrorCode(t, err, "application_edit_file_size_mismatch")
	if _, err := svc.db.Exec(`UPDATE application_edit_session_files SET size=?,sha256=? WHERE session_id=? AND file_key=?`, file.Size, "invalid", session.ID, file.FileKey); err != nil {
		t.Fatal(err)
	}
	_, err = svc.GetEditSessionFile(ctx, "admin", session.ID, file.FileKey)
	assertPanelErrorCode(t, err, "application_edit_file_hash_mismatch")
}

func TestApplicationEditSessionRevisionAndIdempotency(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	session, err := svc.BeginEditSession(ctx, "admin", BeginEditSessionInput{Draft: &SaveInput{Name: "web", SpecYAML: "name: web\nimage: nginx\n"}})
	if err != nil {
		t.Fatal(err)
	}
	input := EditSessionBinaryInput{Revision: session.Revision, ClientOperationID: "same-op", Path: "a.txt", FileName: "a.txt", Content: []byte("one")}
	first, err := svc.UploadEditSessionBinary(ctx, "admin", session.ID, "file-a", "same-key", input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.UploadEditSessionBinary(ctx, "admin", session.ID, "file-a", "same-key", input)
	if err != nil {
		t.Fatal(err)
	}
	if second.Revision != first.Revision {
		t.Fatalf("idempotent revision changed: first=%d second=%d", first.Revision, second.Revision)
	}
	_, err = svc.PatchEditSession(ctx, "admin", session.ID, PatchEditSessionInput{Revision: 1, Draft: session.Draft})
	assertPanelErrorCode(t, err, "edit_session_revision_conflict")
	changed := input
	changed.Content = []byte("two")
	_, err = svc.UploadEditSessionBinary(ctx, "admin", session.ID, "file-a", "same-key", changed)
	assertPanelErrorCode(t, err, "idempotency_key_reused")
}

func TestApplicationEditSessionBinaryRequiresMultipartPath(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	session, err := svc.BeginEditSession(ctx, "admin", BeginEditSessionInput{Draft: &SaveInput{Name: "web", SpecYAML: "name: web\nimage: nginx\n"}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.PutEditSessionFile(ctx, "admin", session.ID, "binary", "json-binary", EditSessionFileInput{Revision: session.Revision, ClientOperationID: "json-binary", Path: "asset.png", Kind: ApplicationFileKindBinary, ContentBase64: base64.StdEncoding.EncodeToString([]byte("png"))})
	assertPanelErrorCode(t, err, "application_file_kind_invalid")

	updated, err := svc.UploadEditSessionBinary(ctx, "admin", session.ID, "binary", "multipart-binary", EditSessionBinaryInput{Revision: session.Revision, ClientOperationID: "multipart-binary", Path: "asset.png", FileName: "asset.png", Content: []byte("png")})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Files) != 1 || updated.Files[0].FileKey != "binary" || updated.Files[0].Kind != ApplicationFileKindBinary || updated.Files[0].ContentType != "image/png" {
		t.Fatalf("binary file = %#v", updated.Files)
	}
}

func TestApplicationArchiveLimitsRejectCountDepthSizeAndRatio(t *testing.T) {
	for _, tc := range []struct {
		name       string
		path       string
		count      int
		extracted  int64
		compressed int64
	}{
		{name: "count", path: "a", count: applicationArchiveMaxFiles + 1, extracted: 1, compressed: 1},
		{name: "depth", path: strings.Repeat("a/", applicationArchiveMaxDepth) + "x", count: 1, extracted: 1, compressed: 1},
		{name: "size", path: "a", count: 1, extracted: applicationArchiveMaxExtracted + 1, compressed: applicationArchiveMaxExtracted},
		{name: "ratio", path: "a", count: 1, extracted: 2 << 20, compressed: 1024},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertPanelErrorCode(t, validateApplicationArchiveLimits(tc.path, tc.count, tc.extracted, tc.compressed), "application_file_archive_limits_exceeded")
		})
	}
}

func TestApplicationEditSessionPreviewAndCommit(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	session, err := svc.BeginEditSession(ctx, "admin", BeginEditSessionInput{Draft: &SaveInput{Name: "web", SpecYAML: "name: web\nimage: nginx\n"}})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := svc.PreviewEditSession(ctx, "admin", session.ID, session.Revision)
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.CommitEditSession(ctx, "admin", session.ID, "commit-1", CommitEditSessionInput{Revision: session.Revision, BaseResourceVersion: "0", PreviewToken: preview.Token.Value})
	if err != nil {
		t.Fatal(err)
	}
	if result.Application.ID == "" || result.Application.Version != 1 || result.ApplyRequested {
		t.Fatalf("commit result = %#v", result)
	}
	repeated, err := svc.CommitEditSession(ctx, "admin", session.ID, "commit-1", CommitEditSessionInput{Revision: session.Revision, BaseResourceVersion: "0", PreviewToken: preview.Token.Value})
	if err != nil {
		t.Fatal(err)
	}
	if repeated.Application.ID != result.Application.ID {
		t.Fatalf("commit was not idempotent: %#v %#v", result, repeated)
	}
}

func TestApplicationEditSessionCommitRejectsChangedBase(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	app, err := svc.Create(ctx, SaveInput{Name: "web", SpecYAML: "name: web\nimage: nginx\n"})
	if err != nil {
		t.Fatal(err)
	}
	committedDraft := SaveInput{Name: "web-committed", SpecYAML: "name: web-committed\nimage: nginx\n"}
	session, err := svc.BeginEditSession(ctx, "admin", BeginEditSessionInput{ApplicationID: app.ID, Draft: &committedDraft})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Update(ctx, app.ID, SaveInput{Name: "changed", SpecYAML: "name: changed\nimage: nginx\n"}); err != nil {
		t.Fatal(err)
	}
	preview, err := svc.PreviewEditSession(ctx, "admin", session.ID, session.Revision)
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.CommitEditSession(ctx, "admin", session.ID, "commit-conflict", CommitEditSessionInput{Revision: session.Revision, BaseResourceVersion: session.BaseResourceVersion.Value, PreviewToken: preview.Token.Value})
	assertPanelErrorCode(t, err, "resource_version_conflict")
}

func TestApplicationEditSessionConcurrentCommitsUseConfigurationVersionCAS(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	app, err := svc.Create(ctx, SaveInput{Name: "web", SpecYAML: "name: web\nimage: nginx\n"})
	if err != nil {
		t.Fatal(err)
	}
	type candidate struct {
		session ApplicationEditSession
		preview EditSessionPreviewResult
		name    string
	}
	candidates := make([]candidate, 2)
	for i, name := range []string{"web-a", "web-b"} {
		draft := SaveInput{Name: name, SpecYAML: "name: " + name + "\nimage: nginx\n"}
		session, err := svc.BeginEditSession(ctx, "admin", BeginEditSessionInput{ApplicationID: app.ID, Draft: &draft})
		if err != nil {
			t.Fatal(err)
		}
		preview, err := svc.PreviewEditSession(ctx, "admin", session.ID, session.Revision)
		if err != nil {
			t.Fatal(err)
		}
		candidates[i] = candidate{session: session, preview: preview, name: name}
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, item := range candidates {
		go func(item candidate) {
			<-start
			_, err := svc.CommitEditSession(ctx, "admin", item.session.ID, "commit-cas-"+item.name, CommitEditSessionInput{Revision: item.session.Revision, BaseResourceVersion: item.session.BaseResourceVersion.Value, PreviewToken: item.preview.Token.Value})
			results <- err
		}(item)
	}
	close(start)
	successes, conflicts := 0, 0
	for range candidates {
		err := <-results
		if err == nil {
			successes++
		} else if panelErrorCode(err) == "resource_version_conflict" {
			conflicts++
		} else {
			t.Fatalf("unexpected commit error: %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes=%d conflicts=%d", successes, conflicts)
	}
	got, err := svc.Get(ctx, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != app.Version+1 {
		t.Fatalf("version = %d, want %d", got.Version, app.Version+1)
	}
}

func TestApplicationUpdateIdenticalConfigurationDoesNotIncrementVersion(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	app, err := svc.Create(ctx, SaveInput{Name: "web", SpecYAML: "name: web\nimage: nginx\n"})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := svc.Update(ctx, app.ID, saveInputFromApplication(app))
	if err != nil {
		t.Fatal(err)
	}
	if updated.Version != app.Version {
		t.Fatalf("version changed for identical configuration: before=%d after=%d", app.Version, updated.Version)
	}
}

func TestApplicationEditSessionFileConflictPreservesCurrentBlob(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	session, err := svc.BeginEditSession(ctx, "admin", BeginEditSessionInput{Draft: &SaveInput{Name: "web", SpecYAML: "name: web\nimage: nginx\n"}})
	if err != nil {
		t.Fatal(err)
	}
	firstInput := EditSessionBinaryInput{Revision: session.Revision, ClientOperationID: "file-1", Path: "a.txt", FileName: "a.txt", Content: []byte("one")}
	session, err = svc.UploadEditSessionBinary(ctx, "admin", session.ID, "file-a", "file-key-1", firstInput)
	if err != nil {
		t.Fatal(err)
	}
	var firstBlob string
	if err := svc.db.QueryRow(`SELECT blob_path FROM application_edit_session_files WHERE session_id=? AND file_key=?`, session.ID, "file-a").Scan(&firstBlob); err != nil {
		t.Fatal(err)
	}
	stale := firstInput
	stale.ClientOperationID = "file-stale"
	stale.Content = []byte("stale")
	_, err = svc.UploadEditSessionBinary(ctx, "admin", session.ID, "file-a", "file-key-stale", stale)
	assertPanelErrorCode(t, err, "edit_session_revision_conflict")
	content, err := os.ReadFile(firstBlob)
	if err != nil || string(content) != "one" {
		t.Fatalf("current blob changed after conflict: content=%q err=%v", content, err)
	}
	entries, err := os.ReadDir(svc.editSessionPath(session.ID))
	if err != nil || len(entries) != 1 {
		t.Fatalf("failed blob was not cleaned: entries=%v err=%v", entries, err)
	}
	pathConflict := EditSessionBinaryInput{Revision: session.Revision, ClientOperationID: "file-path-conflict", Path: "a.txt", FileName: "a.txt", Content: []byte("conflict")}
	if _, err := svc.UploadEditSessionBinary(ctx, "admin", session.ID, "file-b", "file-key-path-conflict", pathConflict); err == nil {
		t.Fatal("expected duplicate path conflict")
	}
	afterConflict, err := svc.GetEditSession(ctx, "admin", session.ID)
	if err != nil || afterConflict.Revision != session.Revision || len(afterConflict.Files) != 1 {
		t.Fatalf("database conflict changed session: session=%#v err=%v", afterConflict, err)
	}
	entries, err = os.ReadDir(svc.editSessionPath(session.ID))
	if err != nil || len(entries) != 1 {
		t.Fatalf("database-conflict blob was not cleaned: entries=%v err=%v", entries, err)
	}

	replacement := EditSessionBinaryInput{Revision: session.Revision, ClientOperationID: "file-2", Path: "a.txt", FileName: "a.txt", Content: []byte("two")}
	if _, err := svc.UploadEditSessionBinary(ctx, "admin", session.ID, "file-a", "file-key-2", replacement); err != nil {
		t.Fatal(err)
	}
	var secondBlob string
	if err := svc.db.QueryRow(`SELECT blob_path FROM application_edit_session_files WHERE session_id=? AND file_key=?`, session.ID, "file-a").Scan(&secondBlob); err != nil {
		t.Fatal(err)
	}
	if secondBlob == firstBlob {
		t.Fatal("replacement reused the old blob path")
	}
	if _, err := os.Stat(firstBlob); !os.IsNotExist(err) {
		t.Fatalf("old blob still exists after committed replacement: %v", err)
	}
}

func TestApplicationEditSessionCleanupNeverRemovesCommittingWorkspace(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	session, err := svc.BeginEditSession(ctx, "admin", BeginEditSessionInput{Draft: &SaveInput{Name: "web", SpecYAML: "name: web\nimage: nginx\n"}})
	if err != nil {
		t.Fatal(err)
	}
	old := formatTime(time.Now().UTC().Add(-time.Hour))
	futureLease := formatTime(time.Now().UTC().Add(time.Hour))
	if _, err := svc.db.Exec(`UPDATE application_edit_sessions SET state=?,idle_expires_at=?,absolute_expires_at=?,commit_lease_expires_at=? WHERE id=?`, EditSessionStateCommitting, old, old, futureLease, session.ID); err != nil {
		t.Fatal(err)
	}
	svc.cleanupEditSessions(time.Now().UTC())
	var state string
	if err := svc.db.QueryRow(`SELECT state FROM application_edit_sessions WHERE id=?`, session.ID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != EditSessionStateCommitting {
		t.Fatalf("state = %q", state)
	}
	if _, err := os.Stat(svc.editSessionPath(session.ID)); err != nil {
		t.Fatalf("committing workspace was removed: %v", err)
	}
}

func TestApplicationEditSessionCleanupRecoversExpiredCommitWithoutGet(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	app, err := svc.Create(ctx, SaveInput{Name: "web", SpecYAML: "name: web\nimage: nginx\n"})
	if err != nil {
		t.Fatal(err)
	}
	committedDraft := SaveInput{Name: "web-cleanup", SpecYAML: "name: web-cleanup\nimage: nginx\n"}
	session, err := svc.BeginEditSession(ctx, "admin", BeginEditSessionInput{ApplicationID: app.ID, Draft: &committedDraft})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Update(ctx, app.ID, session.Draft); err != nil {
		t.Fatal(err)
	}
	expiredLease := formatTime(time.Now().UTC().Add(-time.Minute))
	if _, err := svc.db.Exec(`UPDATE application_edit_sessions SET state=?,commit_application_id=?,commit_lease_owner='lost',commit_lease_expires_at=? WHERE id=?`, EditSessionStateCommitting, app.ID, expiredLease, session.ID); err != nil {
		t.Fatal(err)
	}
	svc.cleanupEditSessions(time.Now().UTC())
	var state string
	if err := svc.db.QueryRow(`SELECT state FROM application_edit_sessions WHERE id=?`, session.ID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != EditSessionStateCommitted {
		t.Fatalf("cleanup recovery state = %q", state)
	}
}

func TestApplicationEditSessionCleanupReleasesUncommittedExpiredLeaseThenExpires(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	session, err := svc.BeginEditSession(ctx, "admin", BeginEditSessionInput{Draft: &SaveInput{Name: "web", SpecYAML: "name: web\nimage: nginx\n"}})
	if err != nil {
		t.Fatal(err)
	}
	old := formatTime(time.Now().UTC().Add(-time.Hour))
	if _, err := svc.db.Exec(`UPDATE application_edit_sessions SET state=?,commit_application_id='app_missing',commit_lease_owner='lost',commit_lease_expires_at=?,idle_expires_at=?,absolute_expires_at=? WHERE id=?`, EditSessionStateCommitting, old, old, old, session.ID); err != nil {
		t.Fatal(err)
	}
	svc.cleanupEditSessions(time.Now().UTC())
	var state string
	if err := svc.db.QueryRow(`SELECT state FROM application_edit_sessions WHERE id=?`, session.ID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != EditSessionStateExpired {
		t.Fatalf("cleanup state = %q", state)
	}
	if _, err := os.Stat(svc.editSessionPath(session.ID)); !os.IsNotExist(err) {
		t.Fatalf("expired workspace remains: %v", err)
	}
}

func TestApplicationEditSessionCleanupPreservesFreshCreatingWorkspace(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	now := time.Now().UTC()
	dir := svc.editSessionPath("aedit_creating")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	blob := filepath.Join(dir, "new.blob")
	if err := os.WriteFile(blob, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Directory mtimes are not a reliable proxy for an in-progress writer: an
	// existing file can be freshly written while the directory itself is old.
	old := now.Add(-2 * editSessionOrphanStaleAfter)
	if err := os.Chtimes(dir, old, old); err != nil {
		t.Fatal(err)
	}
	svc.cleanupEditSessions(now)
	if _, err := os.Stat(blob); err != nil {
		t.Fatalf("fresh creating workspace was removed: %v", err)
	}
}

func TestApplicationEditSessionCleanupDeletesOnlyStaleOrphans(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	now := time.Now().UTC()
	dir := svc.editSessionPath("aedit_stale")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	blob := filepath.Join(dir, "old.blob")
	if err := os.WriteFile(blob, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := now.Add(-2 * editSessionOrphanStaleAfter)
	if err := os.Chtimes(blob, old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(dir, old, old); err != nil {
		t.Fatal(err)
	}
	svc.cleanupEditSessions(now)
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("stale orphan workspace remains: %v", err)
	}
}

func TestApplicationEditSessionCleanupAgesUnreferencedBlobs(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	session, err := svc.BeginEditSession(ctx, "admin", BeginEditSessionInput{Draft: &SaveInput{Name: "web", SpecYAML: "name: web\nimage: nginx\n"}})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	blob := filepath.Join(svc.editSessionPath(session.ID), "unreferenced.blob")
	if err := os.WriteFile(blob, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	svc.cleanupEditSessions(now)
	if _, err := os.Stat(blob); err != nil {
		t.Fatalf("fresh unreferenced blob was removed: %v", err)
	}
	old := now.Add(-2 * editSessionOrphanStaleAfter)
	if err := os.Chtimes(blob, old, old); err != nil {
		t.Fatal(err)
	}
	svc.cleanupEditSessions(now)
	if _, err := os.Stat(blob); !os.IsNotExist(err) {
		t.Fatalf("stale unreferenced blob remains: %v", err)
	}
}

func TestApplicationEditSessionCommitSeparatesPersistedConfigFromApplyFailure(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(*Service)
		draft SaveInput
	}{
		{name: "proxy", setup: func(s *Service) { s.proxyReconciler = failingEditProxyReconciler{} }, draft: SaveInput{Name: "web", SpecYAML: "name: web\nimage: nginx\n"}},
		{name: "reconcile", setup: func(s *Service) { s.reconcileTrigger = failingEditReconcileTrigger{} }, draft: SaveInput{Name: "web", Enabled: true, SpecYAML: "name: web\nimage: nginx\n"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, _, _, closeStore := newTestService(t)
			defer closeStore()
			tc.setup(svc)
			ctx := context.Background()
			session, err := svc.BeginEditSession(ctx, "admin", BeginEditSessionInput{Draft: &tc.draft})
			if err != nil {
				t.Fatal(err)
			}
			preview, err := svc.PreviewEditSession(ctx, "admin", session.ID, session.Revision)
			if err != nil {
				t.Fatal(err)
			}
			result, err := svc.CommitEditSession(ctx, "admin", session.ID, "commit-failure", CommitEditSessionInput{Revision: session.Revision, BaseResourceVersion: "0", PreviewToken: preview.Token.Value})
			if err != nil {
				t.Fatal(err)
			}
			if result.Application.ID == "" || len(result.Diagnostics) == 0 || result.Diagnostics[0].Code != "application_apply_request_failed" {
				t.Fatalf("result = %#v", result)
			}
			stored, err := svc.GetEditSession(ctx, "admin", session.ID)
			if err != nil || stored.State != EditSessionStateCommitted {
				t.Fatalf("session = %#v err=%v", stored, err)
			}
		})
	}
}

func TestApplicationEditSessionRecoversPersistedCommitAfterLeaseExpiry(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	app, err := svc.Create(ctx, SaveInput{Name: "web", SpecYAML: "name: web\nimage: nginx\n"})
	if err != nil {
		t.Fatal(err)
	}
	committedDraft := SaveInput{Name: "web-recovered", SpecYAML: "name: web-recovered\nimage: nginx\n"}
	session, err := svc.BeginEditSession(ctx, "admin", BeginEditSessionInput{ApplicationID: app.ID, Draft: &committedDraft})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Update(ctx, app.ID, session.Draft); err != nil {
		t.Fatal(err)
	}
	expired := formatTime(time.Now().UTC().Add(-time.Minute))
	if _, err := svc.db.Exec(`UPDATE application_edit_sessions SET state=?,commit_application_id=?,commit_idempotency_key=?,commit_lease_owner='lost',commit_lease_expires_at=? WHERE id=?`, EditSessionStateCommitting, app.ID, "commit-recover", expired, session.ID); err != nil {
		t.Fatal(err)
	}
	recovered, err := svc.GetEditSession(ctx, "admin", session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.State != EditSessionStateCommitted || recovered.CommitResult == nil {
		t.Fatalf("recovered = %#v", recovered)
	}
}

func TestApplicationEditSessionAmbiguousPersistedCommitDoesNotReturnActive(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	app, err := svc.Create(ctx, SaveInput{Name: "web", SpecYAML: "name: web\nimage: nginx\n"})
	if err != nil {
		t.Fatal(err)
	}
	committedDraft := SaveInput{Name: "web-first", SpecYAML: "name: web-first\nimage: nginx\n"}
	session, err := svc.BeginEditSession(ctx, "admin", BeginEditSessionInput{ApplicationID: app.ID, Draft: &committedDraft})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Update(ctx, app.ID, session.Draft); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Update(ctx, app.ID, SaveInput{Name: "newer", SpecYAML: "name: newer\nimage: nginx\n"}); err != nil {
		t.Fatal(err)
	}
	expired := formatTime(time.Now().UTC().Add(-time.Minute))
	if _, err := svc.db.Exec(`UPDATE application_edit_sessions SET state=?,commit_application_id=?,commit_idempotency_key=?,commit_lease_owner='lost',commit_lease_expires_at=? WHERE id=?`, EditSessionStateCommitting, app.ID, "commit-ambiguous", expired, session.ID); err != nil {
		t.Fatal(err)
	}
	recovered, err := svc.GetEditSession(ctx, "admin", session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.State != EditSessionStateConflict {
		t.Fatalf("ambiguous commit returned to %q", recovered.State)
	}
}

func TestApplicationEditSessionBaseTimestampIsSnapshot(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	app, err := svc.Create(ctx, SaveInput{Name: "web", SpecYAML: "name: web\nimage: nginx\n"})
	if err != nil {
		t.Fatal(err)
	}
	session, err := svc.BeginEditSession(ctx, "admin", BeginEditSessionInput{ApplicationID: app.ID})
	if err != nil {
		t.Fatal(err)
	}
	base := session.BaseResourceVersion.UpdatedAt
	if base.IsZero() {
		t.Fatal("base timestamp is missing")
	}
	changedAt := time.Now().UTC().Add(time.Hour)
	if _, err := svc.db.Exec(`UPDATE applications SET updated_at=? WHERE id=?`, formatTime(changedAt), app.ID); err != nil {
		t.Fatal(err)
	}
	reloaded, err := svc.GetEditSession(ctx, "admin", session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reloaded.BaseResourceVersion.UpdatedAt.Equal(base) {
		t.Fatalf("base timestamp changed: before=%v after=%v", base, reloaded.BaseResourceVersion.UpdatedAt)
	}
}

func TestApplicationDerivedRefreshDoesNotChangeResourceVersion(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	app, err := svc.Create(ctx, SaveInput{Name: "web", SpecYAML: "name: web\nimage: nginx\n"})
	if err != nil {
		t.Fatal(err)
	}
	version, updatedAt := app.Version, app.UpdatedAt
	app.ImageLatestDigest = "sha256:new"
	now := time.Now().UTC()
	app.ImageCheckedAt = &now
	app.ImageUpdateAvailable = true
	if err := svc.updateApplicationDerived(ctx, app); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Get(ctx, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != version || !got.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("resource version changed after derived refresh: before=(%d,%v) after=(%d,%v)", version, updatedAt, got.Version, got.UpdatedAt)
	}
}

type failingEditProxyReconciler struct{}

func (failingEditProxyReconciler) ReconcileReverseProxy(context.Context) error {
	return errors.New("proxy unavailable")
}

type failingEditReconcileTrigger struct{}

func (failingEditReconcileTrigger) TriggerApplicationReconcile(context.Context, tasks.PeriodicTrigger) (tasks.Task, bool, error) {
	return tasks.Task{}, false, errors.New("reconcile unavailable")
}

func assertPanelErrorCode(t *testing.T, err error, code string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected %s error", code)
	}
	var target *panelerr.Error
	if !errors.As(err, &target) || target.Code != code {
		t.Fatalf("error = %#v, want code %q", err, code)
	}
}

func TestApplicationEditSessionSameNamedNewFileAcrossApplications(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	commitWithFile := func(name string) string {
		t.Helper()
		session, err := svc.BeginEditSession(ctx, "admin", BeginEditSessionInput{Draft: &SaveInput{Name: name, SpecYAML: "name: " + name + "\nimage: nginx\n"}})
		if err != nil {
			t.Fatal(err)
		}
		session, err = svc.PutEditSessionFile(ctx, "admin", session.ID, "shared.conf", "file-"+name, EditSessionFileInput{
			Revision:          session.Revision,
			ClientOperationID: "file-" + name,
			Name:              "shared.conf",
			Kind:              ApplicationFileKindTemplate,
			ContentBase64:     base64.StdEncoding.EncodeToString([]byte("server {}")),
		})
		if err != nil {
			t.Fatal(err)
		}
		preview, err := svc.PreviewEditSession(ctx, "admin", session.ID, session.Revision)
		if err != nil {
			t.Fatal(err)
		}
		result, err := svc.CommitEditSession(ctx, "admin", session.ID, "commit-"+name, CommitEditSessionInput{Revision: session.Revision, BaseResourceVersion: "0", PreviewToken: preview.Token.Value})
		if err != nil {
			t.Fatal(err)
		}
		return result.Application.ID
	}

	appA := commitWithFile("web-a")
	appB := commitWithFile("web-b")

	filesA, err := svc.ListFiles(ctx, appA)
	if err != nil {
		t.Fatal(err)
	}
	filesB, err := svc.ListFiles(ctx, appB)
	if err != nil {
		t.Fatal(err)
	}
	if len(filesA) != 1 || filesA[0].Name != "shared.conf" {
		t.Fatalf("filesA = %#v", filesA)
	}
	if len(filesB) != 1 || filesB[0].Name != "shared.conf" {
		t.Fatalf("filesB = %#v", filesB)
	}
	if filesA[0].ID == "" || filesA[0].ID == filesB[0].ID {
		t.Fatalf("expected distinct non-empty global file ids: %q vs %q", filesA[0].ID, filesB[0].ID)
	}
}
