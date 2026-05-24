package containerservice

import (
	"time"

	"panel/internal/tasks"
)

const (
	ResourceTypeContainerService = "container_service"
	TaskTypeReconcile            = "container_service_reconcile"
	TaskTypeRestart              = "container_service_restart"
	TaskTypeDisable              = "container_service_disable"
	TaskTypeEnable               = "container_service_enable"
	TaskTypeDelete               = "container_service_delete"
	TriggerUser                  = "user"
)

type ContainerService struct {
	ID                 string            `json:"id"`
	Name               string            `json:"name"`
	Enabled            bool              `json:"enabled"`
	ComposeServiceYAML string            `json:"composeServiceYaml"`
	Variables          map[string]string `json:"variables"`
	Selector           map[string]string `json:"selector"`
	Generation         int               `json:"generation"`
	SpecRevision       string            `json:"specRevision"`
	SpecHash           string            `json:"specHash"`
	LastError          string            `json:"lastError,omitempty"`
	LastTaskID         string            `json:"lastTaskId,omitempty"`
	CreatedAt          time.Time         `json:"createdAt"`
	UpdatedAt          time.Time         `json:"updatedAt"`
}

type SaveRequest struct {
	Name               string            `json:"name"`
	Enabled            bool              `json:"enabled"`
	ComposeServiceYAML string            `json:"composeServiceYaml"`
	Variables          map[string]string `json:"variables"`
	Selector           map[string]string `json:"selector"`
}

type ParsedServiceBody struct {
	Fields       map[string]any
	Dependencies []string
	PortClaims   []int
}

type Preview struct {
	Operation         string             `json:"operation,omitempty"`
	TargetServiceID   string             `json:"targetServiceId,omitempty"`
	TargetServiceName string             `json:"targetServiceName,omitempty"`
	AffectedServices  []ContainerService `json:"affectedServices"`
	Services          []ContainerService `json:"services,omitempty"`
	ExpectedTasks     []tasks.Task       `json:"expectedTasks,omitempty"`
	Tasks             []tasks.Task       `json:"tasks,omitempty"`
	OperationID       string             `json:"operationId,omitempty"`
}

type RuntimeStatus struct {
	ServiceID            string            `json:"serviceId"`
	ServiceName          string            `json:"serviceName"`
	Status               string            `json:"status"`
	NodeID               string            `json:"nodeId,omitempty"`
	NodeName             string            `json:"nodeName,omitempty"`
	ObservedGeneration   *int              `json:"observedGeneration,omitempty"`
	ObservedSpecRevision string            `json:"observedSpecRevision,omitempty"`
	Labels               map[string]string `json:"labels,omitempty"`
	Ports                []string          `json:"ports,omitempty"`
	ContainerID          string            `json:"containerId,omitempty"`
	Managed              bool              `json:"managed"`
	Stale                bool              `json:"stale"`
	Error                string            `json:"error,omitempty"`
	ObservedAt           *time.Time        `json:"observedAt,omitempty"`
}

type Placement struct {
	ServiceID    string    `json:"serviceId"`
	NodeID       string    `json:"nodeId"`
	NodeName     string    `json:"nodeName,omitempty"`
	Generation   int       `json:"generation"`
	SpecRevision string    `json:"specRevision"`
	ContainerID  string    `json:"containerId,omitempty"`
	Status       string    `json:"status,omitempty"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type File struct {
	ID          string    `json:"id"`
	ServiceID   string    `json:"serviceId"`
	Path        string    `json:"path"`
	Kind        string    `json:"kind"`
	ContentType string    `json:"contentType"`
	Size        int       `json:"size"`
	SHA256      string    `json:"sha256"`
	Content     string    `json:"content,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type FileInput struct {
	Path          string `json:"path"`
	Kind          string `json:"kind"`
	Content       string `json:"content"`
	Base64Content string `json:"base64Content"`
	ContentType   string `json:"contentType"`
}

type ValidationResult struct {
	Valid           bool              `json:"valid"`
	Issues          []ValidationIssue `json:"issues,omitempty"`
	DependencyNames []string          `json:"dependencyNames,omitempty"`
}

type ValidationIssue struct {
	Path     string `json:"path,omitempty"`
	Message  string `json:"message"`
	Severity string `json:"severity,omitempty"`
}

type RenderPreview struct {
	ComposeYAML  string `json:"composeYaml"`
	OverrideYAML string `json:"overrideYaml"`
	ManifestJSON string `json:"manifestJson"`
}

type SchedulePreview struct {
	SelectedNodeID string              `json:"selectedNodeId,omitempty"`
	Candidates     []ScheduleCandidate `json:"candidates"`
	Warnings       []ValidationIssue   `json:"warnings,omitempty"`
	Errors         []ValidationIssue   `json:"errors,omitempty"`
}

type ScheduleCandidate struct {
	NodeID   string   `json:"nodeId"`
	NodeName string   `json:"nodeName,omitempty"`
	Eligible bool     `json:"eligible"`
	Reasons  []string `json:"reasons,omitempty"`
}
