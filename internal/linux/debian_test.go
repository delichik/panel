package linux

import (
	"context"
	"strings"
	"testing"
	"time"

	"panel/internal/sshx"
)

func TestParseOSReleaseSupportsRegisteredDistros(t *testing.T) {
	adapter := DebianAdapter{}
	for _, version := range []string{"12", "13"} {
		info := ParseOSRelease("ID=debian\nVERSION_ID=\"" + version + "\"\nPRETTY_NAME=\"Debian\"\n")
		if !adapter.Supports(info) {
			t.Fatalf("Debian %s should be supported", version)
		}
		if !Supported(info) {
			t.Fatalf("Debian %s should be supported by registry", version)
		}
	}
	info := ParseOSRelease("ID=ubuntu\nVERSION_ID=\"24.04\"\nPRETTY_NAME=\"Ubuntu 24.04 LTS\"\n")
	if adapter.Supports(info) {
		t.Fatal("ubuntu must not be supported by the Debian adapter")
	}
	if !Supported(info) {
		t.Fatal("Ubuntu 24.04 should be supported by registry")
	}
	selected, ok := AdapterFor(info)
	if !ok || selected.ID() != "ubuntu" {
		t.Fatalf("expected Ubuntu adapter, got %#v", selected)
	}
	if !strings.Contains(selected.NomadInstallScript(), "apt.releases.hashicorp.com") ||
		!strings.Contains(selected.NomadRuntimePrereqsScript(), "docker.io") ||
		!strings.Contains(selected.NomadServiceRestartScript(), "systemctl restart nomad") {
		t.Fatalf("Ubuntu adapter should expose apt-based Nomad scripts")
	}
}

