package storage

import (
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
