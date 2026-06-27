package httpx

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	panelerr "panel/internal/platform/errors"
	"panel/internal/platform/i18n"
)

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
	if env.Error.Message != "image: image is required" {
		t.Fatalf("message = %q", env.Error.Message)
	}
	if env.Error.Details == nil || env.Error.Details["issues"] == nil {
		t.Fatalf("details = %#v", env.Error.Details)
	}
}
