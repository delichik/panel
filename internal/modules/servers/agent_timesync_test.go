package server

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"

	panelerr "panel/internal/platform/errors"
	"panel/internal/platform/linux/remoteops"
	sshx "panel/internal/platform/ssh"
)

// timeSyncProvisionFakeExec serves the read-only chrony probe and records every
// sudo command so the test can assert whether the provisioning script ran.
type timeSyncProvisionFakeExec struct {
	status       agentTimeSyncStatus
	failProbe    bool
	failInstall  bool
	probeCalls   int
	installCalls int
	sudoCommands []string
	lastTimeout  time.Duration
}

func (f *timeSyncProvisionFakeExec) Exec(context.Context, sshx.Target, sshx.CommandSpec) (sshx.CommandResult, error) {
	return sshx.CommandResult{ExitCode: 0}, nil
}

func (f *timeSyncProvisionFakeExec) ExecSudo(_ context.Context, _ sshx.Target, command sshx.CommandSpec) (sshx.CommandResult, error) {
	f.sudoCommands = append(f.sudoCommands, command.Command)
	f.lastTimeout = command.Timeout
	// The install script is the only generated command that invokes apt; the
	// probe is read-only and never mentions it.
	if strings.Contains(command.Command, "apt_get") {
		f.installCalls++
		if f.failInstall {
			return sshx.CommandResult{ExitCode: 1}, errors.New("apt install failed")
		}
		return sshx.CommandResult{Stdout: "panel_agent_chrony_ready\n", ExitCode: 0}, nil
	}
	f.probeCalls++
	if f.failProbe {
		return sshx.CommandResult{ExitCode: 1}, errors.New("probe failed")
	}
	return sshx.CommandResult{Stdout: timeSyncProbeOutput(f.status), ExitCode: 0}, nil
}

func (f *timeSyncProvisionFakeExec) Upload(context.Context, sshx.Target, sshx.UploadSpec) error {
	return nil
}

func (f *timeSyncProvisionFakeExec) Download(context.Context, sshx.Target, sshx.DownloadSpec) error {
	return nil
}

func timeSyncProbeOutput(status agentTimeSyncStatus) string {
	installed := "missing"
	if status.Installed {
		installed = "present"
	}
	active := "inactive"
	if status.Active {
		active = "active"
	}
	synced := "false"
	if status.Synced {
		synced = "true"
	}
	return "panel_agent_chrony=" + installed + "\n" +
		"panel_agent_chrony_service=" + active + "\n" +
		"panel_agent_chrony_synced=" + synced + "\n"
}

func TestAgentTimeSyncStatusScriptIsReadOnly(t *testing.T) {
	probe := agentTimeSyncStatusScript()
	for _, want := range []string{
		"command -v chronyd",
		"command -v chronyc",
		"/etc/chrony/chrony.conf",
		"panel_agent_chrony=present",
		"panel_agent_chrony=missing",
		"panel_agent_chrony_service=active",
		"panel_agent_chrony_synced=false",
		`echo "panel_agent_chrony_synced=$synced"`,
		"chronyc tracking",
		"Leap status",
		"System time",
	} {
		if !strings.Contains(probe, want) {
			t.Fatalf("expected %q in the status probe:\n%s", want, probe)
		}
	}
	// A probe must never change remote state; only the read-only service query
	// (systemctl is-active) may appear.
	for _, forbidden := range []string{"apt_get", "install -d", "cat >", ">>", "systemctl enable", "systemctl restart", "systemctl stop"} {
		if strings.Contains(probe, forbidden) {
			t.Fatalf("the status probe must be read-only, found %q in:\n%s", forbidden, probe)
		}
	}
}

func TestAgentTimeSyncInstallScript(t *testing.T) {
	install, err := agentTimeSyncInstallScript()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"apt_get update",
		"apt_get install -y " + agentTimeSyncPackageName,
		"confdir " + agentTimeSyncConfDir,
		agentTimeSyncConfigPath,
		"server ntp.aliyun.com iburst",
		"server ntp.tencent.com iburst",
		"makestep 1.0 3",
		"chronyc tracking",
		"systemctl enable " + agentTimeSyncPackageName,
		"systemctl restart " + agentTimeSyncPackageName,
		"panel_agent_chrony_ready",
		"panel_agent_chrony_failed reason=",
	} {
		if !strings.Contains(install, want) {
			t.Fatalf("expected %q in the install script:\n%s", want, install)
		}
	}
	// The step is additive: it never deletes configuration, never disables the
	// service, and never overwrites the distribution's chrony.conf (the confdir
	// include is appended, not rewritten).
	for _, forbidden := range []string{"rm -", "systemctl disable", "systemctl mask", "apt-get remove", "apt-get purge", "cat > /etc/chrony/chrony.conf", "tee /etc/chrony/chrony.conf", "sed -i"} {
		if strings.Contains(install, forbidden) {
			t.Fatalf("did not expect %q in the install script:\n%s", forbidden, install)
		}
	}
	if !strings.Contains(install, ">> /etc/chrony/chrony.conf") {
		t.Fatalf("expected the confdir include to be appended to chrony.conf:\n%s", install)
	}
	if !strings.Contains(install, "'EOF'") {
		t.Fatalf("expected a quoted heredoc so the static config is not re-expanded:\n%s", install)
	}
	// Both scripts embed the shared awk program inside single quotes, so it must
	// not contain a single quote itself.
	if strings.Contains(agentTimeSyncSyncedAWK, "'") {
		t.Fatalf("the synchronisation awk program must not contain a single quote:\n%s", agentTimeSyncSyncedAWK)
	}
}

