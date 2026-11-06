package linux

import (
	"context"
	"strings"
	"testing"
	"time"

	"panel/internal/platform/linux/remoteops"
	"panel/internal/platform/ssh"
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
	if !selected.SupportsUFW() ||
		!strings.Contains(selected.UFWInstallScript(), "apt_get install -y ufw") {
		t.Fatalf("Ubuntu adapter should expose apt-based UFW support")
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

func TestRemoteopsRunnerStreamsOutputBeforeCommandReturns(t *testing.T) {
	sink := &recordingLogSink{}
	exec := &streamingFakeExecutor{duringRun: func() {
		if len(sink.lines) != 1 || sink.lines[0] != "stdout:first" {
			t.Fatalf("expected stdout to be logged while command is still running, got %#v", sink.lines)
		}
	}}

	if _, err := (remoteops.Runner{Exec: exec, Target: sshx.Target{}, Log: sink}).RunSudoLogged(context.Background(), "apt-get upgrade", 10*time.Minute); err != nil {
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
