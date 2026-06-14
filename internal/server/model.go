package server

import (
	"time"

	"panel/internal/linux"
)

type Server struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Host          string            `json:"host"`
	Port          int               `json:"port"`
	SSHUsername   string            `json:"sshUsername"`
	CredentialID  string            `json:"credentialId"`
	Traits        map[string]string `json:"traits"`
	Variables     map[string]string `json:"variables"`
	Notes         string            `json:"notes"`
	OS            linux.OSRelease   `json:"os"`
	Sudo          SudoState         `json:"sudo"`
	Reachable     bool              `json:"reachable"`
	LoadAverage   string            `json:"loadAverage"`
	LastCheckedAt *time.Time        `json:"lastCheckedAt"`
	LastError     string            `json:"lastError,omitempty"`
	InitialTaskID string            `json:"initialTaskId,omitempty"`
	CreatedAt     time.Time         `json:"createdAt"`
	UpdatedAt     time.Time         `json:"updatedAt"`
}

type SudoState struct {
	Passwordless  bool       `json:"passwordless"`
	LastCheckedAt *time.Time `json:"lastCheckedAt"`
}

type SaveRequest struct {
	Name         string            `json:"name"`
	Host         string            `json:"host"`
	Port         int               `json:"port"`
	SSHUsername  string            `json:"sshUsername"`
	CredentialID string            `json:"credentialId"`
	Traits       map[string]string `json:"traits"`
	Variables    map[string]string `json:"variables"`
	Notes        string            `json:"notes"`
}

type ProbeResult struct {
	Reachable            bool              `json:"reachable"`
	PasswordlessSudo     bool              `json:"passwordlessSudo"`
	Root                 bool              `json:"root"`
	Privileged           bool              `json:"privileged"`
	OS                   linux.OSRelease   `json:"os"`
	Traits               map[string]string `json:"traits"`
	Variables            map[string]string `json:"variables"`
	Error                string            `json:"error,omitempty"`
	PasswordlessSudoText string            `json:"passwordlessSudoText,omitempty"`
}

type UFWState struct {
	ServerID  string    `json:"serverId"`
	Supported bool      `json:"supported"`
	Installed bool      `json:"installed"`
	Active    bool      `json:"active"`
	Status    string    `json:"status"`
	Default   string    `json:"defaultPolicy"`
	Rules     []UFWRule `json:"rules"`
}

type UFWRule struct {
	Number int    `json:"number"`
	To     string `json:"to"`
	Action string `json:"action"`
	From   string `json:"from"`
}

type UFWAllowRequest struct {
	Port     int    `json:"port"`
	Protocol string `json:"protocol"`
	From     string `json:"from"`
}

type AgentCertificateBundle struct {
	CA            string `json:"ca"`
	Certificate   string `json:"certificate"`
	PrivateKey    string `json:"privateKey"`
	ListenAddress string `json:"listenAddress"`
	AgentURL      string `json:"agentUrl"`
}
