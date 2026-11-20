package backups

import (
	"net/http"
	"strings"
	"time"

	panelerr "panel/internal/platform/errors"
	httpx "panel/internal/platform/http"
)

type maintenanceOperation struct {
	Command    string
	HTTPStatus int
	Status     Status
}

func prepareStatus(status Status) Status {
	status.SchemaVersion = MaintenanceStatusSchemaVersion
	if status.Revision == 0 {
		status.Revision = 1
	}
	status.Capabilities = capabilitiesFor(status)
	status.Retryable = status.ErrorDetail != nil && status.ErrorDetail.Retryable
	switch status.Phase {
	case PhaseCheckpointing, PhaseArchiving, PhaseEncrypting, PhaseExtracting, PhaseApplying:
		status.PollAfterMS = 750
	default:
		status.PollAfterMS = 0
	}
	return status
}

func capabilitiesFor(status Status) MaintenanceCapabilities {
	capabilities := MaintenanceCapabilities{}
	if status.Mode == ModeBackupExporting {
		switch status.Phase {
		case PhaseReady:
			capabilities.CanStart = true
		case PhasePassword:
			capabilities.CanSubmitPassword = true
		case PhaseCompleted:
			capabilities.CanDownload = status.DownloadAvailable
			capabilities.CanExit = true
		case PhaseFailed:
			capabilities.CanExit = true
		}
		return capabilities
	}
	if status.Mode == ModeRestoreRunning {
		switch status.Phase {
		case PhasePassword:
			capabilities.CanSubmitPassword = true
			capabilities.CanClearPending = true
		case PhaseFailed:
			capabilities.CanRetry = !status.ClearPendingBlocked && status.ErrorDetail != nil && status.ErrorDetail.Retryable
			capabilities.CanClearPending = !status.ClearPendingBlocked
		}
	}
	return capabilities
}

func transitionStatus(status *Status, phase MaintenancePhase, progress int, errorCode, message string, retryable bool) {
	status.SchemaVersion = MaintenanceStatusSchemaVersion
	if status.Revision == 0 {
		status.Revision = 1
	} else {
		status.Revision++
	}
	status.Phase = phase
	status.Progress = progress
	status.Error = message
	status.ErrorDetail = nil
	if message != "" {
		status.ErrorDetail = &MaintenanceError{Code: errorCode, Message: message, Retryable: retryable}
	}
	if phase == PhaseCompleted || phase == PhaseFailed {
		status.FinishedAt = time.Now().UTC()
	} else {
		status.FinishedAt = time.Time{}
	}
}

func decodeOptionalCommand(w http.ResponseWriter, r *http.Request, target *MaintenanceCommandRequest) bool {
	if r.Body == nil || r.Body == http.NoBody || r.ContentLength == 0 {
		return true
	}
	return httpx.Decode(w, r, target)
}

func commandOperationID(w http.ResponseWriter, r *http.Request, bodyID string) (string, bool) {
	bodyID = strings.TrimSpace(bodyID)
	headerID := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if bodyID != "" && headerID != "" && bodyID != headerID {
		httpx.Error(w, panelerr.BadRequest("idempotency_key_mismatch", "Idempotency key does not match client operation ID"))
		return "", false
	}
	id := bodyID
	if id == "" {
		id = headerID
	}
	if len(id) > 128 {
		httpx.Error(w, panelerr.BadRequest("idempotency_key_invalid", "Idempotency key is too long"))
		return "", false
	}
	return id, true
}

func operationReplay(operations map[string]maintenanceOperation, operationID, command string) (maintenanceOperation, bool, bool) {
	if operationID == "" {
		return maintenanceOperation{}, false, false
	}
	record, ok := operations[operationID]
	if !ok {
		return maintenanceOperation{}, false, false
	}
	return record, true, record.Command != command
}

func revisionMatches(status Status, expected *uint64) bool {
	return expected == nil || *expected == status.Revision
}
