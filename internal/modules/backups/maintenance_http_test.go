package backups

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"panel/internal/platform/config"
)

func TestExportAppStaticDoesNotServeHTMLForUnknownAPI(t *testing.T) {
	app := &ExportApp{}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/settings/public-branding", nil)
	rec := httptest.NewRecorder()

	app.static(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
	if contentType := rec.Header().Get("Content-Type"); !strings.Contains(contentType, "application/json") {
		t.Fatalf("expected JSON response, got %q", contentType)
	}
	if strings.Contains(rec.Body.String(), "<!doctype html") {
		t.Fatal("expected API fallback not to serve frontend HTML")
	}
}

func TestRestoreAppPageDoesNotServeHTMLForUnknownAPI(t *testing.T) {
	app := &RestoreApp{}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/settings/public-branding", nil)
	rec := httptest.NewRecorder()

	app.page(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
	if contentType := rec.Header().Get("Content-Type"); !strings.Contains(contentType, "application/json") {
		t.Fatalf("expected JSON response, got %q", contentType)
	}
	if strings.Contains(rec.Body.String(), "<!doctype html") {
		t.Fatal("expected API fallback not to serve restore HTML")
	}
}

func TestExportDownloadNotFoundReturnsJSON(t *testing.T) {
	app := &ExportApp{
		cfg: config.Config{DataRoot: t.TempDir()},
		status: Status{
			Phase:    PhaseReady,
			ExportID: "expected",
		},
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/backups/export/missing/download", nil)
	req.SetPathValue("id", "missing")
	rec := httptest.NewRecorder()

	app.downloadAPI(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
	if contentType := rec.Header().Get("Content-Type"); !strings.Contains(contentType, "application/json") {
		t.Fatalf("expected JSON response, got %q", contentType)
	}
}
