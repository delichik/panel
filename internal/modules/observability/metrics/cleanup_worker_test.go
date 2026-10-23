package metrics

import (
	"testing"
	"time"
)

func TestCleanupInterval(t *testing.T) {
	tests := map[string]time.Duration{
		"hourly": time.Hour,
		"daily":  24 * time.Hour,
		"weekly": 7 * 24 * time.Hour,
	}
	for schedule, want := range tests {
		if got := cleanupInterval(schedule); got != want {
			t.Fatalf("cleanupInterval(%q) = %s, want %s", schedule, got, want)
		}
	}
}
