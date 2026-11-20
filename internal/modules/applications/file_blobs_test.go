package applications

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReplaceApplicationFilesWritesContentLayout(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{Name: "web", Enabled: false, SpecYAML: "name: web\nimage: nginx\n"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	files := []ApplicationFile{{
		Name: "config.txt", Kind: ApplicationFileKindTemplate, ContentType: "text/plain",
		Content: []byte("hello-disk"), Size: 10, SHA256: sha256Hex([]byte("hello-disk")),
		CreatedAt: now, UpdatedAt: now,
	}}
	if _, err := svc.Update(ctx, app.ID, SaveInput{Name: app.Name, Enabled: false, SpecYAML: app.SpecYAML}); err != nil {
		t.Fatal(err)
	}
	stale, err := svc.replaceApplicationFiles(ctx, svc.db, app.ID, files)
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) != 0 {
		t.Fatalf("stale = %#v", stale)
	}

	var fileID, filename, blobPath string
	var contentLen int
	if err := svc.db.QueryRow(`SELECT id, filename, blob_path, COALESCE(length(content),0) FROM application_files WHERE application_id=?`, app.ID).Scan(&fileID, &filename, &blobPath, &contentLen); err != nil {
		t.Fatal(err)
	}
	if filename != "config.txt" || blobPath != "" || contentLen != 0 {
		t.Fatalf("expected content layout metadata, filename=%q blobPath=%q contentLen=%d", filename, blobPath, contentLen)
	}
	raw, err := os.ReadFile(svc.applicationFileContentPath(app.ID, fileID, filename))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "hello-disk" {
		t.Fatalf("content body = %q", raw)
	}

	loaded, err := svc.listFiles(ctx, app.ID, true)
	if err != nil || len(loaded) != 1 || string(loaded[0].Content) != "hello-disk" {
		t.Fatalf("hydrate = %#v err=%v", loaded, err)
	}
}

