package applications

import (
	"time"

	"panel/internal/nomad"
)

const (
	TaskTypeDeploy      = "application_deploy"
	TaskTypeStop        = "application_stop"
	TaskTypeRestart     = "application_restart"
	TaskTypeDelete      = "application_delete"
	TaskTypeRefresh     = "nomad_refresh"
	TaskTypeImageCheck  = "application_image_check"
	TaskTypeImageUpdate = "application_image_update"

	DeploymentModeAll      = "all"
	DeploymentModeSelected = "selected"
)

type Application struct {
	ID                   string             `json:"id"`
	Name                 string             `json:"name"`
	Enabled              bool               `json:"enabled"`
	SpecYAML             string             `json:"specYaml"`
	Variables            map[string]string  `json:"variables"`
	ResolvedVariables    map[string]any     `json:"resolvedVariables,omitempty"`
	PersistentPath       string             `json:"persistentPath,omitempty"`
	DeploymentMode       string             `json:"deploymentMode"`
	DeploymentServers    []string           `json:"deploymentServers"`
	ReverseProxy         []ReverseProxyRule `json:"reverseProxy"`
	Generation           int                `json:"generation"`
	SpecHash             string             `json:"specHash"`
	ImageReference       string             `json:"imageReference,omitempty"`
	ImageDigest          string             `json:"imageDigest,omitempty"`
	ImageLatestDigest    string             `json:"imageLatestDigest,omitempty"`
	ImageCheckedAt       *time.Time         `json:"imageCheckedAt,omitempty"`
	ImageUpdateAvailable bool               `json:"imageUpdateAvailable"`
	ImageLastError       string             `json:"imageLastError,omitempty"`
	JobID                string             `json:"jobId"`
	Namespace            string             `json:"namespace"`
	LastEvalID           string             `json:"lastEvalId,omitempty"`
	LastDeploymentID     string             `json:"lastDeploymentId,omitempty"`
	LastError            string             `json:"lastError,omitempty"`
	CreatedAt            time.Time          `json:"createdAt"`
	UpdatedAt            time.Time          `json:"updatedAt"`
}

type ReverseProxyRule struct {
	Domain     string             `json:"domain"`
	TargetPort int                `json:"targetPort"`
	Paths      []ReverseProxyPath `json:"paths"`
}

type ReverseProxyPath struct {
	Path      string `json:"path"`
	WebSocket bool   `json:"webSocket"`
}

type ApplicationFile struct {
	ID            string    `json:"id"`
	ApplicationID string    `json:"applicationId"`
	Path          string    `json:"path"`
	Kind          string    `json:"kind"`
	ContentType   string    `json:"contentType"`
	Size          int64     `json:"size"`
	SHA256        string    `json:"sha256"`
	Content       []byte    `json:"content,omitempty"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

type FileSaveInput struct {
	Path          string `json:"path"`
	Kind          string `json:"kind"`
	ContentType   string `json:"contentType"`
	ContentBase64 string `json:"contentBase64"`
}

type FileDeleteInput struct {
	Path string `json:"path"`
}

type BeginSaveSessionInput struct {
	ApplicationID string    `json:"applicationId,omitempty"`
	Save          SaveInput `json:"save"`
}

type SaveSessionResult struct {
	ID            string            `json:"id"`
	ApplicationID string            `json:"applicationId,omitempty"`
	ExpiresAt     time.Time         `json:"expiresAt"`
	Files         []ApplicationFile `json:"files"`
}

type ApplicationRevision struct {
	ID            string    `json:"id"`
	ApplicationID string    `json:"applicationId"`
	Generation    int       `json:"generation"`
	SpecHash      string    `json:"specHash"`
	SpecYAML      string    `json:"specYaml"`
	JobJSON       string    `json:"jobJson"`
	CreatedAt     time.Time `json:"createdAt"`
}

type Runtime struct {
	ApplicationID     string                      `json:"applicationId"`
	JobID             string                      `json:"jobId"`
	JobStatus         string                      `json:"jobStatus"`
	Deployment        *nomad.Deployment           `json:"deployment,omitempty"`
	Evaluations       []nomad.Evaluation          `json:"evaluations"`
	EvaluationDetails []nomad.Evaluation          `json:"evaluationDetails,omitempty"`
	Allocations       []nomad.AllocationListItem  `json:"allocations"`
	Services          []nomad.ServiceRegistration `json:"services,omitempty"`
	ObservedAt        time.Time                   `json:"observedAt"`
}

type SaveInput struct {
	Name              string             `json:"name"`
	Enabled           bool               `json:"enabled"`
	SpecYAML          string             `json:"specYaml"`
	Variables         map[string]string  `json:"variables"`
	PersistentPath    string             `json:"persistentPath"`
	DeploymentMode    string             `json:"deploymentMode"`
	DeploymentServers []string           `json:"deploymentServers"`
	ReverseProxy      []ReverseProxyRule `json:"reverseProxy"`
}

type OperationResult struct {
	TaskID             string      `json:"taskId,omitempty"`
	EvalID             string      `json:"evalId,omitempty"`
	DeploymentID       string      `json:"deploymentId,omitempty"`
	Application        Application `json:"application"`
	ApplicationRuntime *Runtime    `json:"runtime,omitempty"`
}

type ValidationIssue struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

type ValidationResult struct {
	Valid  bool              `json:"valid"`
	Issues []ValidationIssue `json:"issues"`
}
