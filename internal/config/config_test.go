package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultConfigValidAndSplitDatabases(t *testing.T) {
	cfg := Default()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("default config should be valid: %v", err)
	}
	if cfg.AppDatabase == cfg.MetricsDatabase {
		t.Fatal("app and metrics databases must be separate")
	}
}

func TestConfigValidationRejectsWeakSessionSecret(t *testing.T) {
	cfg := Default()
	cfg.SessionSecret = "short"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected weak session secret validation error")
	}
}

func TestLoadRejectsRuntimeSettingsInConfigFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	body := `{
		"listenAddress": "127.0.0.1:8080",
		"adminUsername": "admin",
		"adminPasswordHash": "hash",
		"sessionSecret": "long-enough-session-secret",
		"dataRoot": "data",
		"appDatabase": "data/db/app.db",
		"metricsDatabase": "data/db/metrics.db",
		"remoteCommandTimeoutSeconds": 30,
		"metricsRetentionDays": 7
	}`
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("expected unknown field error, got %v", err)
	}
}