// TestAgentTimeSyncProvisioningWorstCaseFitsInsideThePanelBound pins the timeout
// invariant the other prerequisites have: the target-side worst case (two
// bounded apt invocations, the synchronization wait window, and the cheap
// service operations) must stay strictly inside the Panel-side bound, so the
// script can always finish and report its own failure reason.
func TestAgentTimeSyncProvisioningWorstCaseFitsInsideThePanelBound(t *testing.T) {
	worst := time.Duration(2*agentTimeSyncAPTTimeoutSeconds)*time.Second +
		time.Duration(agentTimeSyncSyncPollAttempts)*agentTimeSyncSyncPollInterval +
		agentTimeSyncServiceOperationBound
	if worst >= agentTimeSyncInstallTimeout {
		t.Fatalf("target-side worst case %s must stay strictly inside the Panel bound %s", worst, agentTimeSyncInstallTimeout)
	}
	waitWindow := time.Duration(agentTimeSyncSyncPollAttempts) * agentTimeSyncSyncPollInterval
	if waitWindow > time.Minute {
		t.Fatalf("the synchronization wait window %s must stay within one minute", waitWindow)
	}
}

func TestAgentTimeSyncScriptsAreSyntacticallyValid(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not available")
	}
	install, err := agentTimeSyncInstallScript()
	if err != nil {
		t.Fatal(err)
	}
	for name, script := range map[string]string{"probe": agentTimeSyncStatusScript(), "install": install} {
		cmd := exec.Command(bash, "-n")
		cmd.Stdin = strings.NewReader(script)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s script failed bash -n: %v\n%s\n--- script ---\n%s", name, err, out, script)
		}
	}
}

// TestAgentTimeSyncSyncedJudgement runs the shared awk program against real
// chronyc tracking output shapes. chronyc prints the offset as
// "0.000012345 seconds fast of NTP time", and a leap status other than Normal
// means the clock is not synchronized even when the offset looks small.
func TestAgentTimeSyncSyncedJudgement(t *testing.T) {
	awk, err := exec.LookPath("awk")
	if err != nil {
		t.Skip("awk is not available")
	}
	tracking := func(systemTime, leap string) string {
		return "Reference ID    : C0A80101 (ntp.aliyun.com)\n" +
			"Stratum         : 2\n" +
			"Ref time (UTC)  : Thu Nov 18 06:00:00 2026\n" +
			"System time     : " + systemTime + "\n" +
			"Last offset     : +0.000000123 seconds\n" +
			"RMS offset      : 0.000000456 seconds\n" +
			"Frequency       : 10.123 ppm slow\n" +
			"Residual freq   : +0.001 ppm\n" +
			"Skew            : 0.123 ppm\n" +
			"Root delay      : 0.012345 seconds\n" +
			"Root dispersion : 0.001234 seconds\n" +
			"Update interval : 64.2 seconds\n" +
			"Leap status     : " + leap + "\n"
	}
	cases := []struct {
		name   string
		stdin  string
		synced string
	}{
		{"normal and tiny offset", tracking("0.000012345 seconds fast of NTP time", "Normal"), "true"},
		{"normal and negative offset inside the bound", tracking("0.500000000 seconds slow of NTP time", "Normal"), "true"},
		{"normal and explicitly signed offset", tracking("+0.200000000 seconds fast of NTP time", "Normal"), "true"},
		{"normal but offset over one second", tracking("1.500000000 seconds fast of NTP time", "Normal"), "false"},
		{"not synchronised", tracking("0.000012345 seconds fast of NTP time", "Not synchronised"), "false"},
		{"non numeric offset", tracking("N/A", "Normal"), "false"},
		{"empty output", "", "false"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command(awk, agentTimeSyncSyncedAWK)
			cmd.Stdin = strings.NewReader(tt.stdin)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("awk failed: %v\n%s", err, out)
			}
			if got := strings.TrimSpace(string(out)); got != tt.synced {
				t.Fatalf("synced = %q, want %q", got, tt.synced)
			}
		})
	}
}

