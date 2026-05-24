package nomad

type Job struct {
	ID          string            `json:"ID,omitempty"`
	Name        string            `json:"Name,omitempty"`
	Type        string            `json:"Type,omitempty"`
	Region      string            `json:"Region,omitempty"`
	Namespace   string            `json:"Namespace,omitempty"`
	Datacenters []string          `json:"Datacenters,omitempty"`
	Meta        map[string]string `json:"Meta,omitempty"`
	TaskGroups  []TaskGroup       `json:"TaskGroups,omitempty"`
}

type TaskGroup struct {
	Name          string         `json:"Name,omitempty"`
	Count         int            `json:"Count,omitempty"`
	Networks      []Network      `json:"Networks,omitempty"`
	Tasks         []Task         `json:"Tasks,omitempty"`
	Services      []Service      `json:"Services,omitempty"`
	Constraints   []Constraint   `json:"Constraints,omitempty"`
	Volumes       map[string]any `json:"Volumes,omitempty"`
	RestartPolicy *RestartPolicy `json:"RestartPolicy,omitempty"`
}

type Task struct {
	Name      string            `json:"Name,omitempty"`
	Driver    string            `json:"Driver,omitempty"`
	Config    map[string]any    `json:"Config,omitempty"`
	Env       map[string]string `json:"Env,omitempty"`
	Resources *Resources        `json:"Resources,omitempty"`
	Services  []Service         `json:"Services,omitempty"`
	Templates []Template        `json:"Templates,omitempty"`
}

type Network struct {
	Mode          string        `json:"Mode,omitempty"`
	ReservedPorts []PortMapping `json:"ReservedPorts,omitempty"`
	DynamicPorts  []PortMapping `json:"DynamicPorts,omitempty"`
}

type PortMapping struct {
	Label string `json:"Label,omitempty"`
	Value int    `json:"Value,omitempty"`
	To    int    `json:"To,omitempty"`
}

type Resources struct {
	CPU      int `json:"CPU,omitempty"`
	MemoryMB int `json:"MemoryMB,omitempty"`
}

type Service struct {
	Name   string   `json:"Name,omitempty"`
	Port   string   `json:"PortLabel,omitempty"`
	Tags   []string `json:"Tags,omitempty"`
	Checks []Check  `json:"Checks,omitempty"`
}

type Check struct {
	Name     string `json:"Name,omitempty"`
	Type     string `json:"Type,omitempty"`
	Path     string `json:"Path,omitempty"`
	Port     string `json:"PortLabel,omitempty"`
	Interval int64  `json:"Interval,omitempty"`
	Timeout  int64  `json:"Timeout,omitempty"`
	Command  string `json:"Command,omitempty"`
}

type Constraint struct {
	LTarget string `json:"LTarget,omitempty"`
	RTarget string `json:"RTarget,omitempty"`
	Operand string `json:"Operand,omitempty"`
}

type Template struct {
	EmbeddedTmpl string `json:"EmbeddedTmpl,omitempty"`
	DestPath     string `json:"DestPath,omitempty"`
}

type RestartPolicy struct {
	Attempts int    `json:"Attempts,omitempty"`
	Interval int64  `json:"Interval,omitempty"`
	Delay    int64  `json:"Delay,omitempty"`
	Mode     string `json:"Mode,omitempty"`
}

type JobListItem struct {
	ID          string   `json:"ID,omitempty"`
	Name        string   `json:"Name,omitempty"`
	Type        string   `json:"Type,omitempty"`
	Status      string   `json:"Status,omitempty"`
	Namespace   string   `json:"Namespace,omitempty"`
	Datacenters []string `json:"Datacenters,omitempty"`
}

type ValidateResponse struct {
	DriverConfigValidated bool     `json:"DriverConfigValidated,omitempty"`
	ValidationErrors      []string `json:"ValidationErrors,omitempty"`
	Error                 string   `json:"Error,omitempty"`
}

type PlanResponse struct {
	JobModifyIndex uint64       `json:"JobModifyIndex,omitempty"`
	CreatedEvals   []Evaluation `json:"CreatedEvals,omitempty"`
	Diff           any          `json:"Diff,omitempty"`
}

type RegisterResponse struct {
	EvalID          string `json:"EvalID,omitempty"`
	EvalCreateIndex uint64 `json:"EvalCreateIndex,omitempty"`
	JobModifyIndex  uint64 `json:"JobModifyIndex,omitempty"`
}

type StopResponse struct {
	EvalID          string `json:"EvalID,omitempty"`
	EvalCreateIndex uint64 `json:"EvalCreateIndex,omitempty"`
	JobModifyIndex  uint64 `json:"JobModifyIndex,omitempty"`
}

type AllocationListItem struct {
	ID                 string         `json:"ID,omitempty"`
	EvalID             string         `json:"EvalID,omitempty"`
	Name               string         `json:"Name,omitempty"`
	NodeID             string         `json:"NodeID,omitempty"`
	JobID              string         `json:"JobID,omitempty"`
	TaskGroup          string         `json:"TaskGroup,omitempty"`
	ClientStatus       string         `json:"ClientStatus,omitempty"`
	DesiredStatus      string         `json:"DesiredStatus,omitempty"`
	TaskStates         map[string]any `json:"TaskStates,omitempty"`
	AllocatedResources any            `json:"AllocatedResources,omitempty"`
	ModifyIndex        uint64         `json:"ModifyIndex,omitempty"`
	CreateIndex        uint64         `json:"CreateIndex,omitempty"`
}

type Deployment struct {
	ID                string `json:"ID,omitempty"`
	JobID             string `json:"JobID,omitempty"`
	Namespace         string `json:"Namespace,omitempty"`
	Status            string `json:"Status,omitempty"`
	StatusDescription string `json:"StatusDescription,omitempty"`
}

type Evaluation struct {
	ID          string `json:"ID,omitempty"`
	Namespace   string `json:"Namespace,omitempty"`
	JobID       string `json:"JobID,omitempty"`
	Status      string `json:"Status,omitempty"`
	Type        string `json:"Type,omitempty"`
	TriggeredBy string `json:"TriggeredBy,omitempty"`
}

type NodeListItem struct {
	ID          string `json:"ID,omitempty"`
	Name        string `json:"Name,omitempty"`
	Datacenter  string `json:"Datacenter,omitempty"`
	Status      string `json:"Status,omitempty"`
	Eligibility string `json:"SchedulingEligibility,omitempty"`
}

type ServiceRegistration struct {
	ID          string   `json:"ID,omitempty"`
	ServiceName string   `json:"ServiceName,omitempty"`
	Namespace   string   `json:"Namespace,omitempty"`
	NodeID      string   `json:"NodeID,omitempty"`
	Datacenter  string   `json:"Datacenter,omitempty"`
	JobID       string   `json:"JobID,omitempty"`
	AllocID     string   `json:"AllocID,omitempty"`
	Tags        []string `json:"Tags,omitempty"`
	Port        int      `json:"Port,omitempty"`
}
