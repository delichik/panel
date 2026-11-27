package networkprobe

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	panelerr "panel/internal/platform/errors"
)

type fakeHandlerService struct {
	config                 Config
	matrixFilter           []string
	matrixRequested        bool
	targetResultsRequested bool
	targetHistoryRequested bool
	serverResults          string
	saveInput              SaveInput
	historyRequested       bool
}

func (f *fakeHandlerService) Get(context.Context) (Config, error) {
	return f.config, nil
}

func (f *fakeHandlerService) Save(_ context.Context, in SaveInput) (Config, error) {
	f.saveInput = in
	return f.config, nil
}

func (f *fakeHandlerService) Matrix(_ context.Context, filterServerIDs []string) (Matrix, error) {
	f.matrixRequested = true
	f.matrixFilter = filterServerIDs
	return Matrix{Mode: ModeAll, Nodes: []Node{}, Results: []Result{}}, nil
}

func (f *fakeHandlerService) TargetResults(_ context.Context, withHistory bool) (TargetMatrix, error) {
	f.targetResultsRequested = true
	f.targetHistoryRequested = withHistory
	return TargetMatrix{Nodes: []TargetResult{{
		ServerID:    "srv-a",
		Name:        "A",
		Address:     "10.0.0.1",
		SourceCount: 2,
	}}}, nil
}

func (f *fakeHandlerService) ServerResults(_ context.Context, serverID string) (ServerResults, error) {
	f.serverResults = serverID
	f.historyRequested = false
	return ServerResults{ServerID: serverID, Results: []Result{{TargetServerID: "srv-b"}}}, nil
}

