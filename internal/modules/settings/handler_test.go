package settings

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPublicBrandingReturnsOnlyPublicFields(t *testing.T) {
	svc := newTestService(t)
	svc.mu.Lock()
	svc.rt.Branding = RuntimeBrandingSettings{
		LoginTitle:    "Operations",
		LoginSubtitle: "Manage infrastructure",
	}
	svc.mu.Unlock()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/settings/public-branding", nil)
	NewHandler(svc).PublicBranding(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
	var response struct {
		Data RuntimeBrandingSettings `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Data.LoginTitle != "Operations" || response.Data.LoginSubtitle != "Manage infrastructure" {
		t.Fatalf("unexpected public branding: %#v", response.Data)
	}
}
