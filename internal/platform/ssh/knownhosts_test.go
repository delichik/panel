package sshx

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	panelerr "panel/internal/platform/errors"
)

func testSigner(t *testing.T) ssh.Signer {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return signer
}

func TestKnownHostsStoreRecordsFirstKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known_hosts")
	store := newKnownHostsStore(path)
	key := testSigner(t).PublicKey()
	if err := store.verify("example.com:2222", key); err != nil {
		t.Fatalf("first verify: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read known_hosts: %v", err)
	}
	if line := string(b); !strings.Contains(line, "example.com:2222") || !strings.Contains(line, key.Type()) {
		t.Fatalf("known_hosts does not contain the recorded key: %q", line)
	}
}

func TestKnownHostsStoreAcceptsMatchingKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known_hosts")
	key := testSigner(t).PublicKey()
	if err := newKnownHostsStore(path).verify("example.com:2222", key); err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := newKnownHostsStore(path).verify("example.com:2222", key); err != nil {
		t.Fatalf("matching verify: %v", err)
	}
}

func TestKnownHostsStoreRejectsChangedKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known_hosts")
	first := testSigner(t).PublicKey()
	if err := newKnownHostsStore(path).verify("example.com:2222", first); err != nil {
		t.Fatalf("record: %v", err)
	}
	err := newKnownHostsStore(path).verify("example.com:2222", testSigner(t).PublicKey())
	if !errors.Is(err, ErrHostKeyMismatch) {
		t.Fatalf("expected ErrHostKeyMismatch, got %v", err)
	}
	if !strings.Contains(err.Error(), "example.com:2222") || !strings.Contains(err.Error(), "SHA256:") {
		t.Fatalf("mismatch error should mention identity and fingerprints: %v", err)
	}
}

func TestKnownHostsStoreReplaceExistingEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known_hosts")
	store := newKnownHostsStore(path)
	first := testSigner(t).PublicKey()
	second := testSigner(t).PublicKey()
	if err := store.verify("example.com:2222", first); err != nil {
		t.Fatalf("record first key: %v", err)
	}
	if err := store.Replace("example.com:2222", second); err != nil {
		t.Fatalf("replace key: %v", err)
	}
	if err := store.verify("example.com:2222", second); err != nil {
		t.Fatalf("verify replaced key: %v", err)
	}
	if err := store.verify("example.com:2222", first); !errors.Is(err, ErrHostKeyMismatch) {
		t.Fatalf("old key after replace: expected ErrHostKeyMismatch, got %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read known_hosts: %v", err)
	}
	if strings.Count(string(b), "example.com:2222") != 1 {
		t.Fatalf("known_hosts should contain exactly one entry for the identity after replace: %q", b)
	}
	if !strings.Contains(string(b), string(ssh.MarshalAuthorizedKey(second))) {
		t.Fatalf("known_hosts does not contain the replaced key: %q", b)
	}
}

func TestKnownHostsStoreReplaceAddsNewEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known_hosts")
	store := newKnownHostsStore(path)
	key := testSigner(t).PublicKey()
	if err := store.Replace("db.example.com:2222", key); err != nil {
		t.Fatalf("replace new entry: %v", err)
	}
	if err := store.verify("db.example.com:2222", key); err != nil {
		t.Fatalf("verify newly recorded key: %v", err)
	}
}

func TestKnownHostsStoreReplaceWritesAtomically(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known_hosts")
	store := newKnownHostsStore(path)
	alpha := testSigner(t).PublicKey()
	beta := testSigner(t).PublicKey()
	if err := store.verify("alpha.example.com:22", alpha); err != nil {
		t.Fatalf("record alpha: %v", err)
	}
	if err := store.verify("beta.example.com:22", beta); err != nil {
		t.Fatalf("record beta: %v", err)
	}
	replacement := testSigner(t).PublicKey()
	if err := store.Replace("alpha.example.com:22", replacement); err != nil {
		t.Fatalf("replace alpha: %v", err)
	}
	// The persisted file must contain both entries (replacement for alpha,
	// unchanged beta) and no leftover temp files from the atomic write.
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read known_hosts: %v", err)
	}
	text := string(b)
	if strings.Count(text, "alpha.example.com:22") != 1 || strings.Count(text, "beta.example.com:22") != 1 {
		t.Fatalf("known_hosts should contain exactly one line per identity: %q", text)
	}
	if !strings.Contains(text, string(ssh.MarshalAuthorizedKey(replacement))) || !strings.Contains(text, string(ssh.MarshalAuthorizedKey(beta))) {
		t.Fatalf("known_hosts content mismatch: %q", text)
	}
	leftovers, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".known_hosts-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(leftovers) != 0 {
		t.Fatalf("atomic write left temp files behind: %#v", leftovers)
	}
}

// testSSHServer is a minimal SSH server used to exercise the executor end to
// end. Its host key can be swapped to simulate a changed server key.
type testSSHServer struct {
	listener   net.Listener
	hostKey    atomic.Value // ssh.Signer
	silentExec atomic.Bool
	closeOnce  sync.Once
}

