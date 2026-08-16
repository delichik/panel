package sshx

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	panelerr "panel/internal/platform/errors"
)

type SSHExecutor struct {
	resolver                     CredentialResolver
	defaultTimeout               time.Duration
	timeoutProvider              func() time.Duration
	knownHosts                   *knownHostsStore
	knownHostsExplicitlyDisabled bool
}

// SSHExecutorOption configures an SSHExecutor at construction time.
type SSHExecutorOption func(*SSHExecutor)

// WithKnownHosts enables host key TOFU verification using the given
// known_hosts-style file. An empty path disables verification.
func WithKnownHosts(path string) SSHExecutorOption {
	return func(e *SSHExecutor) {
		if path == "" {
			e.knownHosts = nil
			e.knownHostsExplicitlyDisabled = true
			return
		}
		e.knownHosts = newKnownHostsStore(path)
	}
}

// NewSSHExecutorWithOptions builds an executor with functional options. Host
// key TOFU verification is enabled by default using the known_hosts file
// under the data root; see WithKnownHosts. Tests that do not want TOFU must
// provide a temporary known_hosts path.
func NewSSHExecutorWithOptions(resolver CredentialResolver, defaultTimeout time.Duration, options ...SSHExecutorOption) *SSHExecutor {
	e := &SSHExecutor{resolver: resolver, defaultTimeout: defaultTimeout}
	for _, opt := range options {
		opt(e)
	}
	if e.knownHosts == nil && !e.knownHostsExplicitlyDisabled {
		e.knownHosts = newKnownHostsStore(defaultKnownHostsPath())
	}
	return e
}

func NewSSHExecutorWithTimeoutProvider(resolver CredentialResolver, defaultTimeout time.Duration, provider func() time.Duration, options ...SSHExecutorOption) *SSHExecutor {
	e := NewSSHExecutorWithOptions(resolver, defaultTimeout, options...)
	e.timeoutProvider = provider
	return e
}

func (e *SSHExecutor) Exec(ctx context.Context, target Target, command CommandSpec) (CommandResult, error) {
	return e.exec(ctx, target, command)
}

func (e *SSHExecutor) ExecSudo(ctx context.Context, target Target, command CommandSpec) (CommandResult, error) {
	command.Command = privilegedCommand(target.PrivilegeMode, command.Command)
	return e.exec(ctx, target, command)
}

func privilegedCommand(mode, command string) string {
	if mode == PrivilegeModeRoot {
		return command
	}
	return "sudo -n sh -c " + shellQuote(command)
}

func (e *SSHExecutor) Upload(ctx context.Context, target Target, transfer UploadSpec) error {
	if strings.TrimSpace(transfer.LocalPath) == "" || strings.TrimSpace(transfer.RemotePath) == "" {
		return panelerr.Validation("upload_path_required", "Upload localPath and remotePath are required")
	}
	file, err := os.Open(transfer.LocalPath)
	if err != nil {
		return err
	}
	defer file.Close()

	timeout := e.timeout()
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	client, err := e.dial(ctx, target)
	if err != nil {
		return err
	}
	defer client.Close()
	session, err := client.NewSession()
	if err != nil {
		return panelerr.BadGateway("remote_session_failed", "Failed to open SSH session")
	}
	defer session.Close()
	session.Stdin = file
	remotePath := strings.TrimSpace(transfer.RemotePath)
	command := "mkdir -p " + shellQuote(path.Dir(remotePath)) + " && cat > " + shellQuote(remotePath)
	errCh := make(chan error, 1)
	go func() {
		errCh <- session.Run(command)
	}()

	select {
	case <-ctx.Done():
		_ = session.Close()
		<-errCh
		return panelerr.Timeout("Remote upload timed out")
	case err := <-errCh:
		if err != nil {
			return panelerr.BadGateway("remote_upload_failed", "Remote upload failed")
		}
		return nil
	}
}

