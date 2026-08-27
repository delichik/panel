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
	instance.ContainerName = firstNonEmpty(m.ObservedContainerName, m.ContainerName)
	instance.ContainerID = firstNonEmpty(m.ObservedContainerID, m.ContainerID)
	instance.DesiredState = m.DesiredState
	instance.Status = m.Status
	if strings.TrimSpace(m.ObservedSource) != "" && strings.TrimSpace(m.ObservedState) != "" {
		instance.Status = m.ObservedState
	}
	instance.LastDeployedGeneration = m.LastDeployedGeneration
	if m.ObservedGeneration > 0 {
		instance.LastDeployedGeneration = m.ObservedGeneration
	}
	instance.LastError = firstNonEmpty(m.LastErrorMessage, m.LastError)
	instance.CreatedAt = m.CreatedAt
	instance.UpdatedAt = m.UpdatedAt
	runtimeSpecJSON := m.RuntimeSpecJSON
	if len(m.DesiredSpecJSON) > 0 {
		runtimeSpecJSON = m.DesiredSpecJSON
	}
	if raw, err := json.Marshal(runtimeSpecJSON); err == nil {
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