func newTestSSHServer(t *testing.T, signer ssh.Signer) *testSSHServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := &testSSHServer{listener: ln}
	srv.hostKey.Store(signer)
	go srv.serve()
	t.Cleanup(srv.Close)
	return srv
}

func (s *testSSHServer) Close() {
	s.closeOnce.Do(func() {
		_ = s.listener.Close()
	})
}

func (s *testSSHServer) serve() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.handleConn(conn)
	}
}

func (s *testSSHServer) handleConn(conn net.Conn) {
	defer conn.Close()
	cfg := &ssh.ServerConfig{NoClientAuth: true}
	cfg.AddHostKey(s.hostKey.Load().(ssh.Signer))
	sshConn, chans, reqs, err := ssh.NewServerConn(conn, cfg)
	if err != nil {
		return
	}
	defer sshConn.Close()
	go ssh.DiscardRequests(reqs)
	for newChannel := range chans {
		if newChannel.ChannelType() != "session" {
			_ = newChannel.Reject(ssh.UnknownChannelType, "unsupported channel type")
			continue
		}
		ch, chReqs, err := newChannel.Accept()
		if err != nil {
			continue
		}
		go handleSession(ch, chReqs, s.silentExec.Load())
	}
}

func handleSession(ch ssh.Channel, reqs <-chan *ssh.Request, silent bool) {
	defer ch.Close()
	for req := range reqs {
		if req.Type == "exec" {
			if silent {
				continue
			}
			if req.WantReply {
				_ = req.Reply(true, nil)
			}
			status := struct{ Status uint32 }{0}
			_, _ = ch.SendRequest("exit-status", false, ssh.Marshal(&status))
			return
		}
		if req.WantReply {
			_ = req.Reply(true, nil)
		}
	}
}

func splitTestAddr(addr string) (string, int) {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		panic(err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		panic(err)
	}
	return host, port
}

func testTarget(host string, port int) Target {
	return Target{Host: host, Port: port, Username: "test", CredentialID: "c1", PrivilegeMode: PrivilegeModeRoot}
}

func testPasswordResolver() fakeResolver {
	return fakeResolver{cred: ResolvedCredential{Type: CredentialTypePassword, Password: "secret", Username: "test"}}
}

func TestExecutorTOFURecordsAndVerifiesHostKey(t *testing.T) {
	knownHosts := filepath.Join(t.TempDir(), "known_hosts")
	srv := newTestSSHServer(t, testSigner(t))
	host, port := splitTestAddr(srv.listener.Addr().String())

	executor := NewSSHExecutorWithOptions(testPasswordResolver(), 10*time.Second, WithKnownHosts(knownHosts))
	target := testTarget(host, port)
	spec := CommandSpec{Command: "true", Timeout: 5 * time.Second}

	if _, err := executor.Exec(context.Background(), target, spec); err != nil {
		t.Fatalf("first connection: %v", err)
	}
	b, err := os.ReadFile(knownHosts)
	if err != nil {
		t.Fatalf("known_hosts file: %v", err)
	}
	if !strings.Contains(string(b), net.JoinHostPort(host, strconv.Itoa(port))) {
		t.Fatalf("known_hosts missing host identity %q: %q", net.JoinHostPort(host, strconv.Itoa(port)), b)
	}

	if _, err := executor.Exec(context.Background(), target, spec); err != nil {
		t.Fatalf("matching second connection: %v", err)
	}

	srv.hostKey.Store(testSigner(t))
	_, err = executor.Exec(context.Background(), target, spec)
	var perr *panelerr.Error
	if !errors.As(err, &perr) {
		t.Fatalf("expected panel error, got %v", err)
	}
	if perr.Code != "ssh_host_key_mismatch" {
		t.Fatalf("expected ssh_host_key_mismatch, got %q (%v)", perr.Code, perr.Message)
	}
}

func TestUploadTimesOutWhenRemoteHangs(t *testing.T) {
	knownHosts := filepath.Join(t.TempDir(), "known_hosts")
	srv := newTestSSHServer(t, testSigner(t))
	srv.silentExec.Store(true)
	host, port := splitTestAddr(srv.listener.Addr().String())

	local := filepath.Join(t.TempDir(), "payload.txt")
	if err := os.WriteFile(local, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	executor := NewSSHExecutorWithOptions(testPasswordResolver(), 400*time.Millisecond, WithKnownHosts(knownHosts))

	started := time.Now()
	err := executor.Upload(context.Background(), testTarget(host, port), UploadSpec{LocalPath: local, RemotePath: "/tmp/payload.txt"})
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("upload did not honor timeout, took %v", elapsed)
	}
	var perr *panelerr.Error
	if !errors.As(err, &perr) || perr.Code != "remote_timeout" {
		t.Fatalf("expected remote_timeout error, got %v", err)
	}
}
