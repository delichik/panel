package appspec

type Spec struct {
	Name        string            `json:"name" yaml:"name"`
	Image       string            `json:"image" yaml:"image"`
	Count       int               `json:"count" yaml:"count"`
	NetworkMode string            `json:"networkMode" yaml:"networkMode"`
	Command     []string          `json:"command" yaml:"command"`
	Env         map[string]string `json:"env" yaml:"env"`
	Ports       []Port            `json:"ports" yaml:"ports"`
	Resources   Resources         `json:"resources" yaml:"resources"`
	Privileged  bool              `json:"privileged" yaml:"privileged"`
	CapAdd      []string          `json:"capAdd" yaml:"capAdd"`
	Constraints []Constraint      `json:"constraints" yaml:"constraints"`
	Services    []Service         `json:"services" yaml:"services"`
	Checks      []Check           `json:"checks" yaml:"checks"`
	Volumes     []Volume          `json:"volumes" yaml:"volumes"`
	Mounts      []Mount           `json:"mounts" yaml:"mounts"`
	Restart     Restart           `json:"restart" yaml:"restart"`
}

type Port struct {
	Label  string `json:"label" yaml:"label"`
	To     int    `json:"to" yaml:"to"`
	Static int    `json:"static" yaml:"static"`
}

type Resources struct {
	CPU      int `json:"cpu" yaml:"cpu"`
	MemoryMB int `json:"memoryMb" yaml:"memoryMb"`
}

type Constraint struct {
	Attribute string `json:"attribute" yaml:"attribute"`
	Operator  string `json:"operator" yaml:"operator"`
	Value     string `json:"value" yaml:"value"`
}

type Service struct {
	Name string   `json:"name" yaml:"name"`
	Port string   `json:"port" yaml:"port"`
	Tags []string `json:"tags" yaml:"tags"`
}

type Check struct {
	Name            string `json:"name" yaml:"name"`
	Type            string `json:"type" yaml:"type"`
	Port            string `json:"port" yaml:"port"`
	Path            string `json:"path" yaml:"path"`
	IntervalSeconds int    `json:"intervalSeconds" yaml:"intervalSeconds"`
	TimeoutSeconds  int    `json:"timeoutSeconds" yaml:"timeoutSeconds"`
	Command         string `json:"command" yaml:"command"`
}

type Volume struct {
	Source   string `json:"source" yaml:"source"`
	Target   string `json:"target" yaml:"target"`
	ReadOnly bool   `json:"readOnly" yaml:"readOnly"`
}

type Mount struct {
	Type     string `json:"type" yaml:"type"`
	Source   string `json:"source" yaml:"source"`
	Target   string `json:"target" yaml:"target"`
	ReadOnly bool   `json:"readOnly" yaml:"readOnly"`
	UID      *int   `json:"uid,omitempty" yaml:"uid,omitempty"`
	GID      *int   `json:"gid,omitempty" yaml:"gid,omitempty"`
	Mode     string `json:"mode,omitempty" yaml:"mode,omitempty"`
}

type Restart struct {
	Policy          string `json:"policy" yaml:"policy"`
	Attempts        int    `json:"attempts" yaml:"attempts"`
	IntervalSeconds int    `json:"intervalSeconds" yaml:"intervalSeconds"`
	DelaySeconds    int    `json:"delaySeconds" yaml:"delaySeconds"`
	Mode            string `json:"mode" yaml:"mode"`
}

type Issue struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}