// TrustHostKey connects to target once and records the host public key it
// presents into the known_hosts store, replacing any previously recorded key.
// This is an explicit administrator trust operation: the HostKeyCallback
// accepts whatever key the server presents and never fails on mismatch. The
// connection still requires a successful TCP dial and SSH authentication so
// the operation cannot silently trust an unreachable or unauthenticated peer.
func (e *SSHExecutor) TrustHostKey(ctx context.Context, target Target) error {
	if e.knownHosts == nil {
		return panelerr.Validation("host_key_verification_disabled", "Host key verification is disabled")
	}
	timeout := e.timeout()
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	resolved, err := e.resolver.Resolve(ctx, target.CredentialID)
	if err != nil {
		return err
	}
	auth, err := authMethod(resolved)
	if err != nil {
		return err
	}
	user := target.Username
	if user == "" {
		user = resolved.Username
	}
	address := net.JoinHostPort(target.Host, strconv.Itoa(target.Port))
	var presented ssh.PublicKey
	cfg := &ssh.ClientConfig{
		User:    user,
		Auth:    []ssh.AuthMethod{auth},
		Timeout: e.timeout(),
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			presented = key
			return nil
		},
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", address)
	if err != nil {
		return panelerr.BadGateway("ssh_connection_failed", "SSH connection failed")
	}
	c, _, _, err := ssh.NewClientConn(conn, address, cfg)
	if err != nil {
		_ = conn.Close()
		return panelerr.BadGateway("ssh_auth_failed", "SSH authentication failed")
	}
	_ = c.Close()
	if presented == nil {
		return panelerr.BadGateway("ssh_host_key_unavailable", "SSH server did not present a host key")
	}
	identity := net.JoinHostPort(target.Host, strconv.Itoa(target.Port))
	if err := e.knownHosts.Replace(identity, presented); err != nil {
		return panelerr.BadGateway("ssh_host_key_verification_failed", err.Error())
	}
	return nil
}

func (e *SSHExecutor) Download(ctx context.Context, target Target, transfer DownloadSpec) error {
	return panelerr.Validation("download_not_implemented", "SFTP download is reserved for later phases")
}

func (e *SSHExecutor) exec(ctx context.Context, target Target, command CommandSpec) (CommandResult, error) {
	timeout := command.Timeout
	if timeout == 0 {
		timeout = e.timeout()
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	started := time.Now().UTC()
	result := CommandResult{StartedAt: started}

	client, err := e.dial(ctx, target)
	if err != nil {
		result.FinishedAt = time.Now().UTC()
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			result.TimedOut = true
			return result, panelerr.Timeout("Remote command timed out before connection completed")
		}
		return result, err
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		result.FinishedAt = time.Now().UTC()
		return result, panelerr.BadGateway("remote_session_failed", "Failed to open SSH session")
	}
	defer session.Close()

	var stdout, stderr bytes.Buffer
	stdoutWriter := newStreamWriter(&stdout, command.OnStdout)
	stderrWriter := newStreamWriter(&stderr, command.OnStderr)
	session.Stdout = stdoutWriter
	session.Stderr = stderrWriter
	errCh := make(chan error, 1)
	go func() {
		errCh <- session.Run(command.Command)
	}()

	select {
	case <-ctx.Done():
		_ = session.Close()
		<-errCh
		stdoutWriter.Flush()
		stderrWriter.Flush()
		result.Stdout = stdout.String()
		result.Stderr = stderr.String()
		result.FinishedAt = time.Now().UTC()
		result.TimedOut = true
		return result, panelerr.Timeout("Remote command timed out")
	case err := <-errCh:
		stdoutWriter.Flush()
		stderrWriter.Flush()
		result.Stdout = stdout.String()
		result.Stderr = stderr.String()
		result.FinishedAt = time.Now().UTC()
		if err != nil {
			var exitErr *ssh.ExitError
			if errors.As(err, &exitErr) {
				result.ExitCode = exitErr.ExitStatus()
				return result, panelerr.BadGateway("remote_command_failed", "Remote command failed")
			}
			return result, panelerr.BadGateway("remote_command_failed", "Remote command failed")
		}
		return result, nil
	}
}

