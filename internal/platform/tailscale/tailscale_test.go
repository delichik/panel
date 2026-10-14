package tailscale

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeConfigDisablesWithoutAuthKey(t *testing.T) {
	cfg, err := NormalizeConfig(Config{Enabled: true, Tags: []string{"tag:server"}})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Enabled {
		t.Fatal("config without an auth key must not stay enabled")
	}
	if cfg.Hostname != PanelHostname {
		t.Fatalf("hostname = %q, want %q", cfg.Hostname, PanelHostname)
	}
}

func TestNormalizeConfigRejectsInvalidAuthKeyAndTag(t *testing.T) {
	if _, err := NormalizeConfig(Config{Enabled: true, AuthKey: "not-a-key"}); err != ErrAuthKeyInvalid {
		t.Fatalf("err = %v, want %v", err, ErrAuthKeyInvalid)
	}
	if _, err := NormalizeConfig(Config{AuthKey: "tskey-auth-abc", Tags: []string{"server"}}); err != ErrTagInvalid {
		t.Fatalf("err = %v, want %v", err, ErrTagInvalid)
	}
	if _, err := NormalizeConfig(Config{AuthKey: "tskey-auth-abc", Hostname: "-bad-"}); err != ErrHostnameInvalid {
		t.Fatalf("err = %v, want %v", err, ErrHostnameInvalid)
	}
}

func TestNormalizeTagsDedupesSortsAndCaps(t *testing.T) {
	tags, err := NormalizeTags([]string{" tag:Web ", "tag:web", "", "tag:db"})
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 2 || tags[0] != "tag:db" || tags[1] != "tag:web" {
		t.Fatalf("tags = %#v", tags)
	}
	many := make([]string, 0, DefaultTagsLimit+1)
	for i := 0; i <= DefaultTagsLimit; i++ {
		many = append(many, "tag:t"+string(rune('a'+i%26))+string(rune('a'+i/26)))
	}
	if _, err := NormalizeTags(many); err == nil {
		t.Fatal("expected tag count limit error")
	}
}

func TestIsTailnetAddressOnlyAcceptsTailnetRanges(t *testing.T) {
	cases := map[string]bool{
		"100.64.0.1":            true,
		"100.127.255.254":       true,
		"fd7a:115c:a1e0::1":     true,
		"100.63.255.255":        false,
		"100.128.0.1":           false,
		"192.168.1.10":          false,
		"203.0.113.10":          false,
		"fd7a:115c:a1e1::1":     false,
		"":                      false,
		"seamark-host.internal": false,
	}
	for value, want := range cases {
		if got := IsTailnetAddress(value); got != want {
			t.Errorf("IsTailnetAddress(%q) = %v, want %v", value, got, want)
		}
	}
}

func TestFirstTailnetAddressPrefersIPv4(t *testing.T) {
	if got := FirstTailnetAddress("100.64.0.5", "fd7a:115c:a1e0::5"); got != "100.64.0.5" {
		t.Fatalf("got %q", got)
	}
	if got := FirstTailnetAddress("", "fd7a:115c:a1e0::5"); got != "fd7a:115c:a1e0::5" {
		t.Fatalf("got %q", got)
	}
	if got := FirstTailnetAddress("192.168.1.5", "203.0.113.9"); got != "" {
		t.Fatalf("non tailnet addresses must not be selected, got %q", got)
	}
}

func TestConfigRoundTripIsAtomicAndRestricted(t *testing.T) {
	root := t.TempDir()
	if cfg, err := ReadConfig(root); err != nil || cfg.Enabled {
		t.Fatalf("missing config = %#v, %v", cfg, err)
	}
	want := Config{Enabled: true, AuthKey: "tskey-auth-abc123", Tags: []string{"tag:web"}, Hostname: "seamark-panel"}
	if err := WriteConfig(root, want); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(ConfigPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("config mode = %v, want 0640", info.Mode().Perm())
	}
	got, err := ReadConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	if got.AuthKey != want.AuthKey || !got.Enabled || len(got.Tags) != 1 || got.Tags[0] != "tag:web" {
		t.Fatalf("config round trip = %#v", got)
	}
	entries, err := os.ReadDir(Dir(root))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".tmp") {
			t.Fatalf("atomic write left a temporary file: %s", entry.Name())
		}
	}
}

func TestStatusRoundTrip(t *testing.T) {
	root := t.TempDir()
	if status, err := ReadStatus(root); err != nil || status.Running {
		t.Fatalf("missing status = %#v, %v", status, err)
	}
	if err := WriteStatus(root, Status{Available: true, Running: true, LoggedIn: true, IPv4: "100.64.0.7"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(StatusPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("status mode = %v, want 0600", info.Mode().Perm())
	}
	status, err := ReadStatus(root)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Available || !status.LoggedIn || status.IPv4 != "100.64.0.7" || status.UpdatedAt.IsZero() {
		t.Fatalf("status = %#v", status)
	}
}

func TestEnsureDirCreatesWorkDirectory(t *testing.T) {
	root := filepath.Join(t.TempDir(), "data")
	dir, err := EnsureDir(root, -1, -1)
	if err != nil {
		t.Fatal(err)
	}
	if dir != Dir(root) {
		t.Fatalf("dir = %q, want %q", dir, Dir(root))
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("stat dir = %v, %v", info, err)
	}
}
