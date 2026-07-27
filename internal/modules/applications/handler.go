package applications

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"

	panelerr "panel/internal/platform/errors"
	"panel/internal/platform/http"
)

const (
	persistentArchiveMaxBytes   = 64 << 20
	persistentArchiveFormMemory = 8 << 20
)

type applicationService interface {
	List(ctx context.Context) ([]Application, error)
	Get(ctx context.Context, id string) (Application, error)
	Create(ctx context.Context, in SaveInput) (Application, error)
	Update(ctx context.Context, id string, in SaveInput) (Application, error)
	Delete(ctx context.Context, id string) error
	ListFiles(ctx context.Context, id string) ([]ApplicationFile, error)
	GetFile(ctx context.Context, id, fileID string) (ApplicationFile, error)
	SaveFile(ctx context.Context, id string, in FileSaveInput) (ApplicationFile, error)
	DeleteFile(ctx context.Context, id, fileID string) error
	BeginSaveSession(ctx context.Context, in BeginSaveSessionInput) (SaveSessionResult, error)
	UploadSaveSessionFile(ctx context.Context, sessionID string, in FileSaveInput) (ApplicationFile, error)
	UploadSaveSessionArchive(ctx context.Context, sessionID string, in FileArchiveInput) ([]ApplicationFile, error)
	DeleteSaveSessionFile(ctx context.Context, sessionID string, in FileDeleteInput) error
	CommitSaveSession(ctx context.Context, sessionID string) (Application, error)
	Package(ctx context.Context, id string) (PackageResult, error)
	PersistentData(ctx context.Context, id string) (PackageResult, error)
	RestorePersistentData(ctx context.Context, id string, content []byte) (OperationResult, error)
	Validate(ctx context.Context, id string) (ValidationResult, error)
	Plan(ctx context.Context, id string) (PlanResult, error)
	CheckImageUpdate(ctx context.Context, id string) (Application, error)
	UpdateImage(ctx context.Context, id string) (OperationResult, error)
	Deploy(ctx context.Context, id string) (OperationResult, error)
	Migrate(ctx context.Context, id string, in MigrationInput) (OperationResult, error)
	Stop(ctx context.Context, id string, purge bool) (OperationResult, error)
	Restart(ctx context.Context, id string) (OperationResult, error)
	Runtime(ctx context.Context, id string) (ApplicationRuntime, error)
	Logs(ctx context.Context, id string, in LogInput) (LogResult, error)
	TemplateCatalog(ctx context.Context) (TemplateCatalog, error)
}

type applicationRuntimeListService interface {
	ListWithRuntime(ctx context.Context) ([]Application, error)
}

type applicationSummaryListService interface {
	ListSummaries(ctx context.Context, page, pageSize int, query string) (httpx.ListPage[ApplicationSummary], error)
}

type applicationEditSessionService interface {
	BeginEditSession(context.Context, string, BeginEditSessionInput) (ApplicationEditSession, error)
	RecoverableEditSessions(context.Context, string, string, string) ([]ApplicationEditSession, error)
	GetEditSession(context.Context, string, string) (ApplicationEditSession, error)
	PatchEditSession(context.Context, string, string, PatchEditSessionInput) (ApplicationEditSession, error)
	PutEditSessionFile(context.Context, string, string, string, string, EditSessionFileInput) (ApplicationEditSession, error)
	UploadEditSessionArchive(context.Context, string, string, string, EditSessionArchiveInput) (ApplicationEditSession, error)
	DeleteEditSessionFile(context.Context, string, string, string, string, EditSessionMutationInput) (ApplicationEditSession, error)
	ValidateEditSession(context.Context, string, string, int) (EditSessionValidationResult, error)
	PreviewEditSession(context.Context, string, string, int) (EditSessionPreviewResult, error)
	CommitEditSession(context.Context, string, string, string, CommitEditSessionInput) (EditCommitResult, error)
	DiscardEditSession(context.Context, string, string) error
}

