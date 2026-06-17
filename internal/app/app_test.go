package app

import (
	"path/filepath"
	"testing"

	"panel/internal/config"
)

func TestServerActionPath(t *testing.T) {
	if !serverActionPath("/api/v1/servers/srv_1/restart", "restart") {
		t.Fatal("expected server restart path to match")
	}
	for _, path := range []string{
		"/api/v1/applications/app_1/restart",
		"/api/v1/servers/restart",
		"/api/v1/servers/srv_1/restart/extra",
	} {
		if serverActionPath(path, "restart") {
			t.Fatalf("unexpected server restart path match: %s", path)
		}
	}
}

func TestApplicationSaveSessionDirUsesDataRootTmp(t *testing.T) {
	cfg := config.Default()
	cfg.DataRoot = filepath.Join("var", "lib", "panel")

	got := applicationSaveSessionDir(cfg)
	want := filepath.Join(cfg.DataRoot, "tmp", "application-save-sessions")
	if got != want {
		t.Fatalf("save session dir = %q, want %q", got, want)
	}
}
