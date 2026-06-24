package server

import (
	"net/http"

	httpx "panel/internal/platform/http"
)

func (h *Handler) RegisterRoutes(mux *http.ServeMux, auth httpx.Middleware) {
	mux.Handle("GET /api/v1/servers", auth(http.HandlerFunc(h.List)))
	mux.Handle("POST /api/v1/servers", auth(http.HandlerFunc(h.Create)))
	mux.Handle("POST /api/v1/servers/probe", auth(http.HandlerFunc(h.Probe)))
	mux.Handle("PUT /api/v1/servers/{id}", auth(http.HandlerFunc(h.Update)))
	mux.Handle("DELETE /api/v1/servers/{id}", auth(http.HandlerFunc(h.Delete)))
	mux.Handle("POST /api/v1/servers/{id}/test", auth(http.HandlerFunc(h.Test)))
	mux.Handle("POST /api/v1/servers/{id}/restart", auth(http.HandlerFunc(h.Restart)))
	mux.Handle("POST /api/v1/servers/{id}/agent/certificate", auth(http.HandlerFunc(h.IssueAgentCertificate)))
	mux.Handle("POST /api/v1/servers/{id}/agent/deploy", auth(http.HandlerFunc(h.DeployAgent)))
	mux.Handle("POST /api/v1/servers/{id}/ufw/install", auth(http.HandlerFunc(h.InstallUFW)))
	mux.Handle("GET /api/v1/servers/{id}/ufw", auth(http.HandlerFunc(h.UFWState)))
	mux.Handle("POST /api/v1/servers/{id}/ufw/rules", auth(http.HandlerFunc(h.AllowUFW)))
	mux.Handle("POST /api/v1/servers/{id}/ufw/enable", auth(http.HandlerFunc(h.EnableUFW)))
	mux.Handle("DELETE /api/v1/servers/{id}/ufw/rules/{number}", auth(http.HandlerFunc(h.DeleteUFWRule)))
	mux.Handle("GET /api/v1/servers/{id}/fail2ban", auth(http.HandlerFunc(h.Fail2BanState)))
	mux.Handle("PUT /api/v1/servers/{id}/fail2ban", auth(http.HandlerFunc(h.SaveFail2Ban)))
	mux.Handle("POST /api/v1/servers/{id}/fail2ban/install", auth(http.HandlerFunc(h.InstallFail2Ban)))
	mux.Handle("GET /api/v1/key-assets/system", auth(http.HandlerFunc(h.SystemCertificates)))
	mux.Handle("POST /api/v1/key-assets/system/{id}/reset", auth(http.HandlerFunc(h.ResetSystemCertificate)))
}
