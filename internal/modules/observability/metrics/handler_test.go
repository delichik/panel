package metrics

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"panel/internal/platform/linux"
)

func newMetricsHandlerRequest(t *testing.T, svc *Service, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.SetPathValue("id", "srv")
	NewHandler(svc).Query(rec, request)
	return rec
}

func decodeMetricsEnvelope(t *testing.T, rec *httptest.ResponseRecorder) (map[string]any, map[string]any) {
	t.Helper()
	var envelope struct {
		Data  map[string]any `json:"data"`
		Error map[string]any `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v (%s)", err, rec.Body.String())
	}
	return envelope.Data, envelope.Error
}

func metricsCPUPoints(t *testing.T, data map[string]any) []any {
	t.Helper()
	points, ok := data["cpu"].([]any)
	if !ok {
		t.Fatalf("cpu = %#v, want an array", data["cpu"])
	}
	return points
}

// TestMetricsQueryHandlerBucketValidationAndPassthrough covers the handler side of
// the bucketed metrics contract: the legacy raw response stays identical (no
// latest), a valid bucket downsamples and materializes latest, non-positive
// buckets fall back to the raw query and invalid buckets map to the stable
// bucket_invalid validation error.
func TestMetricsQueryHandlerBucketValidationAndPassthrough(t *testing.T) {
	svc, closeStore := newMetricsTestService(t)
	defer closeStore()
	ctx := context.Background()
	base := time.Now().UTC().Truncate(time.Second).Add(-2 * time.Minute)
	bucketStart := floorTimeToBucket(base, 60)
	for _, snap := range []linux.MetricsSnapshot{
		{ServerID: "srv", Time: bucketStart, CPUUsagePercent: 10},
		{ServerID: "srv", Time: bucketStart.Add(20 * time.Second), CPUUsagePercent: 20},
	} {
		if err := svc.Save(ctx, snap); err != nil {
			t.Fatal(err)
		}
	}

	// 未传 bucket：保持既有原始查询响应，不出现 latest。
	raw := newMetricsHandlerRequest(t, svc, "/api/v1/servers/srv/metrics?range=1h")
	if raw.Code != http.StatusOK {
		t.Fatalf("raw response = %d (%s)", raw.Code, raw.Body.String())
	}
	rawData, _ := decodeMetricsEnvelope(t, raw)
	if _, ok := rawData["latest"]; ok {
		t.Fatalf("raw response must not contain latest: %#v", rawData)
	}
	if _, ok := rawData["diskCurrent"]; !ok {
		t.Fatal("raw response must keep diskCurrent")
	}
	if cpu := metricsCPUPoints(t, rawData); len(cpu) != 2 {
		t.Fatalf("raw cpu points = %d, want 2", len(cpu))
	}

	// bucket=60：同一 60s 桶内的两个原始点聚合为 1 点，并附带 latest 当前值。
	bucketed := newMetricsHandlerRequest(t, svc, "/api/v1/servers/srv/metrics?range=1h&bucket=60")
	if bucketed.Code != http.StatusOK {
		t.Fatalf("bucketed response = %d (%s)", bucketed.Code, bucketed.Body.String())
	}
	data, _ := decodeMetricsEnvelope(t, bucketed)
	if data["range"] != "1h" {
		t.Fatalf("bucketed range = %#v, want 1h", data["range"])
	}
	cpu := metricsCPUPoints(t, data)
	if len(cpu) != 1 {
		t.Fatalf("bucketed cpu points = %d, want 1: %#v", len(cpu), cpu)
	}
	point, ok := cpu[0].(map[string]any)
	if !ok || point["usagePercent"] != 15.0 {
		t.Fatalf("bucketed cpu point = %#v, want the bucket average", cpu[0])
	}
	if point["usagePercentMin"] != 10.0 || point["usagePercentMax"] != 20.0 {
		t.Fatalf("bucketed cpu point range = %#v, want min 10 / max 20", point)
	}
	if at, err := time.Parse(time.RFC3339Nano, point["time"].(string)); err != nil || !at.Equal(bucketStart) {
		t.Fatalf("bucketed point time = %#v, want %s", point["time"], bucketStart)
	}
	latest, ok := data["latest"].(map[string]any)
	if !ok {
		t.Fatalf("bucketed response must contain latest: %#v", data)
	}
	if latest["cpuUsagePercent"] != 20.0 {
		t.Fatalf("latest.cpuUsagePercent = %#v, want the newest raw sample", latest["cpuUsagePercent"])
	}
	if _, ok := latest["time"].(string); !ok {
		t.Fatalf("latest.time = %#v", latest["time"])
	}
	if _, ok := latest["diskSampledAt"]; !ok {
		t.Fatalf("latest must always carry diskSampledAt (null when absent): %#v", latest)
	}
	if _, ok := data["diskCurrent"]; !ok {
		t.Fatal("bucketed response must keep diskCurrent")
	}

	// bucket=86400 合法上界仍走降采样；bucket<=0 回落到原始查询。
	if rec := newMetricsHandlerRequest(t, svc, "/api/v1/servers/srv/metrics?range=1d&bucket=86400"); rec.Code != http.StatusOK {
		t.Fatalf("bucket=86400 response = %d (%s)", rec.Code, rec.Body.String())
	}
	for _, query := range []string{"bucket=0", "bucket=-5"} {
		rec := newMetricsHandlerRequest(t, svc, "/api/v1/servers/srv/metrics?range=1h&"+query)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s response = %d (%s)", query, rec.Code, rec.Body.String())
		}
		fallback, _ := decodeMetricsEnvelope(t, rec)
		if _, ok := fallback["latest"]; ok {
			t.Fatalf("%s must fall back to the raw query without latest: %#v", query, fallback)
		}
		if got := metricsCPUPoints(t, fallback); len(got) != 2 {
			t.Fatalf("%s cpu points = %d, want the 2 raw points", query, len(got))
		}
	}

	// 非整数或越界 bucket：稳定的 422 bucket_invalid。
	for _, query := range []string{"bucket=abc", "bucket=59", "bucket=86401"} {
		rec := newMetricsHandlerRequest(t, svc, "/api/v1/servers/srv/metrics?range=1h&"+query)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("%s response = %d, want 422 (%s)", query, rec.Code, rec.Body.String())
		}
		_, errorPayload := decodeMetricsEnvelope(t, rec)
		if errorPayload["code"] != "bucket_invalid" {
			t.Fatalf("%s error payload = %#v, want bucket_invalid", query, errorPayload)
		}
	}
}