func (h *Handler) TemplateCatalog(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.TemplateCatalog(r.Context())
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

type Handler struct {
	service applicationService
}

func NewHandler(service applicationService) *Handler {
	return &Handler{service: service}
}

func (h *Handler) editSessions() (applicationEditSessionService, error) {
	service, ok := h.service.(applicationEditSessionService)
	if !ok {
		return nil, panelerr.New(http.StatusNotImplemented, "application_edit_sessions_unavailable", "Application edit sessions are not available")
	}
	return service, nil
}

func editSessionOwner(ctx context.Context) string {
	// Panel currently has one administrator principal. The username is editable,
	// so it cannot be used as durable edit-session ownership identity.
	return applicationEditSessionOwner
}

func (h *Handler) BeginEditSession(w http.ResponseWriter, r *http.Request) {
	service, err := h.editSessions()
	if err != nil {
		httpx.Error(w, err)
		return
	}
	var in BeginEditSessionInput
	if !httpx.Decode(w, r, &in) {
		return
	}
	result, err := service.BeginEditSession(r.Context(), editSessionOwner(r.Context()), in)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, result)
}

func (h *Handler) RecoverableEditSessions(w http.ResponseWriter, r *http.Request) {
	service, err := h.editSessions()
	if err != nil {
		httpx.Error(w, err)
		return
	}
	result, err := service.RecoverableEditSessions(r.Context(), editSessionOwner(r.Context()), r.URL.Query().Get("applicationId"), r.URL.Query().Get("clientDraftKey"))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (h *Handler) GetEditSession(w http.ResponseWriter, r *http.Request) {
	service, err := h.editSessions()
	if err != nil {
		httpx.Error(w, err)
		return
	}
	result, err := service.GetEditSession(r.Context(), editSessionOwner(r.Context()), editSessionIDFromRequest(r))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (h *Handler) PatchEditSession(w http.ResponseWriter, r *http.Request) {
	service, err := h.editSessions()
	if err != nil {
		httpx.Error(w, err)
		return
	}
	var in PatchEditSessionInput
	if !httpx.Decode(w, r, &in) {
		return
	}
	result, err := service.PatchEditSession(r.Context(), editSessionOwner(r.Context()), editSessionIDFromRequest(r), in)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (h *Handler) PutEditSessionFile(w http.ResponseWriter, r *http.Request) {
	service, err := h.editSessions()
	if err != nil {
		httpx.Error(w, err)
		return
	}
	var in EditSessionFileInput
	if !httpx.Decode(w, r, &in) {
		return
	}
	key, ok := editIdempotencyKey(w, r)
	if !ok {
		return
	}
	result, err := service.PutEditSessionFile(r.Context(), editSessionOwner(r.Context()), editSessionIDFromRequest(r), strings.TrimSpace(r.PathValue("fileKey")), key, in)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (h *Handler) UploadEditSessionArchive(w http.ResponseWriter, r *http.Request) {
	service, err := h.editSessions()
	if err != nil {
		httpx.Error(w, err)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, persistentArchiveMaxBytes)
	if err := r.ParseMultipartForm(persistentArchiveFormMemory); err != nil {
		httpx.Error(w, panelerr.BadRequest("bad_request", "Invalid multipart request body"))
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		httpx.Error(w, panelerr.Validation("bad_request", "Archive file is required"))
		return
	}
	defer file.Close()
	content, err := io.ReadAll(file)
	if err != nil {
		httpx.Error(w, panelerr.BadRequest("bad_request", "Failed to read archive upload"))
		return
	}
	revision, _ := strconv.Atoi(r.FormValue("revision"))
	key, ok := editIdempotencyKey(w, r)
	if !ok {
		return
	}
	result, err := service.UploadEditSessionArchive(r.Context(), editSessionOwner(r.Context()), editSessionIDFromRequest(r), key, EditSessionArchiveInput{Revision: revision, ClientOperationID: r.FormValue("clientOperationId"), FileKey: r.FormValue("fileKey"), BasePath: r.FormValue("basePath"), Kind: r.FormValue("kind"), FileName: header.Filename, Content: content})
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (h *Handler) DeleteEditSessionFile(w http.ResponseWriter, r *http.Request) {
	service, err := h.editSessions()
	if err != nil {
		httpx.Error(w, err)
		return
	}
	var in EditSessionMutationInput
	if !httpx.Decode(w, r, &in) {
		return
	}
	key, ok := editIdempotencyKey(w, r)
	if !ok {
		return
	}
	result, err := service.DeleteEditSessionFile(r.Context(), editSessionOwner(r.Context()), editSessionIDFromRequest(r), strings.TrimSpace(r.PathValue("fileKey")), key, in)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (h *Handler) ValidateEditSession(w http.ResponseWriter, r *http.Request) {
	h.handleEditSessionRevisionAction(w, r, "validate")
}

func (h *Handler) PreviewEditSession(w http.ResponseWriter, r *http.Request) {
	h.handleEditSessionRevisionAction(w, r, "preview")
}

func (h *Handler) handleEditSessionRevisionAction(w http.ResponseWriter, r *http.Request, action string) {
	service, err := h.editSessions()
	if err != nil {
		httpx.Error(w, err)
		return
	}
	var in struct {
		Revision int `json:"revision"`
	}
	if !httpx.Decode(w, r, &in) {
		return
	}
	if action == "validate" {
		result, err := service.ValidateEditSession(r.Context(), editSessionOwner(r.Context()), editSessionIDFromRequest(r), in.Revision)
		if err != nil {
			httpx.Error(w, err)
			return
		}
		httpx.JSON(w, http.StatusOK, result)
		return
	}
	result, err := service.PreviewEditSession(r.Context(), editSessionOwner(r.Context()), editSessionIDFromRequest(r), in.Revision)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (h *Handler) CommitEditSession(w http.ResponseWriter, r *http.Request) {
	service, err := h.editSessions()
	if err != nil {
		httpx.Error(w, err)
		return
	}
	var in CommitEditSessionInput
	if !httpx.Decode(w, r, &in) {
		return
	}
	key, ok := editIdempotencyKey(w, r)
	if !ok {
		return
	}
	result, err := service.CommitEditSession(r.Context(), editSessionOwner(r.Context()), editSessionIDFromRequest(r), key, in)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (h *Handler) DiscardEditSession(w http.ResponseWriter, r *http.Request) {
	service, err := h.editSessions()
	if err != nil {
		httpx.Error(w, err)
		return
	}
	if err := service.DiscardEditSession(r.Context(), editSessionOwner(r.Context()), editSessionIDFromRequest(r)); err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.NoContent(w)
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	if summaryList, ok := h.service.(applicationSummaryListService); ok {
		page, pageSize, err := httpx.ParseListPage(r, "q")
		if err != nil {
			httpx.Error(w, err)
			return
		}
		summaries, err := summaryList.ListSummaries(r.Context(), page, pageSize, strings.TrimSpace(r.URL.Query().Get("q")))
		if err != nil {
			httpx.Error(w, err)
			return
		}
		httpx.JSON(w, http.StatusOK, summaries)
		return
	}
	var (
		apps []Application
		err  error
	)
	if runtimeList, ok := h.service.(applicationRuntimeListService); ok {
		apps, err = runtimeList.ListWithRuntime(r.Context())
	} else {
		apps, err = h.service.List(r.Context())
	}
	if err != nil {
		httpx.Error(w, err)
		return
	}
	summaries := make([]ApplicationSummary, 0, len(apps))
	for _, app := range apps {
		summaries = append(summaries, applicationSummaryFromApplication(app))
	}
	httpx.JSON(w, http.StatusOK, summaries)
}

func applicationSummaryFromApplication(app Application) ApplicationSummary {
	return ApplicationSummary{
		ID:                   app.ID,
		Name:                 app.Name,
		Enabled:              app.Enabled,
		ImageReference:       app.ImageReference,
		JobID:                app.JobID,
		Namespace:            app.Namespace,
		RuntimeStatus:        app.RuntimeStatus,
		ImageUpdateAvailable: app.ImageUpdateAvailable,
		LastError:            app.LastError,
		UpdatedAt:            app.UpdatedAt,
	}
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var in SaveInput
	if !httpx.Decode(w, r, &in) {
		return
	}
	app, err := h.service.Create(r.Context(), in)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, app)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	app, err := h.service.Get(r.Context(), applicationIDFromRequest(r))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, app)
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	var in SaveInput
	if !httpx.Decode(w, r, &in) {
		return
	}
	app, err := h.service.Update(r.Context(), applicationIDFromRequest(r), in)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, app)
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	if err := h.service.Delete(r.Context(), applicationIDFromRequest(r)); err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.NoContent(w)
}

func (h *Handler) ListFiles(w http.ResponseWriter, r *http.Request) {
	files, err := h.service.ListFiles(r.Context(), applicationIDFromRequest(r))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, files)
}

func (h *Handler) GetFile(w http.ResponseWriter, r *http.Request) {
	file, err := h.service.GetFile(r.Context(), applicationIDFromRequest(r), applicationFileIDFromRequest(r))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, file)
}

func (h *Handler) SaveFile(w http.ResponseWriter, r *http.Request) {
	var in FileSaveInput
	if !httpx.Decode(w, r, &in) {
		return
	}
	file, err := h.service.SaveFile(r.Context(), applicationIDFromRequest(r), in)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, file)
}

func (h *Handler) DeleteFile(w http.ResponseWriter, r *http.Request) {
	if err := h.service.DeleteFile(r.Context(), applicationIDFromRequest(r), applicationFileIDFromRequest(r)); err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.NoContent(w)
}

func (h *Handler) Package(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.Package(r.Context(), applicationIDFromRequest(r))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+safeDownloadName(result.Filename)+`"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(result.Content)
}

func (h *Handler) PersistentData(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.PersistentData(r.Context(), applicationIDFromRequest(r))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+safeDownloadName(result.Filename)+`"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(result.Content)
}

func (h *Handler) RestorePersistentData(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, persistentArchiveMaxBytes)
	if err := r.ParseMultipartForm(persistentArchiveFormMemory); err != nil {
		httpx.Error(w, panelerr.BadRequest("bad_request", "Invalid multipart request body"))
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		httpx.Error(w, panelerr.Validation("bad_request", "Archive file is required"))
		return
	}
	defer file.Close()
	content, err := io.ReadAll(file)
	if err != nil {
		httpx.Error(w, panelerr.BadRequest("bad_request", "Failed to read archive upload"))
		return
	}
	if len(content) == 0 {
		httpx.Error(w, panelerr.Validation("bad_request", "Archive file is required"))
		return
	}
	result, err := h.service.RestorePersistentData(r.Context(), applicationIDFromRequest(r), content)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (h *Handler) BeginSaveSession(w http.ResponseWriter, r *http.Request) {
	var in BeginSaveSessionInput
	if !httpx.Decode(w, r, &in) {
		return
	}
	result, err := h.service.BeginSaveSession(r.Context(), in)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, result)
}

func (h *Handler) UploadSaveSessionFile(w http.ResponseWriter, r *http.Request) {
	var in FileSaveInput
	if !httpx.Decode(w, r, &in) {
		return
	}
	file, err := h.service.UploadSaveSessionFile(r.Context(), saveSessionIDFromRequest(r), in)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, file)
}

