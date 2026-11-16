package networkprobe

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	panelerr "panel/internal/platform/errors"
)

type fakeHandlerService struct {
	config        Config
	matrixFilter  []string
	serverResults string
	saveInput     SaveInput
}

func (f *fakeHandlerService) Get(context.Context) (Config, error) {
	return f.config, nil
}

func (f *fakeHandlerService) Save(_ context.Context, in SaveInput) (Config, error) {
	f.saveInput = in
	return f.config, nil
}

func (f *fakeHandlerService) Matrix(_ context.Context, filterServerIDs []string) (Matrix, error) {
	f.matrixFilter = filterServerIDs
	return Matrix{Mode: ModeAll, Nodes: []Node{}, Results: []Result{}}, nil
}

func (f *fakeHandlerService) ServerResults(_ context.Context, serverID string) (ServerResults, error) {
	f.serverResults = serverID
	return ServerResults{ServerID: serverID}, nil
}

func decodeEnvelope(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var envelope map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v (%s)", err, recorder.Body.String())
	}
	return envelope
}

// TestHandlerRoutes covers the facility HTTP surface: config read/save, matrix
// query filtering and the server-scoped results endpoint.
func TestHandlerRoutes(t *testing.T) {
	svc := &fakeHandlerService{config: Config{Mode: ModeAll, ServerIDs: []string{}, Version: 1}}
	handler := NewHandler(svc)

	rec := httptest.NewRecorder()
	handler.Config(rec, httptest.NewRequest(http.MethodGet, "/api/v1/facility-apps/network-probe", nil))
	if rec.Code != http.StatusOK || svc.config.Mode != ModeAll {
		t.Fatalf("config response = %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	handler.SaveConfig(rec, httptest.NewRequest(http.MethodPut, "/api/v1/facility-apps/network-probe", strings.NewReader(`{"mode":"selected","serverIds":["srv-a"]}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("save response = %d (%s)", rec.Code, rec.Body.String())
	}
	if svc.saveInput.Mode != ModeSelected || len(svc.saveInput.ServerIDs) != 1 || svc.saveInput.ServerIDs[0] != "srv-a" {
		t.Fatalf("decoded save input = %#v", svc.saveInput)
	}

	rec = httptest.NewRecorder()
	handler.Results(rec, httptest.NewRequest(http.MethodGet, "/api/v1/facility-apps/network-probe/results?serverIds=srv-a,srv-b&serverIds=srv-c", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("results response = %d", rec.Code)
	}
	if len(svc.matrixFilter) != 3 || svc.matrixFilter[0] != "srv-a" || svc.matrixFilter[1] != "srv-b" || svc.matrixFilter[2] != "srv-c" {
		t.Fatalf("matrix filter = %#v", svc.matrixFilter)
	}

	rec = httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/servers/srv-a/network-probes", nil)
	request.SetPathValue("id", "srv-a")
	handler.ServerResults(rec, request)
	if rec.Code != http.StatusOK || svc.serverResults != "srv-a" {
		t.Fatalf("server results response = %d serverID=%q", rec.Code, svc.serverResults)
	}
}

// TestHandlerMapsValidationErrors keeps the stable error codes visible on the
// wire.
func TestHandlerMapsValidationErrors(t *testing.T) {
	svc := &failingHandlerService{err: panelerr.Validation("network_probe_mode_invalid", "mode must be all or selected")}
	handler := NewHandler(svc)
	rec := httptest.NewRecorder()
	handler.SaveConfig(rec, httptest.NewRequest(http.MethodPut, "/api/v1/facility-apps/network-probe", strings.NewReader(`{"mode":"bogus"}`)))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	envelope := decodeEnvelope(t, rec)
	errorPayload, ok := envelope["error"].(map[string]any)
	if !ok || errorPayload["code"] != "network_probe_mode_invalid" {
		t.Fatalf("error payload = %#v", envelope["error"])
	}
}

type failingHandlerService struct {
	fakeHandlerService
	err error
}

func (f *failingHandlerService) Save(context.Context, SaveInput) (Config, error) {
	return Config{}, f.err
}
