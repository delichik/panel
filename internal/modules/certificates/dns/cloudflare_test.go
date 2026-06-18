package dns

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCloudflareListRecordsReadsEveryPage(t *testing.T) {
	requestedPages := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case "/zones":
			if r.URL.Query().Get("name") != "example.com" || r.URL.Query().Has("account.id") {
				t.Fatalf("zone query = %q", r.URL.RawQuery)
			}
			writeCloudflareJSON(t, w, map[string]any{"success": true, "result": []map[string]any{{"id": "zone_1", "name": "example.com"}}})
		case "/zones/zone_1/dns_records":
			page := r.URL.Query().Get("page")
			requestedPages = append(requestedPages, page)
			if r.URL.Query().Get("per_page") != "5000" {
				t.Fatalf("per_page = %q", r.URL.Query().Get("per_page"))
			}
			recordID := "record_" + page
			writeCloudflareJSON(t, w, map[string]any{
				"success":     true,
				"result":      []map[string]any{{"id": recordID, "name": page + ".example.com", "type": "A", "content": "192.0.2." + page, "ttl": 120}},
				"result_info": map[string]any{"total_pages": 2},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	provider := NewCloudflareProvider("secret", server.Client())
	provider.baseURL = server.URL
	records, err := provider.ListRecords(context.Background(), "example.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 || records[0].ID != "record_1" || records[1].ID != "record_2" {
		t.Fatalf("records = %#v", records)
	}
	if strings.Join(requestedPages, ",") != "1,2" {
		t.Fatalf("requested pages = %#v", requestedPages)
	}
}

func TestCloudflareCreateRecordNormalizesRelativeName(t *testing.T) {
	server := newCloudflareRecordServer(t, http.MethodPost, "www.example.com")
	defer server.Close()

	provider := NewCloudflareProvider("secret", server.Client())
	provider.baseURL = server.URL
	if _, err := provider.CreateRecord(context.Background(), "example.com", RecordInput{Name: "www", Type: "A", Value: "192.0.2.1"}); err != nil {
		t.Fatal(err)
	}
}

func TestCloudflareUpdateRecordNormalizesApexName(t *testing.T) {
	server := newCloudflareRecordServer(t, http.MethodPut, "example.com")
	defer server.Close()

	provider := NewCloudflareProvider("secret", server.Client())
	provider.baseURL = server.URL
	if _, err := provider.UpdateRecord(context.Background(), "example.com", "record_1", RecordInput{Name: "@", Type: "A", Value: "192.0.2.1"}); err != nil {
		t.Fatal(err)
	}
}

func TestCloudflareErrorUsesOfficialEnvelope(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		writeCloudflareJSON(t, w, map[string]any{
			"success": false,
			"errors":  []map[string]any{{"code": 10000, "message": "Authentication error"}},
		})
	}))
	defer server.Close()

	provider := NewCloudflareProvider("secret", server.Client())
	provider.baseURL = server.URL
	_, err := provider.ListRecords(context.Background(), "example.com")
	if err == nil || !strings.Contains(err.Error(), "10000: Authentication error") {
		t.Fatalf("error = %v", err)
	}
}

func newCloudflareRecordServer(t *testing.T, expectedMethod, expectedName string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/zones":
			writeCloudflareJSON(t, w, map[string]any{"success": true, "result": []map[string]any{{"id": "zone_1", "name": "example.com"}}})
		case "/zones/zone_1/dns_records", "/zones/zone_1/dns_records/record_1":
			if r.Method != expectedMethod {
				t.Fatalf("method = %q", r.Method)
			}
			raw, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatal(err)
			}
			var body map[string]any
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Fatal(err)
			}
			if body["name"] != expectedName {
				t.Fatalf("name = %#v", body["name"])
			}
			writeCloudflareJSON(t, w, map[string]any{
				"success": true,
				"result":  map[string]any{"id": "record_1", "name": expectedName, "type": "A", "content": "192.0.2.1", "ttl": 120},
			})
		default:
			http.NotFound(w, r)
		}
	}))
}

func writeCloudflareJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatal(err)
	}
}
