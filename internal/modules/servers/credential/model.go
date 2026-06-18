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

type CreateRequest struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	Username   string `json:"username"`
	Password   string `json:"password"`
	PrivateKey string `json:"privateKey"`
	Passphrase string `json:"passphrase"`
}

type UpdateRequest = CreateRequest
