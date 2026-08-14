package applications

import (
	"encoding/base64"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	appruntime "panel/internal/modules/applications/runtime"
	"panel/internal/platform/database/models"
)

// orm_mappers.go 提供模块领域类型与 orm 模型/行结构体之间的转换。
// 行结构体用于 models 无法逐字节表达的列（空字符串时间/JSON 透传），
// 语义与原手写 scan 函数保持一致。

// toDomainApplication 复刻原 scanApplication 的归一化语义。
func toDomainApplication(m models.Application) Application {
	app := Application{
		ID:                m.ID,
		Version:           m.Version,
		Kind:              applicationKind(m.Kind),
		Name:              m.Name,
		Enabled:           m.Enabled,
		DeletionRequested: m.DeletionRequested,
		ReconcileStopped:  m.ReconcileStopped,
		SpecYAML:          m.SpecYAML,
		DeploymentMode:    m.DeploymentMode,
		DeploymentServers: m.DeploymentServerIDsJSON,

		Generation:           m.Generation,
		SpecHash:             m.SpecHash,
		ImageReference:       m.ImageReference,
		ImageDigest:          m.ImageDigest,
		ImageLatestDigest:    m.ImageLatestDigest,
		ImageCheckedAt:       m.ImageCheckedAt,
		ImageUpdateAvailable: m.ImageUpdateAvailable,
		ImageLastError:       m.ImageLastError,
		JobID:                m.JobID,
		Namespace:            m.Namespace,
		LastEvalID:           m.LastEvalID,
		LastDeploymentID:     m.LastDeploymentID,
		LastError:            m.LastError,
		CreatedAt:            m.CreatedAt,
		UpdatedAt:            m.UpdatedAt,
	}
	if app.DeploymentMode == "" {
		app.DeploymentMode = DeploymentModeAll
	}
	if app.DeploymentServers == nil {
		app.DeploymentServers = []string{}
	}
	if app.ReverseProxy == nil {
		app.ReverseProxy = []ReverseProxyRule{}
	}
	app.PersistentPath = persistentPathForSpecYAML(app.ID, app.SpecYAML)
	return app
}

func fromDomainApplication(app Application) *models.Application {
	return &models.Application{
		ID:                      app.ID,
		Version:                 app.Version,
		Kind:                    applicationKind(app.Kind),
		Name:                    app.Name,
		Enabled:                 app.Enabled,
		DeletionRequested:       app.DeletionRequested,
		ReconcileStopped:        app.ReconcileStopped,
		SpecYAML:                app.SpecYAML,
		DeploymentMode:          app.DeploymentMode,
		DeploymentServerIDsJSON: app.DeploymentServers,

		Generation:           app.Generation,
		SpecHash:             app.SpecHash,
		ImageReference:       app.ImageReference,
		ImageDigest:          app.ImageDigest,
		ImageLatestDigest:    app.ImageLatestDigest,
		ImageCheckedAt:       app.ImageCheckedAt,
		ImageUpdateAvailable: app.ImageUpdateAvailable,
		ImageLastError:       app.ImageLastError,
		JobID:                app.JobID,
		Namespace:            app.Namespace,
		LastEvalID:           app.LastEvalID,
		LastDeploymentID:     app.LastDeploymentID,
		LastError:            app.LastError,
		CreatedAt:            app.CreatedAt,
		UpdatedAt:            app.UpdatedAt,
	}
}

func toDomainLifecycleOperation(m models.ApplicationLifecycleOperation) LifecycleOperation {
	return LifecycleOperation{
		ID:            m.ID,
		ApplicationID: m.ApplicationID,
		Type:          m.Type,
		Status:        m.Status,
		TaskID:        m.TaskID,
		Generation:    m.Generation,
		SpecHash:      m.SpecHash,
		Trigger:       m.Trigger,
		Error:         m.Error,
		CreatedAt:     m.CreatedAt,
		StartedAt:     m.StartedAt,
		FinishedAt:    m.FinishedAt,
		UpdatedAt:     m.UpdatedAt,
	}
}

