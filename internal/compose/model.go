package compose

import "time"

const (
	FileKindBinary   = "binary"
	FileKindTemplate = "template"

	ServiceStatusDraft   = "draft"
	ServiceStatusReady   = "ready"
	ServiceStatusRemoved = "removed"
)

type TemplateVariable struct {
	Key         string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required"`
	Default     any    `json:"defaultValue,omitempty"`
}

type ServiceTemplate struct {
	ID           string             `json:"id"`
	Name         string             `json:"name"`
	Description  string             `json:"description"`
	ComposeYAML  string             `json:"composeYaml"`
	VisualState  map[string]any     `json:"visual"`
	Variables    []TemplateVariable `json:"variables"`
	Dependencies []string           `json:"dependencies"`
	Version      int                `json:"version"`
	CreatedAt    time.Time          `json:"createdAt"`
	UpdatedAt    time.Time          `json:"updatedAt"`
}

type SaveTemplateRequest struct {
	Name         string             `json:"name"`
	Description  string             `json:"description"`
	ComposeYAML  string             `json:"composeYaml"`
	VisualState  map[string]any     `json:"visual"`
	Variables    []TemplateVariable `json:"variables"`
	Dependencies []string           `json:"dependencies"`
}

type TemplateFile struct {
	ID          string    `json:"id"`
	TemplateID  string    `json:"templateId"`
	Path        string    `json:"path"`
	Kind        string    `json:"kind"`
	ContentType string    `json:"contentType"`
	Size        int64     `json:"sizeBytes"`
	SHA256      string    `json:"sha256"`
	Content     string    `json:"content,omitempty"`
	Base64      string    `json:"base64Content,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type SaveFileRequest struct {
	Path        string `json:"path"`
	ContentType string `json:"contentType"`
	Body        string `json:"content"`
	Base64      string `json:"base64Content"`
}

type DeployedService struct {
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	ServerID        string            `json:"serverId"`
	ServerName      string            `json:"serverName,omitempty"`
	TemplateID      string            `json:"templateId"`
	TemplateName    string            `json:"templateName,omitempty"`
	TemplateVersion int               `json:"templateVersion"`
	RemotePath      string            `json:"remotePath"`
	Values          map[string]any    `json:"values"`
	Labels          map[string]string `json:"labels"`
	Status          string            `json:"status"`
	ManagementState string            `json:"managementState,omitempty"`
	RuntimeStatus   string            `json:"runtimeStatus,omitempty"`
	Drifted         bool              `json:"drift"`
	LastTaskID      string            `json:"lastTaskId,omitempty"`
	CreatedAt       time.Time         `json:"createdAt"`
	UpdatedAt       time.Time         `json:"updatedAt"`
}

type SaveServiceRequest struct {
	Name       string         `json:"name"`
	ServerID   string         `json:"serverId"`
	TemplateID string         `json:"templateId"`
	RemotePath string         `json:"remotePath"`
	Values     map[string]any `json:"values"`
}

type RenderRequest struct {
	ServerID    string         `json:"serverId"`
	ServiceID   string         `json:"serviceId"`
	ServiceName string         `json:"serviceName"`
	RemotePath  string         `json:"remotePath"`
	Values      map[string]any `json:"values,omitempty"`
}

type RenderedFile struct {
	Path    string `json:"path"`
	Kind    string `json:"kind"`
	Content string `json:"content,omitempty"`
	Size    int64  `json:"size"`
}

type RenderResult struct {
	ComposeYAML string         `json:"renderedYaml"`
	Files       []RenderedFile `json:"files"`
	Values      map[string]any `json:"values"`
}

type ValidateResult struct {
	Valid bool `json:"valid"`
}
