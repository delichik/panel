package applications

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"panel/internal/httpx"
)

type applicationService interface {
	List(ctx context.Context) ([]Application, error)
	Get(ctx context.Context, id string) (Application, error)
	Create(ctx context.Context, in SaveInput) (Application, error)
	Update(ctx context.Context, id string, in SaveInput) (Application, error)
	Delete(ctx context.Context, id string) error
	ListFiles(ctx context.Context, id string) ([]ApplicationFile, error)
	SaveFile(ctx context.Context, id string, in FileSaveInput) (ApplicationFile, error)
	DeleteFile(ctx context.Context, id, fileID string) error
	BeginSaveSession(ctx context.Context, in BeginSaveSessionInput) (SaveSessionResult, error)
	UploadSaveSessionFile(ctx context.Context, sessionID string, in FileSaveInput) (ApplicationFile, error)
	DeleteSaveSessionFile(ctx context.Context, sessionID string, in FileDeleteInput) error
	CommitSaveSession(ctx context.Context, sessionID string) (Application, error)
	Package(ctx context.Context, id string) (PackageResult, error)
	PersistentData(ctx context.Context, id string) (PackageResult, error)
	Validate(ctx context.Context, id string) (ValidationResult, error)
	Plan(ctx context.Context, id string) (PlanResult, error)
	CheckImageUpdate(ctx context.Context, id string) (Application, error)
	UpdateImage(ctx context.Context, id string) (OperationResult, error)
	Deploy(ctx context.Context, id string) (OperationResult, error)
	Stop(ctx context.Context, id string, purge bool) (OperationResult, error)
	Restart(ctx context.Context, id string) (OperationResult, error)
	Runtime(ctx context.Context, id string) (ApplicationRuntime, error)
	Logs(ctx context.Context, id string, in LogInput) (LogResult, error)
	TemplateCatalog(ctx context.Context) (TemplateCatalog, error)
}

type applicationRuntimeListService interface {
	ListWithRuntime(ctx context.Context) ([]Application, error)
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

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
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
	httpx.JSON(w, http.StatusOK, apps)
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
	app, err := h.service.Get(r.Context(), applicationID(r.URL.Path))
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
	app, err := h.service.Update(r.Context(), applicationID(r.URL.Path), in)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, app)
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	if err := h.service.Delete(r.Context(), applicationID(r.URL.Path)); err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.NoContent(w)
}

func (h *Handler) ListFiles(w http.ResponseWriter, r *http.Request) {
	files, err := h.service.ListFiles(r.Context(), applicationID(r.URL.Path))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, files)
}

func (h *Handler) SaveFile(w http.ResponseWriter, r *http.Request) {
	var in FileSaveInput
	if !httpx.Decode(w, r, &in) {
		return
	}
	file, err := h.service.SaveFile(r.Context(), applicationID(r.URL.Path), in)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, file)
}

func (h *Handler) DeleteFile(w http.ResponseWriter, r *http.Request) {
	if err := h.service.DeleteFile(r.Context(), applicationID(r.URL.Path), applicationFileID(r.URL.Path)); err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.NoContent(w)
}

func (h *Handler) Package(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.Package(r.Context(), applicationID(r.URL.Path))
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
	result, err := h.service.PersistentData(r.Context(), applicationID(r.URL.Path))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+safeDownloadName(result.Filename)+`"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(result.Content)
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
	file, err := h.service.UploadSaveSessionFile(r.Context(), saveSessionID(r.URL.Path), in)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, file)
}

func (h *Handler) DeleteSaveSessionFile(w http.ResponseWriter, r *http.Request) {
	var in FileDeleteInput
	if !httpx.Decode(w, r, &in) {
		return
	}
	if err := h.service.DeleteSaveSessionFile(r.Context(), saveSessionID(r.URL.Path), in); err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.NoContent(w)
}

func (h *Handler) CommitSaveSession(w http.ResponseWriter, r *http.Request) {
	app, err := h.service.CommitSaveSession(r.Context(), saveSessionID(r.URL.Path))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, app)
}

func (h *Handler) Validate(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.Validate(r.Context(), applicationID(r.URL.Path))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (h *Handler) Plan(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.Plan(r.Context(), applicationID(r.URL.Path))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (h *Handler) CheckImageUpdate(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.CheckImageUpdate(r.Context(), applicationID(r.URL.Path))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (h *Handler) UpdateImage(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.UpdateImage(r.Context(), applicationID(r.URL.Path))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (h *Handler) Deploy(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.Deploy(r.Context(), applicationID(r.URL.Path))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (h *Handler) Stop(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.Stop(r.Context(), applicationID(r.URL.Path), r.URL.Query().Get("purge") == "true")
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (h *Handler) Restart(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.Restart(r.Context(), applicationID(r.URL.Path))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (h *Handler) Runtime(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.Runtime(r.Context(), applicationID(r.URL.Path))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (h *Handler) Logs(w http.ResponseWriter, r *http.Request) {
	tail, _ := strconv.Atoi(r.URL.Query().Get("tail"))
	result, err := h.service.Logs(r.Context(), applicationID(r.URL.Path), LogInput{
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

func applicationID(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) >= 4 {
		return parts[3]
	}
	return ""
}

func applicationFileID(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) >= 6 {
		return parts[5]
	}
	return ""
}

func saveSessionID(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) >= 4 {
		return parts[3]
	}
	return ""
}

func safeDownloadName(name string) string {
	name = strings.NewReplacer("\\", "-", "/", "-", `"`, "").Replace(name)
	name = strings.TrimSpace(name)
	if name == "" {
		return "application-package.zip"
	}
	return name
}
