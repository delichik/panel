package keyassets

import (
	"path/filepath"
	"testing"
)

func TestSafeContentDispositionFilename(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"cert.pem", "cert.pem"},
		{"a\"b\nc\x01", "a_b_c_"},
		{"bad\\name", "bad_name"},
		{"..", "download"},
		{"", "download"},
		{"   ", "download"},
	}
	for _, c := range cases {
		if got := safeContentDispositionFilename(c.in); got != c.want {
			t.Fatalf("safeContentDispositionFilename(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestPathWithinDir(t *testing.T) {
	root := filepath.Join(t.TempDir(), "exports")
	inside := filepath.Join(root, "a", "b.panel")
	if !pathWithinDir(root, inside) {
		t.Fatal("path inside export dir should be allowed")
	}
	outside := filepath.Join(t.TempDir(), "outside.panel")
	if pathWithinDir(root, outside) {
		t.Fatal("path outside export dir should be rejected")
	}
	if pathWithinDir(root, "") {
		t.Fatal("empty target should be rejected")
	}
	if pathWithinDir("", inside) {
		t.Fatal("empty root should be rejected")
	}
}
