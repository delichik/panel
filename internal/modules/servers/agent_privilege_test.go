package server

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	storage "panel/internal/platform/database"
	panelerr "panel/internal/platform/errors"
	"panel/internal/platform/ssh"
)

// privilegeFakeExec 模拟特权探测涉及的最小命令集，并记录探测次数，用于断言
// “已确认 root 时不再尝试 sudo”以及“已有特权时不发起任何远端探测”。
type privilegeFakeExec struct {
	idStdout  string
	idErr     error
	sudoErr   error
	sudoExit  int
	execCalls int
	idCalls   int
	sudoCalls int
}

func (f *privilegeFakeExec) Exec(_ context.Context, _ sshx.Target, command sshx.CommandSpec) (sshx.CommandResult, error) {
	f.execCalls++
	switch {
	case strings.TrimSpace(command.Command) == "id -u":
		f.idCalls++
		if f.idErr != nil {
			return sshx.CommandResult{}, f.idErr
		}
		return sshx.CommandResult{Stdout: f.idStdout, ExitCode: 0}, nil
	case strings.Contains(command.Command, "cat /etc/os-release"):
		return sshx.CommandResult{Stdout: "ID=debian\nVERSION_ID=\"13\"\nPRETTY_NAME=\"Debian GNU/Linux 13\"\n", ExitCode: 0}, nil
	case strings.TrimSpace(command.Command) == "uname -m":
		return sshx.CommandResult{Stdout: "x86_64\n", ExitCode: 0}, nil
	default:
		return sshx.CommandResult{ExitCode: 0}, nil
	}
}

func (f *privilegeFakeExec) ExecSudo(_ context.Context, _ sshx.Target, _ sshx.CommandSpec) (sshx.CommandResult, error) {
	f.sudoCalls++
	if f.sudoErr != nil {
		return sshx.CommandResult{ExitCode: 1}, f.sudoErr
	}
	return sshx.CommandResult{ExitCode: f.sudoExit}, nil
}

func (f *privilegeFakeExec) Upload(context.Context, sshx.Target, sshx.UploadSpec) error { return nil }

func (f *privilegeFakeExec) Download(context.Context, sshx.Target, sshx.DownloadSpec) error {
	return nil
}