func fromDomainLifecycleOperation(op LifecycleOperation) *models.ApplicationLifecycleOperation {
	return &models.ApplicationLifecycleOperation{
		ID:            op.ID,
		ApplicationID: op.ApplicationID,
		Type:          op.Type,
		Status:        op.Status,
		TaskID:        op.TaskID,
		Generation:    op.Generation,
		SpecHash:      op.SpecHash,
		Trigger:       op.Trigger,
		Error:         op.Error,
		CreatedAt:     op.CreatedAt,
		StartedAt:     op.StartedAt,
		FinishedAt:    op.FinishedAt,
		UpdatedAt:     op.UpdatedAt,
	}
}

// lifecycleTargetRow 使用字符串承载 next_run_at/lease_expires_at/started_at/
// finished_at：存量数据以 ” 作为默认值，models 的 time.Time 无法解析空串，
// 原 scanLifecycleTarget 亦以可空字符串解析（”/NULL 均视为 nil）。
type lifecycleTargetRow struct {
	ID                 string    `orm:"column:id"`
	OperationID        string    `orm:"column:operation_id"`
	ApplicationID      string    `orm:"column:application_id"`
	ServerID           string    `orm:"column:server_id"`
	Action             string    `orm:"column:action"`
	State              string    `orm:"column:state"`
	Status             string    `orm:"column:status"`
	TargetKey          string    `orm:"column:target_key"`
	DesiredState       string    `orm:"column:desired_state"`
	DesiredGeneration  int       `orm:"column:desired_generation"`
	DesiredSpecHash    string    `orm:"column:desired_spec_hash"`
	Priority           int       `orm:"column:priority"`
	Attempt            int       `orm:"column:attempt"`
	NextRunAt          string    `orm:"column:next_run_at"`
	LeaseOwner         string    `orm:"column:lease_owner"`
	LeaseExpiresAt     string    `orm:"column:lease_expires_at"`
	ClaimedTaskID      string    `orm:"column:claimed_task_id"`
	InstanceID         string    `orm:"column:instance_id"`
	ContainerName      string    `orm:"column:container_name"`
	ContainerID        string    `orm:"column:container_id"`
	Stage              string    `orm:"column:stage"`
	Error              string    `orm:"column:error"`
	ErrorCode          string    `orm:"column:error_code"`
	ErrorMessage       string    `orm:"column:error_message"`
	ErrorDetail        string    `orm:"column:error_detail"`
	ObservedState      string    `orm:"column:observed_state"`
	ObservedExitCode   string    `orm:"column:observed_exit_code"`
	ObservedError      string    `orm:"column:observed_error"`
	ObservedGeneration int       `orm:"column:observed_generation"`
	ObservedSpecHash   string    `orm:"column:observed_spec_hash"`
	ObservedImage      string    `orm:"column:observed_image"`
	ObservedAt         *string   `orm:"column:observed_at"`
	CreatedAt          time.Time `orm:"column:created_at"`
	StartedAt          *string   `orm:"column:started_at"`
	FinishedAt         *string   `orm:"column:finished_at"`
	UpdatedAt          time.Time `orm:"column:updated_at"`
}

func toDomainLifecycleTarget(r lifecycleTargetRow) LifecycleTarget {
	return LifecycleTarget{
		ID:                 r.ID,
		OperationID:        r.OperationID,
		ApplicationID:      r.ApplicationID,
		ServerID:           r.ServerID,
		Action:             r.Action,
		State:              r.State,
		Status:             r.Status,
		TargetKey:          r.TargetKey,
		DesiredState:       r.DesiredState,
		DesiredGeneration:  r.DesiredGeneration,
		DesiredSpecHash:    r.DesiredSpecHash,
		Priority:           r.Priority,
		Attempt:            r.Attempt,
		NextRunAt:          parseOptionalStringTime(r.NextRunAt),
		LeaseOwner:         r.LeaseOwner,
		LeaseExpiresAt:     parseOptionalStringTime(r.LeaseExpiresAt),
		ClaimedTaskID:      r.ClaimedTaskID,
		InstanceID:         r.InstanceID,
		ContainerName:      r.ContainerName,
		ContainerID:        r.ContainerID,
		Stage:              r.Stage,
		Error:              r.Error,
		ErrorCode:          r.ErrorCode,
		ErrorMessage:       r.ErrorMessage,
		ErrorDetail:        r.ErrorDetail,
		ObservedState:      r.ObservedState,
		ObservedExitCode:   r.ObservedExitCode,
		ObservedError:      r.ObservedError,
		ObservedGeneration: r.ObservedGeneration,
		ObservedSpecHash:   r.ObservedSpecHash,
		ObservedImage:      r.ObservedImage,
		ObservedAt:         parseOptionalStringTimePtr(r.ObservedAt),
		CreatedAt:          r.CreatedAt,
		StartedAt:          parseOptionalStringTimePtr(r.StartedAt),
		FinishedAt:         parseOptionalStringTimePtr(r.FinishedAt),
		UpdatedAt:          r.UpdatedAt,
	}
}

