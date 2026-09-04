package storage

import (
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
