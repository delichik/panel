package storage

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestValidPath(t *testing.T) {
	cases := []struct {
		value string
		want  bool
	}{
		{"/srv/panel-storage", true},
		{"/srv/panel-storage/srv-a/srv-b/app-1", true},
		{"/", false},
		{"relative", false},
		{"/srv/../etc", false},
		{"/srv/./data", false},
		{"/srv//data", false},
		{"/srv/data/", false},
		{"/srv/data\nrm -rf /", false},
		{"/", false},
	}
	for _, tc := range cases {
		if got := validPath(tc.value); got != tc.want {
			t.Errorf("validPath(%q) = %v, want %v", tc.value, got, tc.want)
		}
	}
}

func TestCleanHosts(t *testing.T) {
	hosts := cleanHosts([]string{"10.0.0.6", "10.0.0.5", "10.0.0.6", "bad host", "", "node-a.example.com"})
	want := []string{"10.0.0.5", "10.0.0.6", "node-a.example.com"}
	if len(hosts) != len(want) {
		t.Fatalf("cleanHosts = %#v, want %#v", hosts, want)
	}
	for i := range want {
		if hosts[i] != want[i] {
			t.Fatalf("cleanHosts = %#v, want %#v", hosts, want)
		}
	}
}

func TestExportsBlockAndStrip(t *testing.T) {
	block := exportsBlock("/srv/panel-storage", []string{"10.0.0.5", "10.0.0.6"})
	expect := "# panel-storage-share:managed\n/srv/panel-storage 10.0.0.5(rw,sync,no_subtree_check,no_root_squash,insecure) 10.0.0.6(rw,sync,no_subtree_check,no_root_squash,insecure)\n"
	if block != expect {
		t.Fatalf("exportsBlock mismatch:\n got %q\nwant %q", block, expect)
	}
	existing := "/other 1.2.3.4(rw)\n" + block + "/tail 5.6.7.8(rw)\n"
	cleaned := stripExports(existing, "/srv/panel-storage")
	want := "/other 1.2.3.4(rw)\n/tail 5.6.7.8(rw)"
	if cleaned != want {
		t.Fatalf("stripExports mismatch:\n got %q\nwant %q", cleaned, want)
	}
}

func TestExportsContainRoot(t *testing.T) {
	output := "/srv/panel-storage 10.0.0.5(rw)\n/srv/other 10.0.0.6(rw)\n"
	if !exportsContainRoot(output, "/srv/panel-storage") {
		t.Fatal("expected root to be found")
	}
	if exportsContainRoot(output, "/srv/missing") {
		t.Fatal("expected missing root to be absent")
	}
}

func TestEnsureTrailingNewline(t *testing.T) {
	if got := ensureTrailingNewline("a"); got != "a\n" {
		t.Fatalf("ensureTrailingNewline(a) = %q", got)
	}
	if got := ensureTrailingNewline("a\n"); got != "a\n" {
		t.Fatalf("ensureTrailingNewline(a\\n) = %q", got)
	}
	if got := ensureTrailingNewline(""); got != "" {
		t.Fatalf("ensureTrailingNewline('') = %q", got)
	}
}

// TestExportBlockGlue 回归：已有导出内容与新托管块拼接时，托管块必须独占一行。
func TestExportBlockGlue(t *testing.T) {
	existing := "/other 1.2.3.4(rw)\n"
	next := ensureTrailingNewline(stripExports(existing, "/srv/panel-storage")) + exportsBlock("/srv/panel-storage", []string{"10.0.0.5"})
	if !strings.Contains(next, "/other 1.2.3.4(rw)\n# panel-storage-share:managed\n/srv/panel-storage 10.0.0.5(") {
		t.Fatalf("exports glue broken: %q", next)
	}
}

func TestValidRootDeniesSystemPrefixes(t *testing.T) {
	for _, root := range []string{"/etc", "/var", "/usr", "/home", "/root", "/tmp"} {
		if validRoot(root) {
			t.Fatalf("validRoot(%q) should be false", root)
		}
	}
	for _, root := range []string{"/srv/panel-storage", "/opt/panel-shared-storage", "/data"} {
		if !validRoot(root) {
			t.Fatalf("validRoot(%q) should be true", root)
		}
	}
}

func TestResolveUnderRootsRejectsSymlinks(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "storage-root")
	outside := filepath.Join(base, "outside")
	if err := os.MkdirAll(filepath.Join(root, "normal"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatal(err)
	}

	resolved, err := resolveUnderRoots([]string{root}, filepath.Join(root, "normal"), true)
	if err != nil {
		t.Fatalf("expected normal dir to resolve, got %v", err)
	}
	if !strings.HasSuffix(resolved, string(os.PathSeparator)+"normal") {
		t.Fatalf("unexpected resolved path: %q", resolved)
	}

	if _, err := resolveUnderRoots([]string{root}, filepath.Join(root, "new-dir", "child"), false); err != nil {
		t.Fatalf("expected non-existent child dir under root to pass when mustExist=false, got %v", err)
	}
	if _, err := resolveUnderRoots([]string{root}, filepath.Join(root, "new-dir", "child"), true); err == nil {
		t.Fatal("expected non-existent child dir to fail when mustExist=true")
	}

	linkPath := filepath.Join(root, "escape-link")
	if err := os.Symlink(outside, linkPath); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlink creation unavailable on Windows without privilege: %v", err)
		}
		t.Fatal(err)
	}
	if _, err := resolveUnderRoots([]string{root}, linkPath, false); err == nil {
		t.Fatal("expected symlink target directly to be rejected")
	}
	if _, err := resolveUnderRoots([]string{root}, filepath.Join(linkPath, "secret"), false); err == nil {
		t.Fatal("expected path traversing symlink to be rejected")
	}
}

func TestResolveFirewallIPs(t *testing.T) {
	got := resolveFirewallIPs([]string{"10.0.0.6", "10.0.0.5", "10.0.0.6", "2001:db8::1"})
	want := []string{"10.0.0.5", "10.0.0.6", "2001:db8::1"}
	if len(got) != len(want) {
		t.Fatalf("resolveFirewallIPs = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("resolveFirewallIPs = %#v, want %#v", got, want)
		}
	}
}
