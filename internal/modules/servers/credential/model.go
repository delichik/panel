package credential

import (
	"time"

	sshx "panel/internal/platform/ssh"
)

const (
	TypePassword   = sshx.CredentialTypePassword
	TypePrivateKey = sshx.CredentialTypePrivateKey
)

type Credential struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Type      string    `json:"type"`
	Username  string    `json:"username"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type ResolvedCredential = sshx.ResolvedCredential

type KeySummary = sshx.KeySummary

// CredentialDetail is the single-credential view: it carries the same fields
// as Credential plus a non-secret key summary computed on demand.
type CredentialDetail struct {
	Credential
	KeySummary *KeySummary `json:"keySummary,omitempty"`
}

type CreateRequest struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	Username   string `json:"username"`
	Password   string `json:"password"`
	PrivateKey string `json:"privateKey"`
	Passphrase string `json:"passphrase"`
}

type UpdateRequest = CreateRequest
