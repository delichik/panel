package backups

import "time"

const MaintenanceStatusSchemaVersion = 1

type MaintenancePhase string

const (
	ModeNormal          = "normal"
	ModeBackupExporting = "backup_exporting"
	ModeRestorePending  = "restore_pending"
	ModeRestoreRunning  = "restore_running"

	PhaseIdle          MaintenancePhase = "idle"
	PhaseFreezing                       = "freezing"
	PhaseCheckpointing                  = "checkpointing"
	PhaseArchiving                      = "archiving"
	PhaseEncrypting                     = "encrypting"
	PhaseCompleted                      = "completed"
	PhaseFailed                         = "failed"
	PhasePassword                       = "password_required"
	PhaseExtracting                     = "extracting"
	PhaseApplying                       = "applying"
	PhaseReady                          = "ready"
)

type MaintenanceCapabilities struct {
	CanStart          bool `json:"canStart"`
	CanSubmitPassword bool `json:"canSubmitPassword"`
	CanRetry          bool `json:"canRetry"`
	CanClearPending   bool `json:"canClearPending"`
	CanDownload       bool `json:"canDownload"`
	CanExit           bool `json:"canExit"`
}

type MaintenanceError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}

type Manifest struct {
	FormatVersion int               `json:"formatVersion"`
	PanelVersion  string            `json:"panelVersion"`
	CreatedAt     time.Time         `json:"createdAt"`
	Encrypted     bool              `json:"encrypted"`
	Includes      []string          `json:"includes"`
	Files         []ManifestFile    `json:"files"`
	Metadata      map[string]string `json:"metadata,omitempty"`
}

type ManifestFile struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type Status struct {
	SchemaVersion       int                     `json:"schemaVersion"`
	Revision            uint64                  `json:"revision"`
	Mode                string                  `json:"mode"`
	Phase               MaintenancePhase        `json:"phase"`
	Progress            int                     `json:"progress"`
	StartedAt           time.Time               `json:"startedAt,omitempty"`
	FinishedAt          time.Time               `json:"finishedAt,omitempty"`
	Error               string                  `json:"error,omitempty"`
	ErrorDetail         *MaintenanceError       `json:"errorDetail,omitempty"`
	Capabilities        MaintenanceCapabilities `json:"capabilities"`
	Retryable           bool                    `json:"retryable"`
	PollAfterMS         int                     `json:"pollAfterMs"`
	ExportID            string                  `json:"exportId,omitempty"`
	DownloadAvailable   bool                    `json:"downloadAvailable"`
	RestartSupported    bool                    `json:"restartSupported"`
	Manifest            *Manifest               `json:"manifest,omitempty"`
	ClearPendingBlocked bool                    `json:"-"`
}

type MaintenanceCommandRequest struct {
	ExpectedRevision  *uint64 `json:"expectedRevision,omitempty"`
	ClientOperationID string  `json:"clientOperationId,omitempty"`
}

type ExportRequest struct {
	Encrypt  bool   `json:"encrypt"`
	Password string `json:"password"`
}

type ExportResponse struct {
	ExportID         string `json:"exportId"`
	RestartSupported bool   `json:"restartSupported"`
}

type RestorePreflightResponse struct {
	Manifest         Manifest `json:"manifest"`
	Encrypted        bool     `json:"encrypted"`
	PasswordRequired bool     `json:"passwordRequired"`
}

type RestoreConfirmRequest struct {
	ConfirmOverwrite bool `json:"confirmOverwrite"`
}

type RestoreConfirmResponse struct {
	Pending          bool `json:"pending"`
	RestartSupported bool `json:"restartSupported"`
}

type RestorePasswordRequest struct {
	MaintenanceCommandRequest
	Password string `json:"password"`
}

type maintenanceCredential struct {
	Username     string `json:"username"`
	PasswordHash string `json:"passwordHash"`
}

type pendingRestore struct {
	ArchiveFilename string                 `json:"archiveFilename"`
	ArchiveSHA256   string                 `json:"archiveSha256,omitempty"`
	ArchiveSize     int64                  `json:"archiveSize,omitempty"`
	CreatedAt       time.Time              `json:"createdAt"`
	Manifest        Manifest               `json:"manifest"`
	MaintenanceAuth *maintenanceCredential `json:"maintenanceAuth,omitempty"`
}

type pendingExport struct {
	ExportID  string    `json:"exportId"`
	CreatedAt time.Time `json:"createdAt"`
	Encrypt   bool      `json:"encrypt"`
}