func insertPrivilegeTestServer(t *testing.T, store *storage.Store, serverID, mode string, reachable bool) {
	t.Helper()
	reachableInt := 0
	if reachable {
		reachableInt = 1
	}
	_, err := store.AppDB().Exec(`INSERT INTO servers(id,name,host,port,ssh_username,credential_id,os_id,os_version_id,os_supported,reachable,sudo_passwordless,privilege_mode,privilege_last_checked_at,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		serverID, "privilege-test", "127.0.0.1", 22, "du", "cred_1", "debian", "13", 1, reachableInt, 0, mode,
		"2026-01-01T00:00:00Z", "2026-01-01T00:00:00Z", "2026-01-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
}

func assertPanelErrorCode(t *testing.T, err error, wantCode string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected %s error, got nil", wantCode)
	}
	var panelErr *panelerr.Error
	if !errors.As(err, &panelErr) {
		t.Fatalf("expected *panelerr.Error, got %T: %v", err, err)
	}
	if panelErr.Code != wantCode {
		t.Fatalf("expected error code %q, got %q (%v)", wantCode, panelErr.Code, err)
	}
}

func TestDetectPrivilegeRootSkipsSudoProbe(t *testing.T) {
	exec := &privilegeFakeExec{idStdout: "0\n"}
	svc, _, _ := testServerService(t, exec)
	mode, err := svc.detectPrivilege(context.Background(), sshx.Target{ServerID: "srv_1", Host: "127.0.0.1", Port: 22, Username: "root", CredentialID: "cred_1"})
	if err != nil {
		t.Fatalf("expected root detection to succeed, got %v", err)
	}
	if mode != sshx.PrivilegeModeRoot {
		t.Fatalf("expected root, got %q", mode)
	}
	if exec.idCalls != 1 {
		t.Fatalf("expected exactly one id -u probe, got %d", exec.idCalls)
	}
	if exec.sudoCalls != 0 {
		t.Fatalf("root servers must not probe sudo, got %d sudo probes", exec.sudoCalls)
	}
}

func TestDetectPrivilegeTransportFailureIsUnknownNotNone(t *testing.T) {
	exec := &privilegeFakeExec{idErr: panelerr.BadGateway("ssh_connection_failed", "SSH connection failed")}
	svc, _, _ := testServerService(t, exec)
	mode, err := svc.detectPrivilege(context.Background(), sshx.Target{Host: "127.0.0.1", Port: 22})
	if mode != "" {
		t.Fatalf("expected unknown mode (empty) on transport failure, got %q", mode)
	}
	assertPanelErrorCode(t, err, "ssh_connection_failed")
}

func TestDetectPrivilegeSudoDeniedIsConfirmedNone(t *testing.T) {
	exec := &privilegeFakeExec{idStdout: "1000\n", sudoErr: panelerr.BadGateway("remote_command_failed", "Remote command failed")}
	svc, _, _ := testServerService(t, exec)
	mode, err := svc.detectPrivilege(context.Background(), sshx.Target{Host: "127.0.0.1", Port: 22})
	if mode != sshx.PrivilegeModeNone {
		t.Fatalf("expected confirmed none when sudo exits non-zero, got %q", mode)
	}
	assertPanelErrorCode(t, err, "remote_command_failed")
	if exec.sudoCalls != 1 {
		t.Fatalf("expected one sudo probe, got %d", exec.sudoCalls)
	}
}

func TestDetectPrivilegeSudoTransportFailureIsUnknown(t *testing.T) {
	exec := &privilegeFakeExec{idStdout: "1000\n", sudoErr: panelerr.BadGateway("ssh_connection_failed", "SSH connection failed")}
	svc, _, _ := testServerService(t, exec)
	mode, err := svc.detectPrivilege(context.Background(), sshx.Target{Host: "127.0.0.1", Port: 22})
	if mode != "" {
		t.Fatalf("expected unknown mode (empty) on sudo transport failure, got %q", mode)
	}
	assertPanelErrorCode(t, err, "ssh_connection_failed")
}

func TestDetectPrivilegePasswordlessSudo(t *testing.T) {
	exec := &privilegeFakeExec{idStdout: "1000\n"}
	svc, _, _ := testServerService(t, exec)
	mode, err := svc.detectPrivilege(context.Background(), sshx.Target{Host: "127.0.0.1", Port: 22})
	if err != nil {
		t.Fatalf("expected passwordless sudo, got error %v", err)
	}
	if mode != sshx.PrivilegeModeSudo {
		t.Fatalf("expected passwordless_sudo, got %q", mode)
	}
}

func TestTestConnectivityKeepsExistingPrivilegeOnTransientProbeFailure(t *testing.T) {
	exec := &privilegeFakeExec{idErr: panelerr.BadGateway("ssh_connection_failed", "SSH connection failed")}
	svc, _, store := testServerService(t, exec)
	insertPrivilegeTestServer(t, store, "srv_transient", sshx.PrivilegeModeRoot, true)

	checked, err := svc.TestConnectivity(context.Background(), "srv_transient")
	if err != nil {
		t.Fatalf("connectivity test must not fail on an unknown privilege probe: %v", err)
	}
	if !checked.Reachable {
		t.Fatalf("expected reachable server after successful transport probe, got %#v", checked)
	}
	if checked.Privilege.Mode != sshx.PrivilegeModeRoot || !checked.Privilege.Privileged {
		t.Fatalf("transient probe failure must not downgrade privilege mode, got %#v", checked.Privilege)
	}
	wantCheckedAt, err := time.Parse(time.RFC3339, "2026-01-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if checked.Privilege.LastCheckedAt == nil || !checked.Privilege.LastCheckedAt.Equal(wantCheckedAt) {
		t.Fatalf("transient probe failure must keep the previous privilege timestamp, got %#v", checked.Privilege.LastCheckedAt)
	}
}

func TestTestConnectivityPersistsConfirmedNone(t *testing.T) {
	exec := &privilegeFakeExec{idStdout: "1000\n", sudoErr: panelerr.BadGateway("remote_command_failed", "Remote command failed")}
	svc, _, store := testServerService(t, exec)
	insertPrivilegeTestServer(t, store, "srv_sudo_denied", sshx.PrivilegeModeSudo, true)

	checked, err := svc.TestConnectivity(context.Background(), "srv_sudo_denied")
	if err != nil {
		t.Fatalf("connectivity test failed: %v", err)
	}
	if checked.Privilege.Mode != sshx.PrivilegeModeNone || checked.Privilege.Privileged {
		t.Fatalf("expected confirmed none to be persisted, got %#v", checked.Privilege)
	}
}

func TestRefreshPrivilegeForDeployHealsLegacyNoneRootServer(t *testing.T) {
	exec := &privilegeFakeExec{idStdout: "0\n"}
	svc, _, store := testServerService(t, exec)
	insertPrivilegeTestServer(t, store, "srv_heal", sshx.PrivilegeModeNone, true)

	srv, err := svc.Get(context.Background(), "srv_heal")
	if err != nil {
		t.Fatal(err)
	}
	refreshed, err := svc.refreshPrivilegeForDeploy(context.Background(), srv)
	if err != nil {
		t.Fatalf("refresh privilege failed: %v", err)
	}
	if refreshed.Privilege.Mode != sshx.PrivilegeModeRoot || !refreshed.Privilege.Privileged {
		t.Fatalf("expected legacy none root server to heal to root, got %#v", refreshed.Privilege)
	}
	if exec.idCalls != 1 || exec.sudoCalls != 0 {
		t.Fatalf("expected a single id -u probe and no sudo probe, got id=%d sudo=%d", exec.idCalls, exec.sudoCalls)
	}
	persisted, err := svc.Get(context.Background(), "srv_heal")
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Privilege.Mode != sshx.PrivilegeModeRoot {
		t.Fatalf("expected healed privilege mode to be persisted, got %#v", persisted.Privilege)
	}
}

func TestRefreshPrivilegeForDeploySkipsProbeWhenPrivileged(t *testing.T) {
	exec := &privilegeFakeExec{idStdout: "0\n"}
	svc, _, store := testServerService(t, exec)
	insertPrivilegeTestServer(t, store, "srv_root", sshx.PrivilegeModeRoot, true)

	srv, err := svc.Get(context.Background(), "srv_root")
	if err != nil {
		t.Fatal(err)
	}
	refreshed, err := svc.refreshPrivilegeForDeploy(context.Background(), srv)
	if err != nil {
		t.Fatalf("refresh privilege failed: %v", err)
	}
	if refreshed.Privilege.Mode != sshx.PrivilegeModeRoot {
		t.Fatalf("expected existing root privilege to be returned unchanged, got %#v", refreshed.Privilege)
	}
	if exec.execCalls != 0 || exec.idCalls != 0 || exec.sudoCalls != 0 {
		t.Fatalf("privileged servers must not be probed again, got exec=%d id=%d sudo=%d", exec.execCalls, exec.idCalls, exec.sudoCalls)
	}
}

func TestRefreshPrivilegeForDeployKeepsConfirmedNoneWithoutError(t *testing.T) {
	exec := &privilegeFakeExec{idStdout: "1000\n", sudoErr: panelerr.BadGateway("remote_command_failed", "Remote command failed")}
	svc, _, store := testServerService(t, exec)
	insertPrivilegeTestServer(t, store, "srv_confirmed_none", sshx.PrivilegeModeNone, true)

	srv, err := svc.Get(context.Background(), "srv_confirmed_none")
	if err != nil {
		t.Fatal(err)
	}
	refreshed, err := svc.refreshPrivilegeForDeploy(context.Background(), srv)
	if err != nil {
		t.Fatalf("confirmed none (sudo denied) must not surface as a probe failure: %v", err)
	}
	if refreshed.Privilege.Mode != sshx.PrivilegeModeNone || refreshed.Privilege.Privileged {
		t.Fatalf("expected confirmed none to be returned unchanged, got %#v", refreshed.Privilege)
	}
	if exec.sudoCalls != 1 {
		t.Fatalf("expected one sudo probe, got %d", exec.sudoCalls)
	}
}

func TestRefreshPrivilegeForDeployKeepsNoneOnUnknownProbe(t *testing.T) {
	exec := &privilegeFakeExec{idErr: panelerr.BadGateway("ssh_connection_failed", "SSH connection failed")}
	svc, _, store := testServerService(t, exec)
	insertPrivilegeTestServer(t, store, "srv_unknown", sshx.PrivilegeModeNone, true)

	srv, err := svc.Get(context.Background(), "srv_unknown")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.refreshPrivilegeForDeploy(context.Background(), srv); err == nil {
		t.Fatal("expected transient probe failure to surface")
	}
	persisted, err := svc.Get(context.Background(), "srv_unknown")
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Privilege.Mode != sshx.PrivilegeModeNone {
		t.Fatalf("unknown probe must not rewrite privilege mode, got %#v", persisted.Privilege)
	}
}

func TestProbeConnectivityShowsUnknownPrivilegeAsNoneWithError(t *testing.T) {
	exec := &privilegeFakeExec{idErr: panelerr.BadGateway("ssh_connection_failed", "SSH connection failed")}
	svc, _, _ := testServerService(t, exec)

	result, err := svc.ProbeConnectivity(context.Background(), SaveRequest{IPv4: "127.0.0.1", Port: 22, SSHUsername: "du", CredentialID: "cred_1"})
	if err != nil {
		t.Fatalf("probe failed: %v", err)
	}
	if !result.Reachable || result.OS.ID != "debian" {
		t.Fatalf("expected reachable probe with detected OS, got %#v", result)
	}
	if result.PrivilegeMode != sshx.PrivilegeModeNone || result.Privileged || result.Root || result.PasswordlessSudo {
		t.Fatalf("unknown privilege must display as none, got %#v", result)
	}
	if !strings.Contains(result.PasswordlessSudoText, "SSH connection failed") {
		t.Fatalf("expected probe error text to be preserved, got %q", result.PasswordlessSudoText)
	}
}
