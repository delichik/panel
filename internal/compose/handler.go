package compose

import (
	"net/http"
	"strings"

	"panel/internal/httpx"
)

type Handler struct{ service *Service }

func NewHandler(service *Service) *Handler { return &Handler{service: service} }

func (h *Handler) ListTemplates(w http.ResponseWriter, r *http.Request) {
	out, err := h.service.ListTemplates(r.Context())
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) CreateTemplate(w http.ResponseWriter, r *http.Request) {
	var req SaveTemplateRequest
	if !httpx.Decode(w, r, &req) {
		return
	}
	out, err := h.service.CreateTemplate(r.Context(), req)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, out)
}

func (h *Handler) GetTemplate(w http.ResponseWriter, r *http.Request) {
	out, err := h.service.GetTemplate(r.Context(), templateID(r.URL.Path))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) UpdateTemplate(w http.ResponseWriter, r *http.Request) {
	var req SaveTemplateRequest
	if !httpx.Decode(w, r, &req) {
		return
	}
	out, err := h.service.UpdateTemplate(r.Context(), templateID(r.URL.Path), req)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) DeleteTemplate(w http.ResponseWriter, r *http.Request) {
	if err := h.service.DeleteTemplate(r.Context(), templateID(r.URL.Path)); err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.NoContent(w)
}

func (h *Handler) ValidateTemplate(w http.ResponseWriter, r *http.Request) {
	var req RenderRequest
	if !httpx.Decode(w, r, &req) {
		return
	}
	if err := h.service.ValidateTemplate(r.Context(), templateID(r.URL.Path), req); err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, ValidateResult{Valid: true})
}

func (h *Handler) RenderTemplate(w http.ResponseWriter, r *http.Request) {
	var req RenderRequest
	if !httpx.Decode(w, r, &req) {
		return
	}
	out, err := h.service.RenderTemplate(r.Context(), templateID(r.URL.Path), req)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) TemplateServices(w http.ResponseWriter, r *http.Request) {
	out, err := h.service.ListTemplateServices(r.Context(), templateID(r.URL.Path))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) ListFiles(w http.ResponseWriter, r *http.Request) {
	out, err := h.service.ListTemplateFiles(r.Context(), templateID(r.URL.Path))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) CreateBinaryFile(w http.ResponseWriter, r *http.Request) {
	h.createFile(w, r, FileKindBinary)
}

func (h *Handler) CreateTemplateFile(w http.ResponseWriter, r *http.Request) {
	h.createFile(w, r, FileKindTemplate)
}

func (h *Handler) createFile(w http.ResponseWriter, r *http.Request, kind string) {
	var req SaveFileRequest
	if !httpx.Decode(w, r, &req) {
		return
	}
	out, err := h.service.CreateTemplateFile(r.Context(), templateID(r.URL.Path), kind, req)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, out)
}

func (h *Handler) UpdateFile(w http.ResponseWriter, r *http.Request) {
	var req SaveFileRequest
	if !httpx.Decode(w, r, &req) {
		return
	}
	tid, fid := templateAndFileID(r.URL.Path)
	out, err := h.service.UpdateTemplateFile(r.Context(), tid, fid, req)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) DeleteFile(w http.ResponseWriter, r *http.Request) {
	tid, fid := templateAndFileID(r.URL.Path)
	if err := h.service.DeleteTemplateFile(r.Context(), tid, fid); err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.NoContent(w)
}

func (h *Handler) GetServerVariables(w http.ResponseWriter, r *http.Request) {
	out, err := h.service.ServerVariables(r.Context(), serverID(r.URL.Path))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) PutServerVariables(w http.ResponseWriter, r *http.Request) {
	var req map[string]any
	if !httpx.Decode(w, r, &req) {
		return
	}
	out, err := h.service.PutServerVariables(r.Context(), serverID(r.URL.Path), req)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) ListServices(w http.ResponseWriter, r *http.Request) {
	out, err := h.service.ListServices(r.Context())
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) CreateService(w http.ResponseWriter, r *http.Request) {
	var req SaveServiceRequest
	if !httpx.Decode(w, r, &req) {
		return
	}
	out, err := h.service.CreateService(r.Context(), req)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, out)
}

func (h *Handler) GetService(w http.ResponseWriter, r *http.Request) {
	out, err := h.service.GetService(r.Context(), serviceID(r.URL.Path))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) UpdateService(w http.ResponseWriter, r *http.Request) {
	var req SaveServiceRequest
	if !httpx.Decode(w, r, &req) {
		return
	}
	out, err := h.service.UpdateService(r.Context(), serviceID(r.URL.Path), req)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) DeleteService(w http.ResponseWriter, r *http.Request) {
	if err := h.service.DeleteService(r.Context(), serviceID(r.URL.Path)); err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.NoContent(w)
}

func (h *Handler) RenderService(w http.ResponseWriter, r *http.Request) {
	out, err := h.service.RenderService(r.Context(), serviceID(strings.TrimSuffix(r.URL.Path, "/render")))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) Lifecycle(w http.ResponseWriter, r *http.Request) {
	sid, op := serviceAndOperation(r.URL.Path)
	task, err := h.service.LifecycleTask(r.Context(), sid, op)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, map[string]any{"taskId": task.ID})
}

func templateID(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) >= 4 {
		return parts[3]
	}
	return ""
}

func serviceID(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) >= 4 {
		return parts[3]
	}
	return ""
}

func serverID(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) >= 4 {
		return parts[3]
	}
	return ""
}

func templateAndFileID(path string) (string, string) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) >= 6 {
		return parts[3], parts[5]
	}
	return templateID(path), ""
}

func serviceAndOperation(path string) (string, string) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) >= 5 {
		return parts[3], parts[4]
	}
	return serviceID(path), ""
}
