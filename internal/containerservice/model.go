package containerservice

import "time"

const (
	ResourceTypeContainerService = "container_service"
	TaskTypeReconcile            = "container_service_reconcile"
	TaskTypeRestart              = "container_service_restart"
	TaskTypeDisable              = "container_service_disable"
	TaskTypeEnable               = "container_service_enable"
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
	Services []ContainerService `json:"services"`
}

type RuntimeStatus struct {
	ServiceID string `json:"serviceId"`
	Status    string `json:"status"`
	NodeID    string `json:"nodeId,omitempty"`
}
