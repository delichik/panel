package sshaccess

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	panelerr "panel/internal/platform/errors"
	httpx "panel/internal/platform/http"
)

// service 是 Handler 依赖的服务面。
type service interface {
	ListSessions(ctx context.Context, serverID string, filter SessionFilter) (SessionList, error)
	ListAttempts(ctx context.Context, serverID string, filter AttemptFilter) (AttemptList, error)
}

// Handler 暴露服务器 SSH 访问记录的 HTTP 入口。
type Handler struct {
	service service
}

// NewHandler 创建 Handler。
func NewHandler(service service) *Handler {
	return &Handler{service: service}
}

// Sessions 返回成功登录会话：GET /api/v1/servers/{id}/ssh-access/sessions。
func (h *Handler) Sessions(w http.ResponseWriter, r *http.Request) {
	filter, err := parseSessionFilter(r)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	result, err := h.service.ListSessions(r.Context(), r.PathValue("id"), filter)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

// Attempts 返回失败登录尝试：GET /api/v1/servers/{id}/ssh-access/attempts。
func (h *Handler) Attempts(w http.ResponseWriter, r *http.Request) {
	filter, err := parseAttemptFilter(r)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	result, err := h.service.ListAttempts(r.Context(), r.PathValue("id"), filter)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func parseSessionFilter(r *http.Request) (SessionFilter, error) {
	query := r.URL.Query()
	page, err := parseIntQuery(query.Get("page"), "page")
	if err != nil {
		return SessionFilter{}, err
	}
	pageSize, err := parseIntQuery(query.Get("pageSize"), "pageSize")
	if err != nil {
		return SessionFilter{}, err
	}
	from, err := parseTimeQuery(query.Get("from"))
	if err != nil {
		return SessionFilter{}, err
	}
	to, err := parseTimeQuery(query.Get("to"))
	if err != nil {
		return SessionFilter{}, err
	}
	return SessionFilter{
		State: query.Get("state"), From: from, To: to,
		User: query.Get("user"), Source: query.Get("source"), Query: query.Get("q"),
		Page: page, PageSize: pageSize,
	}, nil
}

func parseAttemptFilter(r *http.Request) (AttemptFilter, error) {
	query := r.URL.Query()
	page, err := parseIntQuery(query.Get("page"), "page")
	if err != nil {
		return AttemptFilter{}, err
	}
	pageSize, err := parseIntQuery(query.Get("pageSize"), "pageSize")
	if err != nil {
		return AttemptFilter{}, err
	}
	from, err := parseTimeQuery(query.Get("from"))
	if err != nil {
		return AttemptFilter{}, err
	}
	to, err := parseTimeQuery(query.Get("to"))
	if err != nil {
		return AttemptFilter{}, err
	}
	return AttemptFilter{
		From: from, To: to,
		User: query.Get("user"), Source: query.Get("source"), Query: query.Get("q"),
		Page: page, PageSize: pageSize,
	}, nil
}

func parseIntQuery(raw, name string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return 0, panelerr.Validation("ssh_access_page_invalid", name+" must be a positive integer")
	}
	return value, nil
}

func parseTimeQuery(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, nil
	}
	if seconds, err := strconv.ParseInt(raw, 10, 64); err == nil {
		return time.Unix(seconds, 0).UTC(), nil
	}
	if value, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return value.UTC(), nil
	}
	return time.Time{}, panelerr.Validation("ssh_access_time_invalid", "time must be RFC3339 or unix seconds")
}
