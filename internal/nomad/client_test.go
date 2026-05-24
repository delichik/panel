package nomad

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientAddsTokenAndNamespace(t *testing.T) {
	var gotToken, gotNamespace string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("X-Nomad-Token")
		gotNamespace = r.URL.Query().Get("namespace")
		_ = json.NewEncoder(w).Encode([]JobListItem{})
	}))
	defer server.Close()

	client := NewClient(Config{
		Address:   server.URL,
		Token:     "secret-token",
		Namespace: "apps",
	}, server.Client())

	if _, err := client.ListJobs(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if gotToken != "secret-token" {
		t.Fatalf("token header = %q", gotToken)
	}
	if gotNamespace != "apps" {
		t.Fatalf("namespace query = %q", gotNamespace)
	}
}

func TestClientBuildsListJobsPathAndPrefix(t *testing.T) {
	var gotPath, gotPrefix string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotPrefix = r.URL.Query().Get("prefix")
		_ = json.NewEncoder(w).Encode([]JobListItem{})
	}))
	defer server.Close()

	client := NewClient(Config{Address: server.URL}, server.Client())
	if _, err := client.ListJobs(context.Background(), "panel-"); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/v1/jobs" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotPrefix != "panel-" {
		t.Fatalf("prefix = %q", gotPrefix)
	}
}

func TestClientDecodesNomadErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"Errors":["bad job","missing group"]}`))
	}))
	defer server.Close()

	client := NewClient(Config{Address: server.URL}, server.Client())
	_, err := client.ReadJob(context.Background(), "bad")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "bad job") || !strings.Contains(err.Error(), "missing group") {
		t.Fatalf("error = %v", err)
	}
}

func TestClientJobMutationEndpointPaths(t *testing.T) {
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		switch r.URL.Path {
		case "/v1/job/panel-web/plan":
			_ = json.NewEncoder(w).Encode(PlanResponse{})
		case "/v1/job/panel-web":
			if r.Method == http.MethodPost {
				_ = json.NewEncoder(w).Encode(RegisterResponse{})
				return
			}
			_ = json.NewEncoder(w).Encode(StopResponse{})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewClient(Config{Address: server.URL, Namespace: "apps"}, server.Client())
	if _, err := client.PlanJob(context.Background(), "panel-web", Job{ID: "panel-web"}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.RegisterJob(context.Background(), "panel-web", Job{ID: "panel-web"}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.StopJob(context.Background(), "panel-web", true); err != nil {
		t.Fatal(err)
	}

	want := []string{
		"POST /v1/job/panel-web/plan?namespace=apps",
		"POST /v1/job/panel-web?namespace=apps",
		"DELETE /v1/job/panel-web?namespace=apps&purge=true",
	}
	if len(requests) != len(want) {
		t.Fatalf("requests = %#v", requests)
	}
	for i := range want {
		if requests[i] != want[i] {
			t.Fatalf("request %d = %q, want %q", i, requests[i], want[i])
		}
	}
}

func TestClientInventoryEndpointPaths(t *testing.T) {
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		switch r.URL.Path {
		case "/v1/status/leader":
			_ = json.NewEncoder(w).Encode("127.0.0.1:4647")
		case "/v1/nodes":
			_ = json.NewEncoder(w).Encode([]NodeListItem{})
		case "/v1/deployments":
			_ = json.NewEncoder(w).Encode([]Deployment{})
		case "/v1/evaluations":
			_ = json.NewEncoder(w).Encode([]Evaluation{})
		case "/v1/services":
			_ = json.NewEncoder(w).Encode([]ServiceRegistration{})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewClient(Config{Address: server.URL}, server.Client())
	if _, err := client.Status(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Nodes(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Deployments(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Evaluations(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Services(context.Background()); err != nil {
		t.Fatal(err)
	}

	want := []string{
		"GET /v1/status/leader",
		"GET /v1/nodes",
		"GET /v1/deployments",
		"GET /v1/evaluations",
		"GET /v1/services",
	}
	if !equalStringSlices(requests, want) {
		t.Fatalf("requests = %#v, want %#v", requests, want)
	}
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
