package placement

type Capabilities struct {
	Docker  bool
	Compose bool
	Include bool
}

type Node struct {
	ID             string
	Name           string
	Reachable      bool
	OSSupported    bool
	Maintenance    bool
	Capabilities   Capabilities
	Traits         map[string]string
	ExistingClaims []int
}

type ServiceSpec struct {
	ID           string
	Selector     map[string]string
	PortClaims   []int
	Dependencies []DependencyPlacement
}

type DependencyPlacement struct {
	ServiceID string
	NodeID    string
}

type PortClaim struct {
	NodeID    string
	ServiceID string
	Port      int
}

type State struct {
	ExistingNodeID    string
	ManagedPortClaims []PortClaim
}

type Decision struct {
	Node    Node     `json:"node"`
	Reasons []string `json:"reasons"`
}
