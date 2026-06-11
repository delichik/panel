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
	for _, version := range []string{"", "dev", "main", "v1"} {
		if isReleaseVersion(version) {
			t.Fatalf("expected %q not to be a release version", version)
		}
	}
}
