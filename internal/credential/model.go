package credential

import "time"

const (
	TypePassword   = "password"
	TypePrivateKey = "private_key"
)

type Credential struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Type      string    `json:"type"`
	Username  string    `json:"username"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type ResolvedCredential struct {
	ID         string
	Type       string
	Username   string
	Password   string
	PrivateKey []byte
	Passphrase string
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