func TestDetectUsesRegisteredDistroAdapters(t *testing.T) {
	exec := &metricsCommandExecutor{stdout: "ID=ubuntu\nVERSION_ID=\"24.04\"\nPRETTY_NAME=\"Ubuntu 24.04 LTS\"\n"}

	info, err := Detect(context.Background(), exec, sshx.Target{})
	if err != nil {
		t.Fatal(err)
	}
	if !info.Supported || info.ID != "ubuntu" || info.VersionID != "24.04" {
		t.Fatalf("unexpected detection result: %#v", info)
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
	out := "100 40\n8000 2000\n100000 50000\n1000000000 100000000000 200000000000\n2000000000 100000001024 200000002048\nhost\nkernel\nDebian\n123\n0.1 0.2 0.3 1/2 3"
	snap, err := ParseMetricsOutput("srv", out)
	if err != nil {
		t.Fatal(err)
	}
	if snap.CPUUsagePercent != 60 || snap.MemoryUsedBytes != 2000 || snap.Status.Hostname != "host" || snap.NetworkRxBytesRate != 1024 || snap.NetworkTxBytesRate != 2048 {
		t.Fatalf("unexpected snapshot: %#v", snap)
	}
}

func TestParseMetricsOutputUsesNetworkDeltas(t *testing.T) {
	out := "100 40\n8000 2000\n100000 50000\n1000000000 987654321000 123456789000\n3000000000 987654321512 123456789256\nhost\nkernel\nDebian\n123\n0.1 0.2 0.3 1/2 3"
	snap, err := ParseMetricsOutput("srv", out)
	if err != nil {
		t.Fatal(err)
	}
	if snap.NetworkRxBytesRate != 256 || snap.NetworkTxBytesRate != 128 {
		t.Fatalf("expected rates to be based on counter deltas, got rx=%v tx=%v", snap.NetworkRxBytesRate, snap.NetworkTxBytesRate)
	}
}

func TestCollectMetricsSamplesNetworkCounters(t *testing.T) {
	exec := &metricsCommandExecutor{stdout: "100 40\n8000 2000\n100000 50000\n1000000000 10 20\n2000000000 20 30\nhost\nkernel\nDebian\n123\n0.1 0.2 0.3 1/2 3"}
	if _, err := (DebianAdapter{}).CollectMetrics(context.Background(), exec, sshx.Target{ServerID: "srv"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(exec.command, "sleep 1") || !strings.Contains(exec.command, `iface=="lo"`) {
		t.Fatalf("metrics command should sample non-loopback network counters over time, got %q", exec.command)
	}
}

func TestRunLoggedStreamsOutputBeforeCommandReturns(t *testing.T) {
	sink := &recordingLogSink{}
	exec := &streamingFakeExecutor{duringRun: func() {
		if len(sink.lines) != 1 || sink.lines[0] != "stdout:first" {
			t.Fatalf("expected stdout to be logged while command is still running, got %#v", sink.lines)
		}
	}}

	if err := runLogged(context.Background(), exec, sshx.Target{}, "apt-get upgrade", 10*time.Minute, sink); err != nil {
		t.Fatal(err)
	}

	want := []string{"stdout:first", "stderr:warn", "stdout:last"}
	if len(sink.lines) != len(want) {
		t.Fatalf("expected %d log lines, got %#v", len(want), sink.lines)
	}
	for i := range want {
		if sink.lines[i] != want[i] {
			t.Fatalf("line %d = %q, want %q; all lines %#v", i, sink.lines[i], want[i], sink.lines)
		}
	}
}

func TestListUpgradeableUsesExtendedTimeout(t *testing.T) {
	exec := &streamingFakeExecutor{}

	if _, err := (DebianAdapter{}).ListUpgradeable(context.Background(), exec, sshx.Target{}); err != nil {
		t.Fatal(err)
	}
	if exec.lastTimeout != packageListTimeout {
		t.Fatalf("expected list timeout %s, got %s", packageListTimeout, exec.lastTimeout)
	}
}

func TestUpgradeAllUsesLongTimeout(t *testing.T) {
	exec := &streamingFakeExecutor{}
	sink := &recordingLogSink{}

	if err := (DebianAdapter{}).UpgradeAll(context.Background(), exec, sshx.Target{}, sink); err != nil {
		t.Fatal(err)
	}
	if exec.lastTimeout != packageUpgradeTimeout {
		t.Fatalf("expected upgrade timeout %s, got %s", packageUpgradeTimeout, exec.lastTimeout)
	}
}

type metricsCommandExecutor struct {
	command string
	stdout  string
}

func (f *metricsCommandExecutor) Exec(_ context.Context, _ sshx.Target, command sshx.CommandSpec) (sshx.CommandResult, error) {
	f.command = command.Command
	return sshx.CommandResult{Stdout: f.stdout}, nil
}

func (f *metricsCommandExecutor) ExecSudo(context.Context, sshx.Target, sshx.CommandSpec) (sshx.CommandResult, error) {
	return sshx.CommandResult{}, nil
}

func (f *metricsCommandExecutor) Upload(context.Context, sshx.Target, sshx.UploadSpec) error {
	return nil
}

func (f *metricsCommandExecutor) Download(context.Context, sshx.Target, sshx.DownloadSpec) error {
	return nil
}

type recordingLogSink struct {
	lines []string
}

func (s *recordingLogSink) AppendLog(_ context.Context, stream, line string) error {
	s.lines = append(s.lines, stream+":"+line)
	return nil
}

type streamingFakeExecutor struct {
	duringRun   func()
	lastTimeout time.Duration
}

func (f *streamingFakeExecutor) Exec(context.Context, sshx.Target, sshx.CommandSpec) (sshx.CommandResult, error) {
	return sshx.CommandResult{}, nil
}

func (f *streamingFakeExecutor) ExecSudo(ctx context.Context, target sshx.Target, command sshx.CommandSpec) (sshx.CommandResult, error) {
	f.lastTimeout = command.Timeout
	if command.OnStdout != nil {
		command.OnStdout("first")
	}
	if f.duringRun != nil {
		f.duringRun()
	}
	if command.OnStderr != nil {
		command.OnStderr("warn")
	}
	if command.OnStdout != nil {
		command.OnStdout("last")
	}
	return sshx.CommandResult{Stdout: "first\nlast\n", Stderr: "warn\n"}, nil
}

func (f *streamingFakeExecutor) Upload(context.Context, sshx.Target, sshx.UploadSpec) error {
	return nil
}

func (f *streamingFakeExecutor) Download(context.Context, sshx.Target, sshx.DownloadSpec) error {
	return nil
}