func (h *Handler) UploadSaveSessionArchive(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, persistentArchiveMaxBytes)
	if err := r.ParseMultipartForm(persistentArchiveFormMemory); err != nil {
		httpx.Error(w, panelerr.BadRequest("bad_request", "Invalid multipart request body"))
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		httpx.Error(w, panelerr.Validation("bad_request", "Archive file is required"))
		return
	}
	defer file.Close()
	content, err := io.ReadAll(file)
	if err != nil {
		httpx.Error(w, panelerr.BadRequest("bad_request", "Failed to read archive upload"))
		return
	}
	files, err := h.service.UploadSaveSessionArchive(r.Context(), saveSessionIDFromRequest(r), FileArchiveInput{
		BasePath: r.FormValue("basePath"),
		Kind:     r.FormValue("kind"),
		FileName: header.Filename,
		Content:  content,
	})
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, files)
}

func (h *Handler) DeleteSaveSessionFile(w http.ResponseWriter, r *http.Request) {
	var in FileDeleteInput
	if !httpx.Decode(w, r, &in) {
		return
	}
	if err := h.service.DeleteSaveSessionFile(r.Context(), saveSessionIDFromRequest(r), in); err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.NoContent(w)
}

func (h *Handler) CommitSaveSession(w http.ResponseWriter, r *http.Request) {
	app, err := h.service.CommitSaveSession(r.Context(), saveSessionIDFromRequest(r))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, app)
}

