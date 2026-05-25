package linux

import (
	"context"
	"testing"

	"panel/internal/sshx"
)

func TestParseOSReleaseSupportsDebian12And13(t *testing.T) {
	adapter := DebianAdapter{}
	for _, version := range []string{"12", "13"} {
		info := ParseOSRelease("ID=debian\nVERSION_ID=\"" + version + "\"\nPRETTY_NAME=\"Debian\"\n")
		if !adapter.Supports(info) {
			t.Fatalf("Debian %s should be supported", version)
		}
	}
	info := ParseOSRelease("ID=ubuntu\nVERSION_ID=\"24.04\"\n")
	if adapter.Supports(info) {
		t.Fatal("ubuntu must not be supported in phase 1")
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
	out := "100 40\n8000 2000\n100000 50000\n10 20\nhost\nkernel\nDebian\n123\n0.1 0.2 0.3 1/2 3"
	snap, err := ParseMetricsOutput("srv", out)
	if err != nil {
		t.Fatal(err)
	}
	if snap.CPUUsagePercent != 60 || snap.MemoryUsedBytes != 2000 || snap.Status.Hostname != "host" {
		t.Fatalf("unexpected snapshot: %#v", snap)
	}
}

func TestRunLoggedStreamsOutputBeforeCommandReturns(t *testing.T) {
	sink := &recordingLogSink{}
	exec := &streamingFakeExecutor{duringRun: func() {
		if len(sink.lines) != 1 || sink.lines[0] != "stdout:first" {
			t.Fatalf("expected stdout to be logged while command is still running, got %#v", sink.lines)
		}
	}}

	if err := runLogged(context.Background(), exec, sshx.Target{}, "apt-get upgrade", sink); err != nil {
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

type recordingLogSink struct {
	lines []string
}

func (s *recordingLogSink) AppendLog(_ context.Context, stream, line string) error {
	s.lines = append(s.lines, stream+":"+line)
	return nil
}

type streamingFakeExecutor struct {
	duringRun func()
}

func (f *streamingFakeExecutor) Exec(context.Context, sshx.Target, sshx.CommandSpec) (sshx.CommandResult, error) {
	return sshx.CommandResult{}, nil
}

func (f *streamingFakeExecutor) ExecSudo(ctx context.Context, target sshx.Target, command sshx.CommandSpec) (sshx.CommandResult, error) {
	command.OnStdout("first")
	if f.duringRun != nil {
		f.duringRun()
	}
	command.OnStderr("warn")
	command.OnStdout("last")
	return sshx.CommandResult{Stdout: "first\nlast\n", Stderr: "warn\n"}, nil
}

func (f *streamingFakeExecutor) Upload(context.Context, sshx.Target, sshx.UploadSpec) error {
	return nil
}

func (f *streamingFakeExecutor) Download(context.Context, sshx.Target, sshx.DownloadSpec) error {
	return nil
}
