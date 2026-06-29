package backups

import "time"

const (
	ModeNormal          = "normal"
	ModeBackupExporting = "backup_exporting"
	ModeRestorePending  = "restore_pending"
	ModeRestoreRunning  = "restore_running"

	PhaseIdle          = "idle"
	PhaseFreezing      = "freezing"
	PhaseCheckpointing = "checkpointing"
	PhaseArchiving     = "archiving"
	PhaseEncrypting    = "encrypting"
	PhaseCompleted     = "completed"
	PhaseFailed        = "failed"
	PhasePassword      = "password_required"
	PhaseExtracting    = "extracting"
	PhaseApplying      = "applying"
	PhaseReady         = "ready"
)

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
	Mode              string    `json:"mode"`
	Phase             string    `json:"phase"`
	Progress          int       `json:"progress"`
	StartedAt         time.Time `json:"startedAt,omitempty"`
	FinishedAt        time.Time `json:"finishedAt,omitempty"`
	Error             string    `json:"error,omitempty"`
	ExportID          string    `json:"exportId,omitempty"`
	DownloadAvailable bool      `json:"downloadAvailable"`
	RestartSupported  bool      `json:"restartSupported"`
	Manifest          *Manifest `json:"manifest,omitempty"`
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
	Password string `json:"password"`
}

type pendingRestore struct {
	ArchiveFilename string    `json:"archiveFilename"`
	CreatedAt       time.Time `json:"createdAt"`
	Manifest        Manifest  `json:"manifest"`
}

type pendingExport struct {
	ExportID  string    `json:"exportId"`
	CreatedAt time.Time `json:"createdAt"`
	Encrypt   bool      `json:"encrypt"`
}