func fromDomainLifecycleTarget(t LifecycleTarget) lifecycleTargetRow {
	return lifecycleTargetRow{
		ID:                 t.ID,
		OperationID:        t.OperationID,
		ApplicationID:      t.ApplicationID,
		ServerID:           t.ServerID,
		Action:             t.Action,
		State:              t.State,
		Status:             t.Status,
		TargetKey:          t.TargetKey,
		DesiredState:       t.DesiredState,
		DesiredGeneration:  t.DesiredGeneration,
		DesiredSpecHash:    t.DesiredSpecHash,
		Priority:           t.Priority,
		Attempt:            t.Attempt,
		NextRunAt:          optionalTimeString(t.NextRunAt),
		LeaseOwner:         t.LeaseOwner,
		LeaseExpiresAt:     optionalTimeString(t.LeaseExpiresAt),
		ClaimedTaskID:      t.ClaimedTaskID,
		InstanceID:         t.InstanceID,
		ContainerName:      t.ContainerName,
		ContainerID:        t.ContainerID,
		Stage:              t.Stage,
		Error:              t.Error,
		ErrorCode:          t.ErrorCode,
		ErrorMessage:       t.ErrorMessage,
		ErrorDetail:        t.ErrorDetail,
		ObservedState:      t.ObservedState,
		ObservedExitCode:   t.ObservedExitCode,
		ObservedError:      t.ObservedError,
		ObservedGeneration: t.ObservedGeneration,
		ObservedSpecHash:   t.ObservedSpecHash,
		ObservedImage:      t.ObservedImage,
		ObservedAt:         optionalTimeStringPtr(t.ObservedAt),
		CreatedAt:          t.CreatedAt,
		StartedAt:          optionalTimeStringPtr(t.StartedAt),
		FinishedAt:         optionalTimeStringPtr(t.FinishedAt),
		UpdatedAt:          t.UpdatedAt,
	}
}

func parseOptionalStringTime(value string) *time.Time {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return nil
	}
	return &parsed
}

func parseOptionalStringTimePtr(value *string) *time.Time {
	if value == nil {
		return nil
	}
	return parseOptionalStringTime(*value)
}

func optionalTimeStringPtr(value *time.Time) *string {
	if value == nil || value.IsZero() {
		return nil
	}
	out := formatTime(*value)
	return &out
}

func optionalTimeString(value *time.Time) string {
	if value == nil || value.IsZero() {
		return ""
	}
	return formatTime(*value)
}

func toDomainApplicationFile(m models.ApplicationFile, includeContent bool) ApplicationFile {
	file := ApplicationFile{
		ID:            m.ID,
		ApplicationID: m.ApplicationID,
		Name:          m.Name,
		Kind:          m.Kind,
		ContentType:   m.ContentType,
		Size:          int64(m.Size),
		SHA256:        m.SHA256,
		Content:       m.Content,
		CreatedAt:     m.CreatedAt,
		UpdatedAt:     m.UpdatedAt,
	}
	file.Path = file.Name
	if includeContent {
		file.ContentBase64 = base64.StdEncoding.EncodeToString(file.Content)
	}
	return file
}

func fromDomainApplicationFile(file ApplicationFile) *models.ApplicationFile {
	return &models.ApplicationFile{
		ID:            file.ID,
		ApplicationID: file.ApplicationID,
		Name:          file.Name,
		Kind:          file.Kind,
		ContentType:   file.ContentType,
		Size:          int(file.Size),
		SHA256:        file.SHA256,
		Content:       file.Content,
		CreatedAt:     file.CreatedAt,
		UpdatedAt:     file.UpdatedAt,
	}
}

