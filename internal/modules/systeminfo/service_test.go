package systeminfo

import "testing"

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		left  string
		right string
		want  int
	}{
		{left: "v0.2.0", right: "v0.1.9", want: 1},
		{left: "1.0.0", right: "v1.0.0", want: 0},
		{left: "v1.0.0", right: "v1.1.0", want: -1},
		{left: "v1.2.0-alpha", right: "v1.1.9", want: 1},
	}
	for _, test := range tests {
		if got := compareVersions(test.left, test.right); got != test.want {
			t.Fatalf("compareVersions(%q, %q) = %d, want %d", test.left, test.right, got, test.want)
		}
	}
}

func TestReleaseVersionValidation(t *testing.T) {
	for _, version := range []string{"v0.1.0", "1.2.3", "v2.0.0-alpha"} {
		if !isReleaseVersion(version) {
			t.Fatalf("expected %q to be a release version", version)
		}
	}
	for _, version := range []string{"", "dev", "main", "v1", "v0.1.7.20260613153045"} {
		if isReleaseVersion(version) {
			t.Fatalf("expected %q not to be a release version", version)
		}
	}
}

func TestUpdateCheckRequiresReleaseChannel(t *testing.T) {
	tests := []struct {
		channel string
		version string
		want    bool
	}{
		{channel: "release", version: "v0.1.7", want: true},
		{channel: "dev", version: "v0.1.7.20260613153045", want: false},
		{channel: "dev", version: "v0.1.7", want: false},
		{channel: "release", version: "v0.1.7.20260613153045", want: false},
	}
	for _, test := range tests {
		if got := shouldCheckForUpdates(test.channel, test.version); got != test.want {
			t.Fatalf("shouldCheckForUpdates(%q, %q) = %t, want %t", test.channel, test.version, got, test.want)
		}
	}
}
