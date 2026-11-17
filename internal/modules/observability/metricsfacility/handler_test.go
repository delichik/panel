package metricsfacility

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	panelerr "panel/internal/platform/errors"
	"panel/internal/platform/i18n"
)

type fakeHandlerService struct {
	config    Config
	saveInput SaveInput
	err       error
}

func (f *fakeHandlerService) Get(context.Context) (Config, error) {
	if f.err != nil {
		return Config{}, f.err
	}
	return f.config, nil
}

func (f *fakeHandlerService) Save(_ context.Context, in SaveInput) (Config, error) {
	f.saveInput = in
	if f.err != nil {
		return Config{}, f.err
	}
	return f.config, nil
}

func decodeEnvelope(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var envelope map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v (%s)", err, recorder.Body.String())
	}
	return envelope
}

func dataOf(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	data, ok := decodeEnvelope(t, recorder)["data"].(map[string]any)
	if !ok {
		t.Fatalf("response data = %#v", recorder.Body.String())
	}
	return data
}

// TestHandlerRoutes covers the facility HTTP surface through the registered
// mux routes: GET config and PUT save with camelCase DTO fields.
func TestHandlerRoutes(t *testing.T) {
	updatedAt := time.Date(2026, 11, 17, 0, 0, 0, 0, time.UTC)
	svc := &fakeHandlerService{config: Config{
		Mode:                      ModeSelected,
		ServerIDs:                 []string{"srv-a"},
		RetentionDays:             14,
		CollectionIntervalSeconds: 120,
		CleanupSchedule:           "weekly",
		Version:                   3,
		UpdatedAt:                 updatedAt,
	}}
	handler := NewHandler(svc)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux, func(next http.Handler) http.Handler { return next })

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/facility-apps/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d (%s)", rec.Code, rec.Body.String())
	}
	data := dataOf(t, rec)
	if data["mode"] != ModeSelected || data["retentionDays"] != 14.0 || data["collectionIntervalSeconds"] != 120.0 ||
		data["cleanupSchedule"] != "weekly" || data["version"] != 3.0 {
		t.Fatalf("GET data = %#v", data)
	}
	ids, ok := data["serverIds"].([]any)
	if !ok || len(ids) != 1 || ids[0] != "srv-a" {
		t.Fatalf("serverIds = %#v", data["serverIds"])
	}
	if _, ok := data["updatedAt"].(string); !ok {
		t.Fatalf("updatedAt = %#v, want RFC3339 string", data["updatedAt"])
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/v1/facility-apps/metrics",
		strings.NewReader(`{"mode":"selected","serverIds":["srv-a","srv-b"],"retentionDays":3,"collectionIntervalSeconds":30,"cleanupSchedule":"hourly"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d (%s)", rec.Code, rec.Body.String())
	}
	if svc.saveInput.Mode != ModeSelected || len(svc.saveInput.ServerIDs) != 2 || svc.saveInput.ServerIDs[1] != "srv-b" ||
		svc.saveInput.RetentionDays != 3 || svc.saveInput.CollectionIntervalSeconds != 30 || svc.saveInput.CleanupSchedule != "hourly" {
		t.Fatalf("decoded save input = %#v", svc.saveInput)
	}
}

// TestHandlerMapsValidationErrors keeps the stable error codes visible on the
// wire for every metrics facility validation branch.
func TestHandlerMapsValidationErrors(t *testing.T) {
	codes := []string{
		"metrics_facility_mode_invalid",
		"metrics_facility_servers_required",
		"metrics_facility_server_not_found",
		"metrics_facility_retention_invalid",
		"metrics_facility_interval_invalid",
		"metrics_facility_cleanup_schedule_invalid",
	}
	for _, code := range codes {
		handler := NewHandler(&fakeHandlerService{err: panelerr.Validation(code, "fallback")})
		rec := httptest.NewRecorder()
		handler.SaveConfig(rec, httptest.NewRequest(http.MethodPut, "/api/v1/facility-apps/metrics", strings.NewReader(`{"mode":"all"}`)))
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("%s: status = %d, want 422", code, rec.Code)
		}
		errorPayload, ok := decodeEnvelope(t, rec)["error"].(map[string]any)
		if !ok || errorPayload["code"] != code {
			t.Fatalf("%s: error payload = %#v", code, decodeEnvelope(t, rec)["error"])
		}
	}
}

// TestValidationErrorCodesHaveChineseTranslations pins the six zh-CN messages
// carried by i18n.codeTranslations.
func TestValidationErrorCodesHaveChineseTranslations(t *testing.T) {
	want := map[string]string{
		"metrics_facility_mode_invalid":             "指标采集模式必须为 all 或 selected",
		"metrics_facility_servers_required":         "选择指定节点模式时必须至少选择一个采集节点",
		"metrics_facility_server_not_found":         "所选采集节点不存在",
		"metrics_facility_retention_invalid":        "指标保留天数至少为 1 天",
		"metrics_facility_interval_invalid":         "指标采集间隔至少为 1 秒",
		"metrics_facility_cleanup_schedule_invalid": "指标清理计划必须为每小时、每天或每周",
	}
	for code, expected := range want {
		if got := i18n.TranslateLocale(i18n.LocaleSimplifiedChinese, code, "fallback"); got != expected {
			t.Fatalf("translation for %s = %q, want %q", code, got, expected)
		}
	}
}
