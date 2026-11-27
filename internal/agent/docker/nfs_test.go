package docker

import (
	"testing"
)

func TestSplitNFSSource(t *testing.T) {
	cases := []struct {
		source   string
		host     string
		export   string
		hasError bool
	}{
		{"10.0.0.5:/srv/data/app-1", "10.0.0.5", "/srv/data/app-1", false},
		{"[2001:db8::1]:/srv/data", "2001:db8::1", "/srv/data", false},
		{"/srv/data", "", "", true},
		{"10.0.0.5", "", "", true},
		{"", "", "", true},
	}
	for _, tc := range cases {
		host, export, err := splitNFSSource(tc.source)
		if tc.hasError {
			if err == nil {
				t.Errorf("splitNFSSource(%q) expected error", tc.source)
			}
			continue
		}
		if err != nil {
			t.Fatalf("splitNFSSource(%q): %v", tc.source, err)
		}
		if host != tc.host || export != tc.export {
			t.Errorf("splitNFSSource(%q) = (%q,%q), want (%q,%q)", tc.source, host, export, tc.host, tc.export)
		}
	}
}

func TestNFSVolumeNameDeterministic(t *testing.T) {
	first := nfsVolumeName("10.0.0.5:/srv/data/app-1", "/data")
	second := nfsVolumeName("10.0.0.5:/srv/data/app-1", "/data")
	other := nfsVolumeName("10.0.0.5:/srv/data/app-1", "/other")
	if first != second {
		t.Fatalf("nfsVolumeName not deterministic")
	}
	if first == other {
		t.Fatalf("nfsVolumeName ignores target")
	}
	if len(first) != len("panel-nfs-")+24 {
		t.Fatalf("nfsVolumeName length = %d", len(first))
	}
}

func TestNFSVolumeOptions(t *testing.T) {
	opts, err := nfsVolumeOptions("10.0.0.5:/srv/data/app-1", false)
	if err != nil {
		t.Fatal(err)
	}
	if opts["type"] != "nfs" || opts["device"] != ":/srv/data/app-1" || opts["o"] != "addr=10.0.0.5,rw,nfsvers=4" {
		t.Fatalf("nfsVolumeOptions = %#v", opts)
	}
}
