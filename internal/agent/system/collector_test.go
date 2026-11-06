package system

import (
	"os"
	"path/filepath"
	"testing"
)

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

func TestReadCPUModelIgnoresNumericProcessorID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cpuinfo")
	if err := os.WriteFile(path, []byte("processor\t: 0\nvendor_id\t: GenuineIntel\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if got := readCPUModelFrom(path); got != "unknown" {
		t.Fatalf("expected numeric processor id to be ignored, got %q", got)
	}
}

func TestReadCPUModelPrefersModelName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cpuinfo")
	if err := os.WriteFile(path, []byte("processor\t: 0\nmodel name\t: Intel(R) Xeon(R)\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if got := readCPUModelFrom(path); got != "Intel(R) Xeon(R)" {
		t.Fatalf("expected model name, got %q", got)
	}
}

func TestReadCPUModelUsesNonNumericProcessorFallback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cpuinfo")
	if err := os.WriteFile(path, []byte("processor\t: AArch64 Processor rev 4\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if got := readCPUModelFrom(path); got != "AArch64 Processor rev 4" {
		t.Fatalf("expected processor fallback, got %q", got)
	}
}
