package server

import (
	"time"

	"panel/internal/linux"
)

type Server struct {
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	Host          string          `json:"host"`
	Port          int             `json:"port"`
	SSHUsername   string          `json:"sshUsername"`
	CredentialID  string          `json:"credentialId"`
	Labels        []string        `json:"labels"`
	Notes         string          `json:"notes"`
	OS            linux.OSRelease `json:"os"`
	Sudo          SudoState       `json:"sudo"`
	Reachable     bool            `json:"reachable"`
	LoadAverage   string          `json:"loadAverage"`
	LastCheckedAt *time.Time      `json:"lastCheckedAt"`
	LastError     string          `json:"lastError,omitempty"`
	CreatedAt     time.Time       `json:"createdAt"`
	UpdatedAt     time.Time       `json:"updatedAt"`
}

type SudoState struct {
	Passwordless  bool       `json:"passwordless"`
	LastCheckedAt *time.Time `json:"lastCheckedAt"`
}

type SaveRequest struct {
	Name         string   `json:"name"`
	Host         string   `json:"host"`
	Port         int      `json:"port"`
	SSHUsername  string   `json:"sshUsername"`
	CredentialID string   `json:"credentialId"`
	Labels       []string `json:"labels"`
	Notes        string   `json:"notes"`
}
