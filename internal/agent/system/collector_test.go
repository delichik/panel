package system

import "testing"

func TestParseLoadAverage(t *testing.T) {
	raw := "1.25 0.75 0.50 2/100 123"
	load1, load5, load15 := parseLoadAverage(raw)
	if load1 != 1.25 || load5 != 0.75 || load15 != 0.50 {
		t.Fatalf("unexpected load averages: %v %v %v", load1, load5, load15)
	}
	if got := normalizedLoadAverage(raw); got != "1.25 0.75 0.50" {
		t.Fatalf("normalized load average = %q", got)
	}
}
