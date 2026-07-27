package domain

import (
	"time"

	"panel/internal/platform/linux"
)

type Server struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Host          string            `json:"host"`
	Port          int               `json:"port"`
	SSHUsername   string            `json:"sshUsername"`
	CredentialID  string            `json:"credentialId"`
	DockerHost    string            `json:"dockerHost"`
	Traits        map[string]string `json:"traits"`
	Variables     map[string]string `json:"variables"`
	Notes         string            `json:"notes"`
	OS            linux.OSRelease   `json:"os"`
	Architecture  ArchitectureInfo  `json:"architecture"`
	Sudo          SudoState         `json:"sudo"`
	Privilege     PrivilegeState    `json:"privilege"`
	Reachable     bool              `json:"reachable"`
	LoadAverage   string            `json:"loadAverage"`
	LastCheckedAt *time.Time        `json:"lastCheckedAt"`
	LastError     string            `json:"lastError,omitempty"`
	InitialTaskID string            `json:"initialTaskId,omitempty"`
	CreatedAt     time.Time         `json:"createdAt"`
	UpdatedAt     time.Time         `json:"updatedAt"`
}

type ServerSummary struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Host          string            `json:"host"`
	Port          int               `json:"port"`
	Traits        map[string]string `json:"traits"`
	Sudo          SudoState         `json:"sudo"`
	Privilege     PrivilegeState    `json:"privilege"`
	Reachable     bool              `json:"reachable"`
	LastCheckedAt *time.Time        `json:"lastCheckedAt"`
	LastError     string            `json:"lastError,omitempty"`
	UpdatedAt     time.Time         `json:"updatedAt"`
}

type ArchitectureInfo struct {
	OS         string `json:"os"`
	Arch       string `json:"arch"`
	RawMachine string `json:"rawMachine"`
}

type SudoState struct {
	Passwordless  bool       `json:"passwordless"`
	LastCheckedAt *time.Time `json:"lastCheckedAt"`
}

type PrivilegeState struct {
	Mode          string     `json:"mode"`
	Privileged    bool       `json:"privileged"`
	LastCheckedAt *time.Time `json:"lastCheckedAt"`
}

type SaveRequest struct {
	Name         string            `json:"name"`
	Host         string            `json:"host"`
	Port         int               `json:"port"`
	SSHUsername  string            `json:"sshUsername"`
	CredentialID string            `json:"credentialId"`
	DockerHost   string            `json:"dockerHost"`
	Traits       map[string]string `json:"traits"`
	Variables    map[string]string `json:"variables"`
	Notes        string            `json:"notes"`
}

type ProbeResult struct {
	Reachable            bool              `json:"reachable"`
	PasswordlessSudo     bool              `json:"passwordlessSudo"`
	Root                 bool              `json:"root"`
	Privileged           bool              `json:"privileged"`
	PrivilegeMode        string            `json:"privilegeMode"`
	OS                   linux.OSRelease   `json:"os"`
	Architecture         ArchitectureInfo  `json:"architecture"`
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

type Fail2BanState struct {
	ServerID           string         `json:"serverId"`
	Installed          bool           `json:"installed"`
	Active             bool           `json:"active"`
	Managed            bool           `json:"managed"`
	PanelConfigPresent bool           `json:"panelConfigPresent"`
	Jails              []string       `json:"jails"`
	Raw                string         `json:"raw"`
	ConfigYAML         string         `json:"configYaml"`
	Config             Fail2BanConfig `json:"config"`
	UpdatedAt          *time.Time     `json:"updatedAt,omitempty"`
}

type Fail2BanUpdateRequest struct {
	ConfigYAML string `json:"configYaml"`
}

type Fail2BanEnableRequest struct {
	ConfigYAML      string `json:"configYaml"`
	ConfirmTakeover bool   `json:"confirmTakeover"`
}

type Fail2BanConfig struct {
	Jails []Fail2BanJail `json:"jails" yaml:"jails"`
}

type Fail2BanJail struct {
	Name     string            `json:"name" yaml:"name"`
	Enabled  bool              `json:"enabled" yaml:"enabled"`
	Preset   string            `json:"preset,omitempty" yaml:"preset,omitempty"`
	Filter   string            `json:"filter,omitempty" yaml:"filter,omitempty"`
	LogPath  string            `json:"logpath,omitempty" yaml:"logpath,omitempty"`
	Backend  string            `json:"backend,omitempty" yaml:"backend,omitempty"`
	Port     string            `json:"port,omitempty" yaml:"port,omitempty"`
	Protocol string            `json:"protocol,omitempty" yaml:"protocol,omitempty"`
	Action   string            `json:"action,omitempty" yaml:"action,omitempty"`
	MaxRetry int               `json:"maxretry,omitempty" yaml:"maxretry,omitempty"`
	FindTime string            `json:"findtime,omitempty" yaml:"findtime,omitempty"`
	BanTime  string            `json:"bantime,omitempty" yaml:"bantime,omitempty"`
	IgnoreIP []string          `json:"ignoreip,omitempty" yaml:"ignoreip,omitempty"`
	Options  map[string]string `json:"options,omitempty" yaml:"options,omitempty"`
}

type AgentCertificateBundle struct {
	CA            string `json:"ca"`
	Certificate   string `json:"certificate"`
	PrivateKey    string `json:"privateKey"`
	ListenAddress string `json:"listenAddress"`
	AgentURL      string `json:"agentUrl"`
	DockerHost    string `json:"dockerHost"`
}

type SystemCertificate struct {
	ID          string     `json:"id"`
	Type        string     `json:"type"`
	Name        string     `json:"name"`
	CommonName  string     `json:"commonName,omitempty"`
	Fingerprint string     `json:"fingerprint,omitempty"`
	NotBefore   *time.Time `json:"notBefore,omitempty"`
	NotAfter    *time.Time `json:"notAfter,omitempty"`
	ServerID    string     `json:"serverId,omitempty"`
	ServerName  string     `json:"serverName,omitempty"`
	Status      string     `json:"status,omitempty"`
	BuiltIn     bool       `json:"builtIn"`
	CanReset    bool       `json:"canReset"`
}
