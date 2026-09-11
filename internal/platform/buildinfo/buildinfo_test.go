package buildinfo

import "testing"

func TestNormalizedChannel(t *testing.T) {
	original := Channel
	t.Cleanup(func() {
		Channel = original
	})

	tests := []struct {
		value string
		want  string
	}{
		{value: "release", want: "release"},
		{value: " RELEASE ", want: "release"},
		{value: "dev", want: "dev"},
		{value: "", want: "dev"},
		{value: "unknown", want: "dev"},
	}
	for _, test := range tests {
		Channel = test.value
		if got := NormalizedChannel(); got != test.want {
			t.Fatalf("NormalizedChannel() with %q = %q, want %q", test.value, got, test.want)
		}
	}
}
