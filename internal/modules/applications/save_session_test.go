package applications

import (
	"archive/zip"
	"bytes"
	"context"
	"testing"
)

func TestExtractApplicationFileArchiveZip(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("site/index.html")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("<h1>ok</h1>"))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	files, err := extractApplicationFileArchive(bytes.NewReader(buf.Bytes()), int64(buf.Len()), "site.zip")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Name != "site/index.html" || string(files[0].Content) != "<h1>ok</h1>" {
		t.Fatalf("files = %#v", files)
	}
}

func TestUploadSaveSessionArchiveStoresSingleArchiveFile(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	session, err := svc.BeginSaveSession(ctx, BeginSaveSessionInput{Save: SaveInput{
		Name:     "web",
		SpecYAML: "name: web\nimage: nginx\n",
	}})
	if err != nil {
		t.Fatal(err)
	}
	content := testApplicationZipArchive(t, "site/index.html", "<h1>ok</h1>")

	files, err := svc.UploadSaveSessionArchive(ctx, session.ID, FileArchiveInput{
		BasePath: "public",
		Kind:     "binary",
		FileName: "site.zip",
		Content:  content,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path != "public" || files[0].Kind != ApplicationFileKindArchive || files[0].SHA256 == "" {
		t.Fatalf("archive files = %#v", files)
	}
	result, err := svc.CommitSaveSession(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := svc.listFiles(ctx, result.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 1 || stored[0].Path != "public" || stored[0].Kind != ApplicationFileKindArchive || !bytes.Equal(stored[0].Content, content) {
		t.Fatalf("stored files = %#v", stored)
	}
}

func testApplicationZipArchive(t *testing.T, name, content string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	writer, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
