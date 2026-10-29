package httpx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	panelerr "panel/internal/platform/errors"
	"panel/internal/platform/i18n"
)

func TestDecodeRejectsOversizedBody(t *testing.T) {
	payload := `{"data":"` + strings.Repeat("a", MaxRequestBodyBytes) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(payload))
	rec := httptest.NewRecorder()
	var v any
	if Decode(rec, req, &v) {
		t.Fatal("expected oversized body to be rejected")
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	var env Envelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error == nil || env.Error.Code != "request_body_too_large" {
		t.Fatalf("error = %#v, want code request_body_too_large", env.Error)
	}
}

func TestDecodeAcceptsValidBody(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"ok"}`))
	rec := httptest.NewRecorder()
	var payload struct {
		Name string `json:"name"`
	}
	if !Decode(rec, req, &payload) {
		t.Fatal("decode should succeed")
	}
	if payload.Name != "ok" {
		t.Fatalf("name = %q, want ok", payload.Name)
	}
}
func TestApplicationValidationErrorKeepsIssueDetails(t *testing.T) {
	i18n.SetDefaultLocale(i18n.LocaleSimplifiedChinese)
	defer i18n.SetDefaultLocale(i18n.LocaleEnglish)

	err := panelerr.WithDetails(panelerr.Validation("application_invalid", "image: image is required"), map[string]any{
		"issues": []map[string]string{{"field": "image", "message": "image is required"}},
	})
	rec := httptest.NewRecorder()

	Error(rec, err)

	var env Envelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error == nil {
		t.Fatalf("error envelope is nil")
	}
	if env.Error.Message != "image: 镜像不能为空" {
		t.Fatalf("message = %q", env.Error.Message)
	}
	if env.Error.Details == nil || env.Error.Details["issues"] == nil {
		t.Fatalf("details = %#v", env.Error.Details)
	}
	rawIssues, ok := env.Error.Details["issues"].([]any)
	if !ok || len(rawIssues) != 1 {
		t.Fatalf("issues = %#v", env.Error.Details["issues"])
	}
	issue, ok := rawIssues[0].(map[string]any)
	if !ok || issue["field"] != "image" || issue["message"] != "镜像不能为空" {
		t.Fatalf("issue = %#v", rawIssues[0])
	}
}
