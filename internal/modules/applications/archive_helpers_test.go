package applications

import (
	"archive/zip"
	"bytes"
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
