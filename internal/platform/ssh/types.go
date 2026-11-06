package sshx

import (
	"context"
	"time"
)

const (
	CredentialTypePassword   = "password"
	CredentialTypePrivateKey = "private_key"
	PrivilegeModeRoot        = "root"
	PrivilegeModeSudo        = "passwordless_sudo"
	PrivilegeModeNone        = "none"
)

type ResolvedCredential struct {
	ID         string
	Type       string
	Username   string
	Password   string
	PrivateKey []byte
	Passphrase string
}

type Target struct {
	ServerID      string
	Host          string
	Port          int
	Username      string
	CredentialID  string
	PrivilegeMode string
}

type CommandSpec struct {
	Command  string
	Env      map[string]string
	Timeout  time.Duration
	OnStdout func(line string)
	OnStderr func(line string)
}

type CommandResult struct {
	Stdout     string
	Stderr     string
	ExitCode   int
	StartedAt  time.Time
	FinishedAt time.Time
	TimedOut   bool
}

type UploadSpec struct {
	LocalPath  string
	RemotePath string
	// Timeout overrides the shared remote command timeout for this transfer.
	// Large payloads such as the agent bundle need a longer bound than the
	// default remote command timeout; zero keeps the shared timeout.
	Timeout time.Duration
}

type DownloadSpec struct {
	RemotePath string
	LocalPath  string
}

type RemoteExecutor interface {
	Exec(ctx context.Context, target Target, command CommandSpec) (CommandResult, error)
	ExecSudo(ctx context.Context, target Target, command CommandSpec) (CommandResult, error)
	Upload(ctx context.Context, target Target, transfer UploadSpec) error
	Download(ctx context.Context, target Target, transfer DownloadSpec) error
}

type CredentialResolver interface {
	Resolve(ctx context.Context, credentialID string) (ResolvedCredential, error)
}
