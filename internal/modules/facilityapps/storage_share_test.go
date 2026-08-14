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
		{"/srv/panel-storage/srv-a/srv-b/app-1", true},
		{"/srv/panel-storage/srv-a/srv-b/app-1/", false},
		{"/srv/panel-storage/../app", false},
		{"relative/path", false},
	}
	for _, tc := range cases {
		if got := validStoragePath(tc.value); got != tc.want {
			t.Errorf("validStoragePath(%q) = %v, want %v", tc.value, got, tc.want)
		}
	}
}

func TestStoragePartitionPathAndNFSSource(t *testing.T) {
	if got := storagePartitionPath("/srv/panel-storage", "srv-a", "srv-b", "app-1"); got != "/srv/panel-storage/srv-a/srv-b/app-1" {
		t.Fatalf("storagePartitionPath = %q", got)
	}
	if got := storageNFSSource("10.0.0.5", "/srv/panel-storage/srv-a/srv-b/app-1"); got != "10.0.0.5:/srv/panel-storage/srv-a/srv-b/app-1" {
		t.Fatalf("storageNFSSource ipv4 = %q", got)
	}
	if got := storageNFSSource("2001:db8::1", "/srv/panel-storage/srv-a/srv-b/app-1"); got != "[2001:db8::1]:/srv/panel-storage/srv-a/srv-b/app-1" {
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

func TestResolveStorageServerSetting(t *testing.T) {
	servers := []StorageServerSetting{
		{ServerID: "srv-a", Root: "/srv/a"},
		{ServerID: "srv-c", Root: "/srv/c"},
	}
	if got, err := resolveStorageServerSetting("storage-share", servers); err != nil || got.ServerID != "srv-a" || got.Root != "/srv/a" {
		t.Fatalf("resolve legacy source = %#v err=%v", got, err)
	}
	if got, err := resolveStorageServerSetting("storage-share:srv-c", servers); err != nil || got.ServerID != "srv-c" || got.Root != "/srv/c" {
		t.Fatalf("resolve explicit source = %#v err=%v", got, err)
	}
	if _, err := resolveStorageServerSetting("storage-share:srv-x", servers); err == nil {
		t.Fatal("expected unknown server to be rejected")
	}
	if _, err := resolveStorageServerSetting("other", servers); err == nil {
		t.Fatal("expected invalid source to be rejected")
	}
}