func (h *Handler) Validate(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.Validate(r.Context(), applicationIDFromRequest(r))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (h *Handler) Plan(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.Plan(r.Context(), applicationIDFromRequest(r))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (h *Handler) CheckImageUpdate(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.CheckImageUpdate(r.Context(), applicationIDFromRequest(r))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (h *Handler) UpdateImage(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.UpdateImage(r.Context(), applicationIDFromRequest(r))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (h *Handler) Deploy(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.Deploy(r.Context(), applicationIDFromRequest(r))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (h *Handler) Migrate(w http.ResponseWriter, r *http.Request) {
	var in MigrationInput
	if !httpx.Decode(w, r, &in) {
		return
	}
	result, err := h.service.Migrate(r.Context(), applicationIDFromRequest(r), in)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (h *Handler) Stop(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.Stop(r.Context(), applicationIDFromRequest(r), r.URL.Query().Get("purge") == "true")
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (h *Handler) Restart(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.Restart(r.Context(), applicationIDFromRequest(r))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (h *Handler) Runtime(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.Runtime(r.Context(), applicationIDFromRequest(r))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (h *Handler) Logs(w http.ResponseWriter, r *http.Request) {
	tail, _ := strconv.Atoi(r.URL.Query().Get("tail"))
	result, err := h.service.Logs(r.Context(), applicationIDFromRequest(r), LogInput{
		InstanceID:    r.URL.Query().Get("instanceId"),
		ContainerName: r.URL.Query().Get("containerName"),
		Type:          r.URL.Query().Get("type"),
		Tail:          tail,
	})
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func applicationIDFromRequest(r *http.Request) string {
	return strings.TrimSpace(r.PathValue("id"))
}

func applicationFileIDFromRequest(r *http.Request) string {
	return strings.TrimSpace(r.PathValue("fileId"))
}

func saveSessionIDFromRequest(r *http.Request) string {
	return strings.TrimSpace(r.PathValue("id"))
}

func editSessionIDFromRequest(r *http.Request) string {
	return strings.TrimSpace(r.PathValue("id"))
}

func editIdempotencyKey(w http.ResponseWriter, r *http.Request) (string, bool) {
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		httpx.Error(w, panelerr.Validation("idempotency_key_required", "Idempotency-Key header is required"))
		return "", false
	}
	return key, true
}

func safeDownloadName(name string) string {
	name = strings.NewReplacer("\\", "-", "/", "-", `"`, "").Replace(name)
	name = strings.TrimSpace(name)
	if name == "" {
		return "application-package.zip"
	}
	return name
}
