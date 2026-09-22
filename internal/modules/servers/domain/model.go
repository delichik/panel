package domain

import (
	"time"

	"panel/internal/platform/linux"
)

// ServerKindKind configures how a server exposes its ports. KindNAT marks a
// server whose external ports are opened manually on the NAT provider side;
// it cannot host the reverse-proxy facility.
const (
	ServerKindNormal = "normal"
	ServerKindNAT    = "nat"
)

// IsValidServerKind reports whether kind is a known server kind.
func IsValidServerKind(kind string) bool {
	return kind == ServerKindNormal || kind == ServerKindNAT
}

type Server struct {
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	Kind            string            `json:"kind"`
	Host            string            `json:"host"`
	IPv4            string            `json:"ipv4,omitempty"`
	IPv6            string            `json:"ipv6,omitempty"`
	Port            int               `json:"port"`
	AgentPublicPort int               `json:"agentPublicPort,omitempty"`
	SSHUsername     string            `json:"sshUsername"`
	CredentialID    string            `json:"credentialId"`
	DockerHost      string            `json:"dockerHost"`
	Traits          map[string]string `json:"traits"`
	Variables       map[string]string `json:"variables"`
	Notes           string            `json:"notes"`
	OS              linux.OSRelease   `json:"os"`
	Architecture    ArchitectureInfo  `json:"architecture"`
	Sudo            SudoState         `json:"sudo"`
	Privilege       PrivilegeState    `json:"privilege"`
	Reachable       bool              `json:"reachable"`
	LoadAverage     string            `json:"loadAverage"`
	LastCheckedAt   *time.Time        `json:"lastCheckedAt"`
	LastError       string            `json:"lastError,omitempty"`
	HostKeyMismatch bool              `json:"hostKeyMismatch,omitempty"`
	InitialTaskID   string            `json:"initialTaskId,omitempty"`
	CreatedAt       time.Time         `json:"createdAt"`
	UpdatedAt       time.Time         `json:"updatedAt"`
}

type ServerSummary struct {
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	Kind            string            `json:"kind"`
	Host            string            `json:"host"`
	Port            int               `json:"port"`
	CredentialID    string            `json:"credentialId"`
	Traits          map[string]string `json:"traits"`
	Sudo            SudoState         `json:"sudo"`
	Privilege       PrivilegeState    `json:"privilege"`
	Reachable       bool              `json:"reachable"`
	LastCheckedAt   *time.Time        `json:"lastCheckedAt"`
	LastError       string            `json:"lastError,omitempty"`
	HostKeyMismatch bool              `json:"hostKeyMismatch,omitempty"`
	UpdatedAt       time.Time         `json:"updatedAt"`
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
	Name string `json:"name"`
	// Host is rejected on purpose: the connection address is derived from
	// ipv4/ipv6 so callers cannot supply a free-form hostname anymore.
	Host         string            `json:"host"`
	Kind         string            `json:"kind"`
	IPv4         string            `json:"ipv4"`
	IPv6         string            `json:"ipv6"`
	Port         int               `json:"port"`
	AgentPublicPort int            `json:"agentPublicPort"`
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

// NatPortMapping is a single external-port mapping on a NAT server: the NAT
// provider forwards public_port to the server's internal host_port.
type NatPortMapping struct {
	ID         string    `json:"id"`
	ServerID   string    `json:"serverId"`
	AppID      string    `json:"appId"`
	AppName    string    `json:"appName,omitempty"`
	HostPort   int       `json:"hostPort"`
	PublicPort int       `json:"publicPort"`
	Protocol   string    `json:"protocol"`
	Label      string    `json:"label"`
	Notes      string    `json:"notes"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

// NatPortMappingSave is the create/update payload for a NAT port mapping.
type NatPortMappingSave struct {
	AppID      string `json:"appId"`
	HostPort   int    `json:"hostPort"`
	PublicPort int    `json:"publicPort"`
	Protocol   string `json:"protocol"`
	Label      string `json:"label"`
	Notes      string `json:"notes"`
}

// NatPortNeedOpen is one entry of the ports a NAT server must have opened
// manually on the provider side. Kind is "ssh", "agent" or "app".
type NatPortNeedOpen struct {
	Kind   string `json:"kind"`
	Port   int    `json:"port"`
	Label  string `json:"label"`
	Target string `json:"target,omitempty"`
}

// NatPortConfig is the full NAT port configuration for one server: the managed
// mappings plus the read-only reminder of ports that must be opened upstream.
type NatPortConfig struct {
	ServerID   string            `json:"serverId"`
	ServerHost string            `json:"serverHost"`
	Kind       string            `json:"kind"`
	Mappings   []NatPortMapping  `json:"mappings"`
	NeedOpen   []NatPortNeedOpen `json:"needOpen"`
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
