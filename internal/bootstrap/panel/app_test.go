package panel

import (
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
