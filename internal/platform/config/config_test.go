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
	if cfg.AppDatabase == cfg.LogDatabase || cfg.LogDatabase == cfg.MetricsDatabase {
		t.Fatal("log database must be separate from app and metrics databases")
	}
	if filepath.Base(cfg.LogDatabase) != "log.db" {
		t.Fatalf("default log database = %q, want log.db", cfg.LogDatabase)
	}
}

func TestConfigValidationRejectsWeakJWTSecret(t *testing.T) {
	cfg := Default()
	cfg.JWTSecret = "short"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected weak jwt secret validation error")
	}
}

func TestLoadRejectsLegacyRuntimeConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	raw := `{
		"listenAddress": "127.0.0.1:8080",
		"adminUsername": "admin",
		"jwtSecret": "secret-jwt-value",
		"dataRoot": "data",
		"appDatabase": "data/db/app.db",
		"metricsDatabase": "data/db/metrics.db",
		"nomad": {
			"address": "https://runtime.service:4646",
			"token": "root-token",
			"namespace": "apps",
			"region": "global",
			"datacenter": "dc1"
		}
	}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("expected unknown field error, got %v", err)
	}
}

func TestLoadCertificateConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	raw := `{
		"listenAddress": "127.0.0.1:8080",
		"adminUsername": "admin",
		"jwtSecret": "secret-jwt-value",
		"dataRoot": "data",
		"appDatabase": "data/db/app.db",
		"metricsDatabase": "data/db/metrics.db",
		"certificates": {
			"email": "admin@example.com",
			"dnsPropagationDelaySeconds": 10
		}
	}`
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Certificates.Email != "admin@example.com" || cfg.Certificates.DNSPropagationDelaySeconds != 10 {
		t.Fatalf("certificate config = %#v", cfg.Certificates)
	}
}

func TestLoadLogDatabaseConfigAndLegacyTaskDatabase(t *testing.T) {
	dir := t.TempDir()
	for name, field := range map[string]string{
		"new":    `"logDatabase": "data/db/custom-log.db"`,
		"legacy": `"taskDatabase": "data/db/legacy-tasks.db"`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(dir, name+".json")
			raw := `{
				"listenAddress": "127.0.0.1:8080",
				"adminUsername": "admin",
				"jwtSecret": "secret-jwt-value",
				"dataRoot": "data",
				"appDatabase": "data/db/app.db",
				` + field + `,
				"metricsDatabase": "data/db/metrics.db"
			}`
			if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
				t.Fatal(err)
			}
			cfg, err := Load(path)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(cfg.LogDatabase, dir) {
				t.Fatalf("log database should be relative to config dir, got %q", cfg.LogDatabase)
			}
		})
	}
}

func TestLoadRejectsRuntimeSettingsInConfigFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	body := `{
		"listenAddress": "127.0.0.1:8080",
		"adminUsername": "admin",
		"adminPasswordHash": "hash",
		"jwtSecret": "long-enough-jwt-secret",
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