func toRuntimeInstance(m models.ApplicationInstance) appruntime.Instance {
	var instance appruntime.Instance
	instance.ID = m.ID
	instance.ApplicationID = m.ApplicationID
	instance.ServerID = m.ServerID
	instance.ContainerName = m.ContainerName
	instance.ContainerID = m.ContainerID
	instance.DesiredState = m.DesiredState
	instance.Status = m.Status
	instance.LastDeployedGeneration = m.LastDeployedGeneration
	instance.LastError = m.LastError
	instance.CreatedAt = m.CreatedAt
	instance.UpdatedAt = m.UpdatedAt
	if raw, err := json.Marshal(m.RuntimeSpecJSON); err == nil {
		_ = json.Unmarshal(raw, &instance.RuntimeSpec)
	}
	return instance
}

// editSessionRow 使用字符串承载 base_resource_updated_at/preview_expires_at/
// commit_lease_expires_at/committed_at 等列：存量数据以 ” 作为默认值，
// models 的 time.Time 无法解析空串。draft_json/commit_result_json/conflict_json
// 以字符串透传，保持与原 loadEditSession 的手工 json.Unmarshal 语义一致。
type editSessionRow struct {
	ID                    string `orm:"column:id"`
	ApplicationID         string `orm:"column:application_id"`
	OwnerID               string `orm:"column:owner_id"`
	ClientDraftKey        string `orm:"column:client_draft_key"`
	State                 string `orm:"column:state"`
	BaseResourceVersion   int    `orm:"column:base_resource_version"`
	BaseResourceUpdatedAt string `orm:"column:base_resource_updated_at"`
	DraftJSON             string `orm:"column:draft_json"`
	Revision              int    `orm:"column:revision"`
	PreviewToken          string `orm:"column:preview_token"`
	PreviewRevision       int    `orm:"column:preview_revision"`
	PreviewExpiresAt      string `orm:"column:preview_expires_at"`
	CommitLeaseOwner      string `orm:"column:commit_lease_owner"`
	CommitLeaseExpiresAt  string `orm:"column:commit_lease_expires_at"`
	CommitIdempotencyKey  string `orm:"column:commit_idempotency_key"`
	CommitApplicationID   string `orm:"column:commit_application_id"`
	CommitResultJSON      string `orm:"column:commit_result_json"`
	ConflictJSON          string `orm:"column:conflict_json"`
	IdleExpiresAt         string `orm:"column:idle_expires_at"`
	AbsoluteExpiresAt     string `orm:"column:absolute_expires_at"`
	CreatedAt             string `orm:"column:created_at"`
	UpdatedAt             string `orm:"column:updated_at"`
	CommittedAt           string `orm:"column:committed_at"`
}

func toEditSessionRecord(r editSessionRow) editSessionRecord {
	record := editSessionRecord{
		ApplicationEditSession: ApplicationEditSession{
			ID:                  r.ID,
			ApplicationID:       r.ApplicationID,
			ClientDraftKey:      r.ClientDraftKey,
			State:               r.State,
			BaseResourceVersion: ResourceVersion{Value: strconv.Itoa(r.BaseResourceVersion), UpdatedAt: parseEditTime(r.BaseResourceUpdatedAt)},
			Revision:            r.Revision,
			IdleExpiresAt:       parseEditTime(r.IdleExpiresAt),
			AbsoluteExpiresAt:   parseEditTime(r.AbsoluteExpiresAt),
			CreatedAt:           parseEditTime(r.CreatedAt),
			UpdatedAt:           parseEditTime(r.UpdatedAt),
		},
		OwnerID:            r.OwnerID,
		PreviewRevision:    r.PreviewRevision,
		PreviewExpiresAt:   parseEditTime(r.PreviewExpiresAt),
		CommitLeaseOwner:   r.CommitLeaseOwner,
		CommitLeaseExpires: parseEditTime(r.CommitLeaseExpiresAt),
		CommitKey:          r.CommitIdempotencyKey,
		CommitApplication:  r.CommitApplicationID,
	}
	_ = json.Unmarshal([]byte(r.DraftJSON), &record.Draft)
	record.Draft = normalizeEditDraft(record.Draft)
	if r.PreviewToken != "" {
		record.PreviewToken = &PreviewToken{Value: r.PreviewToken, Action: "application.commit", SubjectVersion: strconv.Itoa(r.BaseResourceVersion)}
	}
	if r.CommittedAt != "" {
		value := parseEditTime(r.CommittedAt)
		record.CommittedAt = &value
	}
	if r.CommitResultJSON != "" {
		var result EditCommitResult
		if json.Unmarshal([]byte(r.CommitResultJSON), &result) == nil {
			record.CommitResult = &result
		}
	}
	return record
}
