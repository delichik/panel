package sshx

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	panelerr "panel/internal/platform/errors"

	"bytes"
	"context"
	"testing"
)

type fakeResolver struct{ cred ResolvedCredential }

func (f fakeResolver) Resolve(ctx context.Context, id string) (ResolvedCredential, error) {
	return f.cred, nil
}

func TestTrustHostKeyReplacesChangedKey(t *testing.T) {
	knownHosts := filepath.Join(t.TempDir(), "known_hosts")
	srv := newTestSSHServer(t, testSigner(t))
	host, port := splitTestAddr(srv.listener.Addr().String())
	executor := NewSSHExecutorWithOptions(testPasswordResolver(), 10*time.Second, WithKnownHosts(knownHosts))
	target := testTarget(host, port)
	spec := CommandSpec{Command: "true", Timeout: 5 * time.Second}

	if _, err := executor.Exec(context.Background(), target, spec); err != nil {
		t.Fatalf("first connection: %v", err)
	}
	srv.hostKey.Store(testSigner(t))
	if _, err := executor.Exec(context.Background(), target, spec); !isPanelCode(err, "ssh_host_key_mismatch") {
		t.Fatalf("expected ssh_host_key_mismatch after key swap, got %v", err)
	}

	if err := executor.TrustHostKey(context.Background(), target); err != nil {
		t.Fatalf("trust host key: %v", err)
	}
	if _, err := executor.Exec(context.Background(), target, spec); err != nil {
		t.Fatalf("connection after trust should accept the new key: %v", err)
	}
	b, err := os.ReadFile(knownHosts)
	if err != nil {
		t.Fatalf("read known_hosts: %v", err)
	}
	if strings.Count(string(b), net.JoinHostPort(host, strconv.Itoa(port))) != 1 {
		t.Fatalf("known_hosts should keep a single entry for the identity: %q", b)
	}
}

func TestTrustHostKeyDisabledWhenVerificationDisabled(t *testing.T) {
	// NewSSHExecutorWithOptions always installs a default store, so build the
	// executor directly to exercise the knownHosts == nil branch.
	executor := &SSHExecutor{resolver: testPasswordResolver(), defaultTimeout: 10 * time.Second}
	err := executor.TrustHostKey(context.Background(), testTarget("127.0.0.1", 22))
	if !isPanelCode(err, "host_key_verification_disabled") {
		t.Fatalf("expected host_key_verification_disabled, got %v", err)
	}
}

func TestTrustHostKeyConnectionFailure(t *testing.T) {
	knownHosts := filepath.Join(t.TempDir(), "known_hosts")
	executor := NewSSHExecutorWithOptions(testPasswordResolver(), 2*time.Second, WithKnownHosts(knownHosts))

	// Accept the TCP connection, then drop it before SSH authentication. This
	// remains a transport failure and must not be reported as bad credentials.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, acceptErr := ln.Accept()
			if acceptErr != nil {
				return
			}
			_ = conn.Close()
		}
	}()
	host, port := splitTestAddr(ln.Addr().String())

	err = executor.TrustHostKey(context.Background(), testTarget(host, port))
	if !isPanelCode(err, "ssh_connection_failed") {
		t.Fatalf("expected ssh_connection_failed, got %v", err)
	}
}

func TestTrustHostKeyAuthFailure(t *testing.T) {
	knownHosts := filepath.Join(t.TempDir(), "known_hosts")
	srv := newTestSSHServer(t, testSigner(t))
	host, port := splitTestAddr(srv.listener.Addr().String())

	resolver := fakeResolver{cred: ResolvedCredential{
		Type:     CredentialTypePassword,
		Username: "test",
		Password: "wrong-password",
	}}
	executor := NewSSHExecutorWithOptions(resolver, 2*time.Second, WithKnownHosts(knownHosts))
	err := executor.TrustHostKey(context.Background(), testTarget(host, port))
	if !isPanelCode(err, "ssh_auth_failed") {
		t.Fatalf("expected ssh_auth_failed, got %v", err)
	}
}

func TestPasswordAuthMethod(t *testing.T) {
	_, err := authMethod(ResolvedCredential{Type: CredentialTypePassword, Password: "secret"})
	if err != nil {
		t.Fatalf("password auth method: %v", err)
	}
}

func TestPrivateKeyAuthMethodRejectsInvalidKey(t *testing.T) {
	_, err := authMethod(ResolvedCredential{Type: CredentialTypePrivateKey, PrivateKey: []byte("bad")})
	if err == nil {
		t.Fatal("expected invalid private key error")
	}
}

func TestSudoCommandWrapsNonInteractive(t *testing.T) {
	got := privilegedCommand(PrivilegeModeSudo, "apt-get update")
	if got != "sudo -n sh -c 'apt-get update'" {
		t.Fatalf("unexpected sudo command: %s", got)
	}
}

func TestPrivilegedCommandRunsDirectlyForRoot(t *testing.T) {
	got := privilegedCommand(PrivilegeModeRoot, "apt-get update")
	if got != "apt-get update" {
		t.Fatalf("unexpected root command: %s", got)
	}
}

func TestStreamWriterEmitsLinesAndFlushesPartialLine(t *testing.T) {
	var buf bytes.Buffer
	var lines []string
	writer := newStreamWriter(&buf, func(line string) {
		lines = append(lines, line)
	})

	if _, err := writer.Write([]byte("one\ntwo\rthree")); err != nil {
		t.Fatal(err)
	}
	writer.Flush()

	if buf.String() != "one\ntwo\rthree" {
		t.Fatalf("unexpected buffered output: %q", buf.String())
	}
	want := []string{"one", "two", "three"}
	if len(lines) != len(want) {
		t.Fatalf("expected lines %#v, got %#v", want, lines)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("line %d = %q, want %q", i, lines[i], want[i])
		}
	}
}

func isPanelCode(err error, code string) bool {
	var perr *panelerr.Error
	return errors.As(err, &perr) && perr.Code == code
}
