package applications

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseImageReferenceDefaultsDockerHubLibraryLatest(t *testing.T) {
	ref, err := parseImageReference("nginx")
	if err != nil {
		t.Fatal(err)
	}
	if ref.Registry != "registry-1.docker.io" || ref.Repository != "library/nginx" || ref.Tag != "latest" {
		t.Fatalf("ref = %#v", ref)
	}
}

func TestRegistryImageResolverReadsManifestDigest(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/v2/team/web/manifests/latest" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if r.Header.Get("Accept") == "" {
			t.Fatal("missing accept header")
		}
		w.Header().Set("Docker-Content-Digest", "sha256:abc")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	resolver := &RegistryImageResolver{Client: server.Client(), Scheme: "http"}
	host := server.Listener.Addr().String()
	result, err := resolver.Resolve(context.Background(), host+"/team/web:latest")
	if err != nil {
		t.Fatal(err)
	}
	if result.Digest != "sha256:abc" || gotAuth != "" {
		t.Fatalf("result=%#v auth=%q", result, gotAuth)
	}
}

func TestRegistryImageResolverUsesBearerChallenge(t *testing.T) {
	var registryURL string
	auth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("service") != "registry.test" || r.URL.Query().Get("scope") != "repository:team/web:pull" {
			t.Fatalf("token query = %s", r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"token": "token-1"})
	}))
	defer auth.Close()
	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			w.Header().Set("WWW-Authenticate", `Bearer realm="`+auth.URL+`",service="registry.test",scope="repository:team/web:pull"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.Header.Get("Authorization") != "Bearer token-1" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Docker-Content-Digest", "sha256:def")
	}))
	defer registry.Close()
	registryURL = registry.Listener.Addr().String()

	resolver := &RegistryImageResolver{Client: registry.Client(), Scheme: "http"}
	result, err := resolver.Resolve(context.Background(), registryURL+"/team/web:latest")
	if err != nil {
		t.Fatal(err)
	}
	if result.Digest != "sha256:def" {
		t.Fatalf("result = %#v", result)
	}
}
