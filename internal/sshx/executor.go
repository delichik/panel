package sshx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"panel/internal/credential"
	"panel/internal/panelerr"
)

type SSHExecutor struct {
	resolver       CredentialResolver
	defaultTimeout time.Duration
}

func NewSSHExecutor(resolver CredentialResolver, defaultTimeout time.Duration) *SSHExecutor {
	return &SSHExecutor{resolver: resolver, defaultTimeout: defaultTimeout}
}

func (e *SSHExecutor) Exec(ctx context.Context, target Target, command CommandSpec) (CommandResult, error) {
	return e.exec(ctx, target, command)
}

func (e *SSHExecutor) ExecSudo(ctx context.Context, target Target, command CommandSpec) (CommandResult, error) {
	command.Command = "sudo -n sh -c " + shellQuote(command.Command)
	return e.exec(ctx, target, command)
}

func (e *SSHExecutor) Upload(ctx context.Context, target Target, transfer UploadSpec) error {
	return panelerr.Validation("upload_not_implemented", "SFTP upload is reserved for later phases")
}

func (e *SSHExecutor) Download(ctx context.Context, target Target, transfer DownloadSpec) error {
	return panelerr.Validation("download_not_implemented", "SFTP download is reserved for later phases")
}

func (e *SSHExecutor) exec(ctx context.Context, target Target, command CommandSpec) (CommandResult, error) {
	timeout := command.Timeout
	if timeout == 0 {
		timeout = e.defaultTimeout
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
	session.Stdout = &stdout
	session.Stderr = &stderr
	errCh := make(chan error, 1)
	go func() {
		errCh <- session.Run(command.Command)
	}()

	select {
	case <-ctx.Done():
		_ = session.Close()
		result.Stdout = stdout.String()
		result.Stderr = stderr.String()
		result.FinishedAt = time.Now().UTC()
		result.TimedOut = true
		return result, panelerr.Timeout("Remote command timed out")
	case err := <-errCh:
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
	cfg := &ssh.ClientConfig{User: user, Auth: []ssh.AuthMethod{auth}, HostKeyCallback: ssh.InsecureIgnoreHostKey(), Timeout: e.defaultTimeout}
	address := fmt.Sprintf("%s:%d", target.Host, target.Port)
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, panelerr.BadGateway("ssh_connection_failed", "SSH connection failed")
	}
	c, chans, reqs, err := ssh.NewClientConn(conn, address, cfg)
	if err != nil {
		_ = conn.Close()
		return nil, panelerr.BadGateway("ssh_auth_failed", "SSH authentication failed")
	}
	return ssh.NewClient(c, chans, reqs), nil
}

func authMethod(c credential.ResolvedCredential) (ssh.AuthMethod, error) {
	switch c.Type {
	case credential.TypePassword:
		return ssh.Password(c.Password), nil
	case credential.TypePrivateKey:
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