func TestParseAgentTimeSyncStatus(t *testing.T) {
	if got := parseAgentTimeSyncStatus(""); got.Installed || got.Active || got.Synced {
		t.Fatalf("an empty probe must degrade to the zero value, got %#v", got)
	}
	ready := parseAgentTimeSyncStatus("panel_agent_chrony=present\npanel_agent_chrony_service=active\npanel_agent_chrony_synced=true\n")
	if !ready.Installed || !ready.Active || !ready.Synced {
		t.Fatalf("expected the ready markers to parse, got %#v", ready)
	}
	unsynced := parseAgentTimeSyncStatus("panel_agent_chrony=present\npanel_agent_chrony_service=active\npanel_agent_chrony_synced=false\n")
	if !unsynced.Installed || !unsynced.Active || unsynced.Synced {
		t.Fatalf("expected installed/active/unsynced, got %#v", unsynced)
	}
	missing := parseAgentTimeSyncStatus("panel_agent_chrony=missing\npanel_agent_chrony_service=inactive\npanel_agent_chrony_synced=false\n")
	if missing.Installed {
		t.Fatalf("expected missing, got %#v", missing)
	}
}

func TestEnsureAgentTimeSync(t *testing.T) {
	base := Server{ID: "srv_1", Host: "10.0.0.1", Port: 22}

	t.Run("a synchronized clock is left alone", func(t *testing.T) {
		fake := &timeSyncProvisionFakeExec{status: agentTimeSyncStatus{Installed: true, Active: true, Synced: true}}
		svc, _, _ := testServerService(t, fake)
		if err := svc.ensureAgentTimeSync(context.Background(), "task_1", base, remoteops.Runner{Exec: fake}); err != nil {
			t.Fatal(err)
		}
		if fake.probeCalls != 1 || fake.installCalls != 0 {
			t.Fatalf("expected one read-only probe and no install, got probe=%d install=%d", fake.probeCalls, fake.installCalls)
		}
		if len(fake.sudoCommands) != 1 || strings.Contains(fake.sudoCommands[0], "apt_get") {
			t.Fatalf("no remote mutation may run for a synchronized clock, got %#v", fake.sudoCommands)
		}
		if fake.lastTimeout != agentTimeSyncProbeTimeout {
			t.Fatalf("probe timeout = %s, want %s", fake.lastTimeout, agentTimeSyncProbeTimeout)
		}
	})

	t.Run("a missing chrony is installed", func(t *testing.T) {
		fake := &timeSyncProvisionFakeExec{}
		svc, _, _ := testServerService(t, fake)
		if err := svc.ensureAgentTimeSync(context.Background(), "task_1", base, remoteops.Runner{Exec: fake}); err != nil {
			t.Fatal(err)
		}
		if fake.probeCalls != 1 || fake.installCalls != 1 {
			t.Fatalf("expected one probe and one install, got probe=%d install=%d", fake.probeCalls, fake.installCalls)
		}
		commands := strings.Join(fake.sudoCommands, "\n---\n")
		for _, want := range []string{"panel_agent_chrony=missing", "apt_get install -y " + agentTimeSyncPackageName} {
			if !strings.Contains(commands, want) {
				t.Fatalf("expected %q in:\n%s", want, commands)
			}
		}
	})

	t.Run("an installed but unsynchronized clock is repaired", func(t *testing.T) {
		fake := &timeSyncProvisionFakeExec{status: agentTimeSyncStatus{Installed: true, Active: true, Synced: false}}
		svc, _, _ := testServerService(t, fake)
		if err := svc.ensureAgentTimeSync(context.Background(), "task_1", base, remoteops.Runner{Exec: fake}); err != nil {
			t.Fatal(err)
		}
		if fake.installCalls != 1 {
			t.Fatalf("expected the provisioning script to run, got install=%d", fake.installCalls)
		}
	})

	t.Run("a failing probe surfaces", func(t *testing.T) {
		fake := &timeSyncProvisionFakeExec{failProbe: true}
		svc, _, _ := testServerService(t, fake)
		if err := svc.ensureAgentTimeSync(context.Background(), "task_1", base, remoteops.Runner{Exec: fake}); err == nil {
			t.Fatal("expected the probe failure to surface")
		}
		if fake.installCalls != 0 {
			t.Fatalf("a failed probe must not run the install script, got %d", fake.installCalls)
		}
	})

	t.Run("a failing install is reported as agent_time_sync_failed", func(t *testing.T) {
		fake := &timeSyncProvisionFakeExec{failInstall: true}
		svc, _, _ := testServerService(t, fake)
		err := svc.ensureAgentTimeSync(context.Background(), "task_1", base, remoteops.Runner{Exec: fake})
		if err == nil {
			t.Fatal("expected the provisioning failure to surface")
		}
		if !strings.Contains(err.Error(), "clock") {
			t.Fatalf("expected the error to name the clock, got %v", err)
		}
		var panelErr *panelerr.Error
		if !errors.As(err, &panelErr) || panelErr.Code != "agent_time_sync_failed" {
			t.Fatalf("expected agent_time_sync_failed, got %#v", err)
		}
	})

	t.Run("an unavailable remote executor is a validation error", func(t *testing.T) {
		svc, _, _ := testServerService(t, nil)
		err := svc.ensureAgentTimeSync(context.Background(), "task_1", base, remoteops.Runner{})
		var panelErr *panelerr.Error
		if !errors.As(err, &panelErr) || panelErr.Code != "remote_executor_unavailable" {
			t.Fatalf("expected remote_executor_unavailable, got %#v", err)
		}
	})
}
