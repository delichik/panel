package panel

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"panel/internal/platform/config"
)

func TestApplicationSaveSessionDirUsesDataRootTmp(t *testing.T) {
	cfg := config.Default()
	cfg.DataRoot = filepath.Join("var", "lib", "panel")

	got := applicationSaveSessionDir(cfg)
	want := filepath.Join(cfg.DataRoot, "tmp", "application-save-sessions")
	if got != want {
		t.Fatalf("save session dir = %q, want %q", got, want)
	}
}

func TestStaticCacheControl(t *testing.T) {
	tests := []struct {
		name string
		rel  string
		want string
	}{
		{name: "entry document", rel: "index.html", want: staticDocumentCacheControl},
		{name: "spa document", rel: "views/settings.html", want: staticDocumentCacheControl},
		{name: "upper case document", rel: "views/settings.HTML", want: staticDocumentCacheControl},
		{name: "fingerprinted asset", rel: "assets/index-AbCd1234.js", want: staticImmutableCacheControl},
		{name: "asset with leading slash", rel: "/assets/index-AbCd1234.js", want: staticImmutableCacheControl},
		{name: "unhashed resource", rel: "favicon.svg", want: staticResourceCacheControl},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := staticCacheControl(tt.rel); got != tt.want {
				t.Fatalf("staticCacheControl(%q) = %q, want %q", tt.rel, got, tt.want)
			}
		})
	}
}

func TestStaticSetsCacheControlForAssetsAndSPAFallback(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.MkdirAll(filepath.Join("web", "dist", "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("web", "dist", "index.html"), []byte("<!doctype html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("web", "dist", "assets", "app.js"), []byte("console.log('ok')"), 0o644); err != nil {
		t.Fatal(err)
	}

	app := &App{}
	assetResponse := httptest.NewRecorder()
	app.static(assetResponse, httptest.NewRequest("GET", "/assets/app.js", nil))
	if got := assetResponse.Header().Get("Cache-Control"); got != staticImmutableCacheControl {
		t.Fatalf("asset Cache-Control = %q, want %q", got, staticImmutableCacheControl)
	}

	fallbackResponse := httptest.NewRecorder()
	app.static(fallbackResponse, httptest.NewRequest("GET", "/settings", nil))
	if got := fallbackResponse.Header().Get("Cache-Control"); got != staticDocumentCacheControl {
		t.Fatalf("SPA fallback Cache-Control = %q, want %q", got, staticDocumentCacheControl)
	}
}
