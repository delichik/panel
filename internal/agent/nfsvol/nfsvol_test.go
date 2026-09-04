package nfsvol

import (
	"testing"
)

func TestNameDeterministic(t *testing.T) {
	a := Name("10.0.0.5:/srv/data/app-1", "/data")
	b := Name("10.0.0.5:/srv/data/app-1", "/data")
	if a != b {
		t.Fatal("Name must be deterministic")
	}
	if a == Name("10.0.0.5:/srv/data/app-1", "/other") {
		t.Fatal("Name must depend on target")
	}
	if len(a) != len("panel-nfs-")+24 {
		t.Fatalf("unexpected name length %q", a)
	}
}

func TestSplitSource(t *testing.T) {
	host, export, err := SplitSource("10.0.0.5:/srv/data")
	if err != nil || host != "10.0.0.5" || export != "/srv/data" {
		t.Fatalf("split ipv4 = %q %q %v", host, export, err)
	}
	host, export, err = SplitSource("[2001:db8::1]:/srv/data")
	if err != nil || host != "2001:db8::1" || export != "/srv/data" {
		t.Fatalf("split ipv6 = %q %q %v", host, export, err)
	}
	if _, _, err := SplitSource("bad"); err == nil {
		t.Fatal("expected error for invalid source")
	}
}

func TestOptions(t *testing.T) {
	opts, err := Options("10.0.0.5:/srv/data", false)
	if err != nil {
		t.Fatal(err)
	}
	if opts["type"] != "nfs" || opts["device"] != ":/srv/data" || opts["o"] != "addr=10.0.0.5,rw,nfsvers=4" {
		t.Fatalf("options = %#v", opts)
	}
	ro, err := Options("10.0.0.5:/srv/data", true)
	if err != nil {
		t.Fatal(err)
	}
	if ro["o"] != "addr=10.0.0.5,ro,nfsvers=4" {
		t.Fatalf("read-only options = %#v", ro)
	}
	if _, err := Options("10.0.0.5,ro:/srv/data", false); err == nil {
		t.Fatal("expected invalid host to be rejected")
	}
}