func (e *SSHExecutor) dial(ctx context.Context, target Target) (*ssh.Client, error) {
	resolved, err := e.resolver.Resolve(ctx, target.CredentialID)
	if err != nil {
		return nil, err
	}
	auth, err := authMethod(resolved)
	if err != nil {
		return nil, err
	}
	user := target.Username
	if user == "" {
		user = resolved.Username
	}
	address := net.JoinHostPort(target.Host, strconv.Itoa(target.Port))
	cfg := &ssh.ClientConfig{User: user, Auth: []ssh.AuthMethod{auth}, HostKeyCallback: e.hostKeyCallback(address), Timeout: e.timeout()}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, panelerr.BadGateway("ssh_connection_failed", "SSH connection failed")
	}
	c, chans, reqs, err := ssh.NewClientConn(conn, address, cfg)
	if err != nil {
		_ = conn.Close()
		switch {
		case errors.Is(err, ErrHostKeyMismatch):
			return nil, panelerr.BadGateway("ssh_host_key_mismatch", sshHandshakeMessage(err))
		case errors.Is(err, ErrHostKeyVerification):
			return nil, panelerr.BadGateway("ssh_host_key_verification_failed", sshHandshakeMessage(err))
		default:
			return nil, panelerr.BadGateway("ssh_auth_failed", "SSH authentication failed")
		}
	}
	return ssh.NewClient(c, chans, reqs), nil
}

// sshHandshakeMessage strips the "ssh: handshake failed: " wrapper that
// x/crypto adds around host key errors, so the stable error text ("ssh host key
// mismatch: …") reaches the i18n prefix translation and the servers
// HostKeyMismatch flag unchanged.
func sshHandshakeMessage(err error) string {
	return strings.TrimPrefix(err.Error(), "ssh: handshake failed: ")
}

func (e *SSHExecutor) hostKeyCallback(identity string) ssh.HostKeyCallback {
	if e.knownHosts == nil {
		return ssh.InsecureIgnoreHostKey()
	}
	return func(_ string, _ net.Addr, key ssh.PublicKey) error {
		return e.knownHosts.verify(identity, key)
	}
}

func (e *SSHExecutor) timeout() time.Duration {
	if e.timeoutProvider != nil {
		if timeout := e.timeoutProvider(); timeout > 0 {
			return timeout
		}
	}
	return e.defaultTimeout
}

func authMethod(c ResolvedCredential) (ssh.AuthMethod, error) {
	switch c.Type {
	case CredentialTypePassword:
		return ssh.Password(c.Password), nil
	case CredentialTypePrivateKey:
		var signer ssh.Signer
		var err error
		if c.Passphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(c.PrivateKey, []byte(c.Passphrase))
		} else {
			signer, err = ssh.ParsePrivateKey(c.PrivateKey)
		}
		if err != nil {
			return nil, panelerr.Validation("private_key_invalid", "Private key could not be parsed")
		}
		return ssh.PublicKeys(signer), nil
	default:
		return nil, panelerr.Validation("credential_type_invalid", "Unsupported credential type")
	}
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

type streamWriter struct {
	dst      io.Writer
	onLine   func(string)
	pending  strings.Builder
	lastByte byte
}

func newStreamWriter(dst io.Writer, onLine func(string)) *streamWriter {
	return &streamWriter{dst: dst, onLine: onLine}
}

func (w *streamWriter) Write(p []byte) (int, error) {
	n, err := w.dst.Write(p)
	if w.onLine == nil {
		return n, err
	}
	for _, b := range p {
		switch b {
		case '\n':
			if w.lastByte != '\r' {
				w.emit()
			}
		case '\r':
			w.emit()
		default:
			w.pending.WriteByte(b)
		}
		w.lastByte = b
	}
	return n, err
}

func (w *streamWriter) Flush() {
	if w.onLine != nil && w.pending.Len() > 0 {
		w.emit()
	}
}

func (w *streamWriter) emit() {
	line := strings.TrimSpace(w.pending.String())
	w.pending.Reset()
	if line != "" {
		w.onLine(line)
	}
}
