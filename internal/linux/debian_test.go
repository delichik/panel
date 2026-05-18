package linux

import "testing"

func TestParseOSReleaseSupportsDebian12And13(t *testing.T) {
	adapter := DebianAdapter{}
	for _, version := range []string{"12", "13"} {
		info := ParseOSRelease("ID=debian\nVERSION_ID=\"" + version + "\"\nPRETTY_NAME=\"Debian\"\n")
		if !adapter.Supports(info) {
			t.Fatalf("Debian %s should be supported", version)
		}
	}
	info := ParseOSRelease("ID=ubuntu\nVERSION_ID=\"24.04\"\n")
	if adapter.Supports(info) {
		t.Fatal("ubuntu must not be supported in phase 1")
	}
}

func TestParseAptListUpgradable(t *testing.T) {
	out := `Listing...
openssl/stable-security 3.0.17-1 amd64 [upgradable from: 3.0.16-1]
curl/stable 8.0.1 amd64 [upgradable from: 8.0.0]`
	updates := ParseAptListUpgradable(out)
	if len(updates) != 2 || updates[0].Name != "openssl" || updates[0].InstalledVersion != "3.0.16-1" {
		t.Fatalf("unexpected updates: %#v", updates)
	}
}

func TestParseMetricsOutput(t *testing.T) {
	out := "100 40\n8000 2000\n100000 50000\n10 20\nhost\nkernel\nDebian\n123\n0.1 0.2 0.3 1/2 3"
	snap, err := ParseMetricsOutput("srv", out)
	if err != nil {
		t.Fatal(err)
	}
	if snap.CPUUsagePercent != 60 || snap.MemoryUsedBytes != 2000 || snap.Status.Hostname != "host" {
		t.Fatalf("unexpected snapshot: %#v", snap)
	}
}