func TestBackfillApplicationFileBlobsMovesLegacyContent(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{Name: "web", Enabled: false, SpecYAML: "name: web\nimage: nginx\n"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	body := []byte("legacy-blob")
	fileID := "afile_legacy1"
	if _, err := svc.db.Exec(`INSERT INTO application_files(id,application_id,name,kind,content_type,filename,size,sha256,blob_path,content,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		fileID, app.ID, "legacy.txt", ApplicationFileKindTemplate, "text/plain", "", len(body), sha256Hex(body), "", body, now, now); err != nil {
		t.Fatal(err)
	}

	if err := svc.BackfillApplicationFileBlobs(ctx); err != nil {
		t.Fatal(err)
	}
	var filename, blobPath string
	var contentLen int
	if err := svc.db.QueryRow(`SELECT filename, blob_path, COALESCE(length(content),0) FROM application_files WHERE id=?`, fileID).Scan(&filename, &blobPath, &contentLen); err != nil {
		t.Fatal(err)
	}
	if filename != "legacy.txt" || blobPath != "" || contentLen != 0 {
		t.Fatalf("backfill incomplete: filename=%q blobPath=%q contentLen=%d", filename, blobPath, contentLen)
	}
	want := svc.applicationFileContentPath(app.ID, fileID, "legacy.txt")
	raw, err := os.ReadFile(want)
	if err != nil || string(raw) != "legacy-blob" {
		t.Fatalf("backfilled body = %q err=%v", raw, err)
	}

	loaded, err := svc.getFileByName(ctx, app.ID, "legacy.txt", true)
	if err != nil || string(loaded.Content) != "legacy-blob" {
		t.Fatalf("loaded = %#v err=%v", loaded, err)
	}
}

func TestBackfillApplicationFileBlobsMovesFlatBlob(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{Name: "web", Enabled: false, SpecYAML: "name: web\nimage: nginx\n"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	body := []byte("flat-blob")
	fileID := "afile_flat1"
	flat := filepath.Join(svc.applicationFilesDir(app.ID), fileID+".blob")
	if err := os.MkdirAll(filepath.Dir(flat), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(flat, body, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.db.Exec(`INSERT INTO application_files(id,application_id,name,kind,content_type,filename,size,sha256,blob_path,content,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		fileID, app.ID, "flat.txt", ApplicationFileKindTemplate, "text/plain", "", len(body), sha256Hex(body), flat, nil, now, now); err != nil {
		t.Fatal(err)
	}
	if err := svc.BackfillApplicationFileBlobs(ctx); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(svc.applicationFileContentPath(app.ID, fileID, "flat.txt"))
	if err != nil || string(raw) != "flat-blob" {
		t.Fatalf("backfilled flat = %q err=%v", raw, err)
	}
	if _, err := os.Stat(flat); !os.IsNotExist(err) {
		t.Fatalf("legacy flat blob still present: %v", err)
	}
}

func TestDeleteApplicationRemovesFileBlobs(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{Name: "web", Enabled: false, SpecYAML: "name: web\nimage: nginx\n"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err := svc.replaceApplicationFiles(ctx, svc.db, app.ID, []ApplicationFile{{
		Name: "gone.txt", Kind: ApplicationFileKindTemplate, Content: []byte("x"), Size: 1, SHA256: sha256Hex([]byte("x")),
		CreatedAt: now, UpdatedAt: now,
	}}); err != nil {
		t.Fatal(err)
	}
	dir := svc.applicationFilesDir(app.ID)
	if _, err := os.Stat(dir); err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(ctx, app.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("application file dir should be removed, err=%v", err)
	}
}

func TestBeginEditSessionDoesNotCopyCommittedContent(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{Name: "web", Enabled: false, SpecYAML: "name: web\nimage: nginx\n"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err := svc.replaceApplicationFiles(ctx, svc.db, app.ID, []ApplicationFile{{
		Name: "cfg.txt", Kind: ApplicationFileKindTemplate, Content: []byte("committed"), Size: 9, SHA256: sha256Hex([]byte("committed")),
		CreatedAt: now, UpdatedAt: now,
	}}); err != nil {
		t.Fatal(err)
	}
	session, err := svc.BeginEditSession(ctx, "admin", BeginEditSessionInput{ApplicationID: app.ID})
	if err != nil {
		t.Fatal(err)
	}
	var sourceID, blobDir string
	if err := svc.db.QueryRow(`SELECT source_file_id, blob_dir FROM application_edit_session_files WHERE session_id=?`, session.ID).Scan(&sourceID, &blobDir); err != nil {
		t.Fatal(err)
	}
	if sourceID == "" || blobDir != "" {
		t.Fatalf("begin should be zero-copy: source=%q blobDir=%q", sourceID, blobDir)
	}
	filesRoot := filepath.Join(svc.editSessionPath(session.ID), "files")
	entries, _ := os.ReadDir(filesRoot)
	if len(entries) != 0 {
		t.Fatalf("begin copied files into session workspace: %v", entries)
	}
	got, err := svc.GetEditSessionFile(ctx, "admin", session.ID, "cfg.txt")
	if err != nil || string(got.Content) != "committed" {
		t.Fatalf("download fallback = %#v err=%v", got, err)
	}
}

func TestCommitEditSessionRenamesBlobDir(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	session, err := svc.BeginEditSession(ctx, "admin", BeginEditSessionInput{Draft: &SaveInput{Name: "web", SpecYAML: "name: web\nimage: nginx\n"}})
	if err != nil {
		t.Fatal(err)
	}
	session, err = svc.UploadEditSessionBinary(ctx, "admin", session.ID, "cfg.txt", "up-1", EditSessionBinaryInput{
		Revision: session.Revision, ClientOperationID: "op-1", Path: "cfg.txt", FileName: "cfg.txt", Content: []byte("from-session"),
	})
	if err != nil {
		t.Fatal(err)
	}
	var blobDir string
	if err := svc.db.QueryRow(`SELECT blob_dir FROM application_edit_session_files WHERE session_id=?`, session.ID).Scan(&blobDir); err != nil {
		t.Fatal(err)
	}
	validation, err := svc.ValidateEditSession(ctx, "admin", session.ID, session.Revision)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := svc.PreviewEditSession(ctx, "admin", session.ID, session.Revision)
	if err != nil {
		t.Fatal(err)
	}
	_ = validation
	result, err := svc.CommitEditSession(ctx, "admin", session.ID, "commit-1", CommitEditSessionInput{
		Revision: session.Revision, BaseResourceVersion: "0", PreviewToken: preview.Token.Value,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(blobDir); !os.IsNotExist(err) {
		t.Fatalf("session blob_dir should be renamed away: %v", err)
	}
	loaded, err := svc.listFiles(ctx, result.Application.ID, true)
	if err != nil || len(loaded) != 1 || string(loaded[0].Content) != "from-session" {
		t.Fatalf("committed files = %#v err=%v", loaded, err)
	}
	contentPath := svc.applicationFileContentPath(result.Application.ID, loaded[0].ID, "cfg.txt")
	raw, err := os.ReadFile(contentPath)
	if err != nil || string(raw) != "from-session" {
		t.Fatalf("renamed content = %q err=%v path=%s", raw, err, contentPath)
	}
}