func (f *fakeHandlerService) ServerResultsHistory(_ context.Context, serverID string) (ServerResults, error) {
	f.serverResults = serverID
	f.historyRequested = true
	return ServerResults{ServerID: serverID, Results: []Result{{
		TargetServerID: "srv-b",
		History:        []HistoryPoint{{Time: time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC), LatencyMS: 1.5, PacketLossPercent: 2}},
	}}}, nil
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
	svc := &fakeHandlerService{config: Config{Mode: ModeAll, ServerIDs: []string{}, IntervalSeconds: 120, PingCount: 5, PingTimeoutSeconds: 4, Version: 1}}
	handler := NewHandler(svc)

	rec := httptest.NewRecorder()
	handler.Config(rec, httptest.NewRequest(http.MethodGet, "/api/v1/facility-apps/network-probe", nil))
	if rec.Code != http.StatusOK || svc.config.Mode != ModeAll {
		t.Fatalf("config response = %d", rec.Code)
	}
	configData, ok := decodeEnvelope(t, rec)["data"].(map[string]any)
	if !ok || configData["intervalSeconds"] != 120.0 || configData["pingCount"] != 5.0 || configData["pingTimeoutSeconds"] != 4.0 {
		t.Fatalf("config response must expose probe parameters: %#v", configData)
	}

	rec = httptest.NewRecorder()
	handler.SaveConfig(rec, httptest.NewRequest(http.MethodPut, "/api/v1/facility-apps/network-probe", strings.NewReader(`{"mode":"selected","serverIds":["srv-a"],"intervalSeconds":120,"pingCount":5,"pingTimeoutSeconds":4}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("save response = %d (%s)", rec.Code, rec.Body.String())
	}
	if svc.saveInput.Mode != ModeSelected || len(svc.saveInput.ServerIDs) != 1 || svc.saveInput.ServerIDs[0] != "srv-a" {
		t.Fatalf("decoded save input = %#v", svc.saveInput)
	}
	if svc.saveInput.IntervalSeconds != 120 || svc.saveInput.PingCount != 5 || svc.saveInput.PingTimeoutSeconds != 4 {
		t.Fatalf("decoded probe parameters = %#v", svc.saveInput)
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
	if svc.historyRequested {
		t.Fatal("default request must not ask the service for probe history")
	}
	if got := firstResultField(t, rec, "history"); got != nil {
		t.Fatalf("default response must not contain history: %v", got)
	}

	// history=1 / history=true 才填充历史曲线。
	for _, query := range []string{"history=1", "history=true"} {
		rec = httptest.NewRecorder()
		request = httptest.NewRequest(http.MethodGet, "/api/v1/servers/srv-a/network-probes?"+query, nil)
		request.SetPathValue("id", "srv-a")
		handler.ServerResults(rec, request)
		if rec.Code != http.StatusOK || !svc.historyRequested {
			t.Fatalf("%s: response = %d historyRequested=%v", query, rec.Code, svc.historyRequested)
		}
		history, ok := firstResultField(t, rec, "history").([]any)
		if !ok || len(history) != 1 {
			t.Fatalf("%s: history = %#v, want one point", query, firstResultField(t, rec, "history"))
		}
		point, ok := history[0].(map[string]any)
		if !ok || point["latencyMs"] != 1.5 || point["packetLossPercent"] != 2.0 {
			t.Fatalf("%s: history point = %#v", query, history[0])
		}
		if _, ok := point["time"]; !ok {
			t.Fatalf("%s: history point must carry an RFC3339 time: %#v", query, point)
		}
	}

	// 其它取值保持既有响应（不请求历史）。
	for _, query := range []string{"history=0", "history=yes"} {
		svc.historyRequested = true
		rec = httptest.NewRecorder()
		request = httptest.NewRequest(http.MethodGet, "/api/v1/servers/srv-a/network-probes?"+query, nil)
		request.SetPathValue("id", "srv-a")
		handler.ServerResults(rec, request)
		if rec.Code != http.StatusOK || svc.historyRequested {
			t.Fatalf("%s: response = %d historyRequested=%v, want the plain response", query, rec.Code, svc.historyRequested)
		}
	}
}

// firstResultField 返回响应 data.results[0] 的指定字段。
func firstResultField(t *testing.T, recorder *httptest.ResponseRecorder, field string) any {
	t.Helper()
	envelope := decodeEnvelope(t, recorder)
	data, ok := envelope["data"].(map[string]any)
	if !ok {
		t.Fatalf("response data = %#v", envelope["data"])
	}
	results, ok := data["results"].([]any)
	if !ok || len(results) == 0 {
		t.Fatalf("response results = %#v", data["results"])
	}
	result, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("response result = %#v", results[0])
	}
	return result[field]
}

// TestHandlerResultsViews keeps the default matrix view and adds the
// target-aggregated view selected by view=targets / targets=1, with history=1
// forwarded to the service.
func TestHandlerResultsViews(t *testing.T) {
	svc := &fakeHandlerService{}
	handler := NewHandler(svc)

	rec := httptest.NewRecorder()
	handler.Results(rec, httptest.NewRequest(http.MethodGet, "/api/v1/facility-apps/network-probe/results?serverIds=srv-a", nil))
	if rec.Code != http.StatusOK || !svc.matrixRequested || svc.targetResultsRequested {
		t.Fatalf("default view must stay the matrix: code=%d matrix=%v targets=%v", rec.Code, svc.matrixRequested, svc.targetResultsRequested)
	}

	for _, request := range []string{"view=targets", "targets=1", "targets=true"} {
		svc.targetResultsRequested = false
		svc.targetHistoryRequested = true
		rec = httptest.NewRecorder()
		handler.Results(rec, httptest.NewRequest(http.MethodGet, "/api/v1/facility-apps/network-probe/results?"+request, nil))
		if rec.Code != http.StatusOK || !svc.targetResultsRequested || svc.targetHistoryRequested {
			t.Fatalf("%s: code=%d targets=%v history=%v", request, rec.Code, svc.targetResultsRequested, svc.targetHistoryRequested)
		}
		data, ok := decodeEnvelope(t, rec)["data"].(map[string]any)
		if !ok {
			t.Fatalf("%s: data = %#v", request, decodeEnvelope(t, rec)["data"])
		}
		nodes, ok := data["nodes"].([]any)
		if !ok || len(nodes) != 1 {
			t.Fatalf("%s: nodes = %#v", request, data["nodes"])
		}
		node, ok := nodes[0].(map[string]any)
		if !ok || node["serverId"] != "srv-a" || node["sourceCount"] != 2.0 {
			t.Fatalf("%s: node = %#v", request, nodes[0])
		}
		if _, ok := node["latencyMs"]; !ok {
			t.Fatalf("%s: target node must expose latencyMs (null allowed): %#v", request, node)
		}
	}

	svc.targetResultsRequested = false
	svc.targetHistoryRequested = false
	rec = httptest.NewRecorder()
	handler.Results(rec, httptest.NewRequest(http.MethodGet, "/api/v1/facility-apps/network-probe/results?view=targets&history=1", nil))
	if rec.Code != http.StatusOK || !svc.targetResultsRequested || !svc.targetHistoryRequested {
		t.Fatalf("view=targets&history=1: code=%d targets=%v history=%v", rec.Code, svc.targetResultsRequested, svc.targetHistoryRequested)
	}
}

// TestHandlerMapsValidationErrors keeps the stable error codes visible on the
// wire.
func TestHandlerMapsValidationErrors(t *testing.T) {
	cases := map[string]string{
		"network_probe_mode_invalid":         "mode must be all or selected",
		"network_probe_interval_invalid":     "intervalSeconds must be between 60 and 3600",
		"network_probe_ping_count_invalid":   "pingCount must be between 1 and 10",
		"network_probe_ping_timeout_invalid": "pingTimeoutSeconds must be between 1 and 10",
	}
	for code, message := range cases {
		svc := &failingHandlerService{err: panelerr.Validation(code, message)}
		handler := NewHandler(svc)
		rec := httptest.NewRecorder()
		handler.SaveConfig(rec, httptest.NewRequest(http.MethodPut, "/api/v1/facility-apps/network-probe", strings.NewReader(`{"mode":"all"}`)))
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("%s: status = %d, want 422", code, rec.Code)
		}
		envelope := decodeEnvelope(t, rec)
		errorPayload, ok := envelope["error"].(map[string]any)
		if !ok || errorPayload["code"] != code {
			t.Fatalf("%s: error payload = %#v", code, envelope["error"])
		}
	}
}

type failingHandlerService struct {
	fakeHandlerService
	err error
}

func (f *failingHandlerService) Save(context.Context, SaveInput) (Config, error) {
	return Config{}, f.err
}
