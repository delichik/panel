package facilityapps

import (
	"testing"
)

func TestValidStorageRoot(t *testing.T) {
	cases := []struct {
		value string
		want  bool
	}{
		{"/srv/panel-storage", true},
		{"/opt/panel/storage", true},
		{"/", false},
		{"srv/panel-storage", false},
		{"/srv/panel storage", false},
		{"/srv/../etc", false},
		{"/srv/./data", false},
		{"/srv//data", false},
		{"/srv/panel-storage/", false},
		{"/srv/panel-storage\nrm -rf /", false},
	}
	for _, tc := range cases {
		if got := validStorageRoot(tc.value); got != tc.want {
			t.Errorf("validStorageRoot(%q) = %v, want %v", tc.value, got, tc.want)
		}
	}
}

func TestValidStoragePath(t *testing.T) {
	cases := []struct {
		value string
		want  bool
	}{
		{"/srv/panel-storage/srv-a/app-1", true},
		{"/srv/panel-storage/srv-a/app-1/", false},
		{"/srv/panel-storage/../app", false},
		{"relative/path", false},
	}
	for _, tc := range cases {
		if got := validStoragePath(tc.value); got != tc.want {
			t.Errorf("validStoragePath(%q) = %v, want %v", tc.value, got, tc.want)
		}
	}
}

func TestStorageExportsBlockAndStrip(t *testing.T) {
	block := storageExportsBlock("/srv/panel-storage", []string{"10.0.0.5", "10.0.0.6"})
	expect := "# panel-storage-share:managed\n/srv/panel-storage 10.0.0.5(rw,sync,no_subtree_check,no_root_squash,insecure) 10.0.0.6(rw,sync,no_subtree_check,no_root_squash,insecure)\n"
	if block != expect {
		t.Fatalf("storageExportsBlock mismatch:\n got %q\nwant %q", block, expect)
	}

	existing := "/other 1.2.3.4(rw)\n" + block + "/tail 5.6.7.8(rw)\n"
	cleaned := stripStorageExports(existing, "/srv/panel-storage")
	want := "/other 1.2.3.4(rw)\n/tail 5.6.7.8(rw)"
	if cleaned != want {
		t.Fatalf("stripStorageExports mismatch:\n got %q\nwant %q", cleaned, want)
	}
}

func TestStoragePartitionPathAndNFSSource(t *testing.T) {
	if got := storagePartitionPath("/srv/panel-storage", "srv-a", "app-1"); got != "/srv/panel-storage/srv-a/app-1" {
		t.Fatalf("storagePartitionPath = %q", got)
	}
	if got := storageNFSSource("10.0.0.5", "/srv/panel-storage/srv-a/app-1"); got != "10.0.0.5:/srv/panel-storage/srv-a/app-1" {
		t.Fatalf("storageNFSSource ipv4 = %q", got)
	}
	if got := storageNFSSource("2001:db8::1", "/srv/panel-storage/srv-a/app-1"); got != "[2001:db8::1]:/srv/panel-storage/srv-a/app-1" {
		t.Fatalf("storageNFSSource ipv6 = %q", got)
	}
}

func TestStorageHostSpec(t *testing.T) {
	if got := storageHostSpec("10.0.0.5"); got != "10.0.0.5" {
		t.Fatalf("storageHostSpec ip = %q", got)
	}
	if got := storageHostSpec("node-a.example.com"); got != "node-a.example.com" {
		t.Fatalf("storageHostSpec hostname = %q", got)
	}
	if got := storageHostSpec("bad host"); got != "" {
		t.Fatalf("storageHostSpec bad = %q", got)
	}
}