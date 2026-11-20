package storage

import "testing"

func TestParseNFSThreadCount(t *testing.T) {
	t.Parallel()
	cases := []struct {
		raw  string
		want int
		ok   bool
	}{
		{"8\n", 8, true},
		{"0", 0, true},
		{" 16 ", 16, true},
		{"", 0, false},
		{"abc", 0, false},
	}
	for _, tc := range cases {
		got, ok := parseNFSThreadCount(tc.raw)
		if ok != tc.ok || got != tc.want {
			t.Fatalf("parseNFSThreadCount(%q)=(%d,%v), want (%d,%v)", tc.raw, got, ok, tc.want, tc.ok)
		}
	}
}
