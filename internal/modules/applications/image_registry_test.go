package applications

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
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

func TestRegistryImageResolverHostOverrideKeepsManifestPath(t *testing.T) {
	var gotHost, gotPath, gotMethod string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHost, gotPath, gotMethod = r.Host, r.URL.Path, r.Method
		w.Header().Set("Docker-Content-Digest", "sha256:override")
	}))
	defer server.Close()
	host := server.Listener.Addr().String()

	cases := []struct {
		image        string
		wantRegistry string
		wantPath     string
	}{
		{"registry-1.docker.io/team/web:1.2", "registry-1.docker.io", "/v2/team/web/manifests/1.2"},
		{"ghcr.io/acme/api:2", "ghcr.io", "/v2/acme/api/manifests/2"},
		{"nginx", "registry-1.docker.io", "/v2/library/nginx/manifests/latest"},
	}
	for _, tc := range cases {
		gotHost, gotPath, gotMethod = "", "", ""
		resolver := &RegistryImageResolver{Client: server.Client(), Scheme: "http", Host: host}
		result, err := resolver.Resolve(context.Background(), tc.image)
		if err != nil {
			t.Fatalf("%s: %v", tc.image, err)
		}
		if gotHost != host {
			t.Fatalf("%s: request host = %q, want %q", tc.image, gotHost, host)
		}
		if gotPath != tc.wantPath {
			t.Fatalf("%s: request path = %q, want %q", tc.image, gotPath, tc.wantPath)
		}
		if gotMethod != http.MethodHead {
			t.Fatalf("%s: request method = %q, want HEAD", tc.image, gotMethod)
		}
		// The override only swaps the transport host: the resolved reference
		// metadata still describes the registry named in the image.
		if result.Registry != tc.wantRegistry || result.Digest != "sha256:override" {
			t.Fatalf("%s: result = %#v", tc.image, result)
		}
	}
}

func TestRegistryImageResolverEmptyHostUsesReferenceRegistry(t *testing.T) {
	var gotHost, gotPath string
	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHost, gotPath = r.Host, r.URL.Path
		w.Header().Set("Docker-Content-Digest", "sha256:direct")
	}))
	defer registry.Close()
	registryHost := registry.Listener.Addr().String()

	resolver := &RegistryImageResolver{Client: registry.Client(), Scheme: "http"}
	result, err := resolver.Resolve(context.Background(), registryHost+"/team/web:latest")
	if err != nil {
		t.Fatal(err)
	}
	if gotHost != registryHost || gotPath != "/v2/team/web/manifests/latest" || result.Digest != "sha256:direct" {
		t.Fatalf("host=%q path=%q result=%#v", gotHost, gotPath, result)
	}
}

func TestRegistryImageResolverHostOverrideUsesGetFallback(t *testing.T) {
	var methods []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Path != "/v2/team/web/manifests/latest" {
			t.Fatalf("GET path = %s", r.URL.Path)
		}
		w.Header().Set("Docker-Content-Digest", "sha256:get")
	}))
	defer server.Close()

	resolver := &RegistryImageResolver{Client: server.Client(), Scheme: "http", Host: server.Listener.Addr().String()}
	result, err := resolver.Resolve(context.Background(), "registry-1.docker.io/team/web:latest")
	if err != nil {
		t.Fatal(err)
	}
	if len(methods) != 2 || methods[0] != http.MethodHead || methods[1] != http.MethodGet {
		t.Fatalf("methods = %#v", methods)
	}
	if result.Digest != "sha256:get" {
		t.Fatalf("result = %#v", result)
	}
}

func TestRegistryImageResolverBearerChallengeUsesOverriddenHost(t *testing.T) {
	var gotPath string
	auth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"token": "token-2"})
	}))
	defer auth.Close()
	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if r.Header.Get("Authorization") == "" {
			w.Header().Set("WWW-Authenticate", `Bearer realm="`+auth.URL+`",service="mirror.test",scope="repository:team/web:pull"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.Header.Get("Authorization") != "Bearer token-2" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Docker-Content-Digest", "sha256:bearer")
	}))
	defer registry.Close()

	resolver := &RegistryImageResolver{Client: registry.Client(), Scheme: "http", Host: registry.Listener.Addr().String()}
	result, err := resolver.Resolve(context.Background(), "registry-1.docker.io/team/web:latest")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/v2/team/web/manifests/latest" || result.Digest != "sha256:bearer" {
		t.Fatalf("path=%q result=%#v", gotPath, result)
	}
}

func TestRegistryImageResolverReadsConfigProviderPerResolve(t *testing.T) {
	serverA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Docker-Content-Digest", "sha256:aaa")
	}))
	defer serverA.Close()
	serverB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/team/web/manifests/latest" {
			t.Fatalf("mirror B path = %s", r.URL.Path)
		}
		w.Header().Set("Docker-Content-Digest", "sha256:bbb")
	}))
	defer serverB.Close()

	config := ImageRegistryConfig{Endpoint: serverA.Listener.Addr().String(), Scheme: "http"}
	resolver := NewRegistryImageResolverWithConfig(func() ImageRegistryConfig { return config })
	client := &http.Client{Timeout: 5 * time.Second}
	resolver.Client = client

	first, err := resolver.Resolve(context.Background(), "registry-1.docker.io/team/web:latest")
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest != "sha256:aaa" {
		t.Fatalf("first digest = %q", first.Digest)
	}

	// Hot-reload: the very next Resolve must use the new mirror without the
	// resolver rebuilding its cached HTTP client.
	config.Endpoint = serverB.Listener.Addr().String()
	second, err := resolver.Resolve(context.Background(), "registry-1.docker.io/team/web:latest")
	if err != nil {
		t.Fatal(err)
	}
	if second.Digest != "sha256:bbb" {
		t.Fatalf("second digest = %q", second.Digest)
	}
	if resolver.Client != client {
		t.Fatal("resolver rebuilt its http client between resolves")
	}
}

func TestRegistryImageResolverConfigEndpointCarriesScheme(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Docker-Content-Digest", "sha256:endpoint")
	}))
	defer server.Close()

	// No explicit Scheme: the "http://" prefix and trailing slash on the
	// endpoint must be understood instead of producing a malformed URL.
	resolver := NewRegistryImageResolverWithConfig(func() ImageRegistryConfig {
		return ImageRegistryConfig{Endpoint: "http://" + server.Listener.Addr().String() + "/"}
	})
	resolver.Client = server.Client()

	result, err := resolver.Resolve(context.Background(), "ghcr.io/acme/api:2")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/v2/acme/api/manifests/2" || result.Digest != "sha256:endpoint" {
		t.Fatalf("path=%q result=%#v", gotPath, result)
	}
}

func TestRegistryImageResolverConfigEmptyKeepsDefaults(t *testing.T) {
	var gotPath string
	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Docker-Content-Digest", "sha256:default")
	}))
	defer registry.Close()
	registryHost := registry.Listener.Addr().String()

	providers := map[string]func() ImageRegistryConfig{
		"nil provider":   nil,
		"empty provider": func() ImageRegistryConfig { return ImageRegistryConfig{} },
	}
	for name, provider := range providers {
		gotPath = ""
		resolver := NewRegistryImageResolverWithConfig(provider)
		resolver.Client = registry.Client()
		resolver.Scheme = "http"
		result, err := resolver.Resolve(context.Background(), registryHost+"/team/web:latest")
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if gotPath != "/v2/team/web/manifests/latest" || result.Digest != "sha256:default" {
			t.Fatalf("%s: path=%q result=%#v", name, gotPath, result)
		}
	}
}
