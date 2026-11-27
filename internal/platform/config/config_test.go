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

func TestDefaultConfigSecurityDatabaseDerived(t *testing.T) {
	cfg := Default()
	want := filepath.Join(filepath.Dir(cfg.AppDatabase), "security.db")
	if got := cfg.SecurityDatabasePath(); got != want {
		t.Fatalf("default security database = %q, want %q", got, want)
	}
	if cfg.SecurityDatabase != "" {
		t.Fatalf("default security database field should stay empty, got %q", cfg.SecurityDatabase)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("default config should accept empty security database: %v", err)
	}
}

func TestLoadSecurityDatabaseFromConfigAndEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	raw := `{
		"listenAddress": "127.0.0.1:8443",
		"adminUsername": "admin",
		"jwtSecret": "secret-jwt-value",
		"dataRoot": "data",
		"appDatabase": "db/app.db",
		"logDatabase": "db/log.db",
		"coordinationDatabase": "db/coordination.db",
		"metricsDatabase": "db/metrics.db",
		"securityDatabase": "db/custom-security.db"
	}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "db", "custom-security.db"); cfg.SecurityDatabase != want {
		t.Fatalf("security database = %q, want %q", cfg.SecurityDatabase, want)
	}

	envPath := filepath.Join(dir, "env", "security.db")
	t.Setenv("PANEL_SECURITY_DATABASE", envPath)
	cfg, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SecurityDatabase != envPath {
		t.Fatalf("env security database = %q, want %q", cfg.SecurityDatabase, envPath)
	}
}

func TestSecurityDatabasePathFallsBackToAppSibling(t *testing.T) {
	cfg := Default()
	cfg.AppDatabase = filepath.Join("x", "db", "app.db")
	cfg.SecurityDatabase = ""
	if got, want := cfg.SecurityDatabasePath(), filepath.Join("x", "db", "security.db"); got != want {
		t.Fatalf("security database = %q, want %q", got, want)
	}
}

func TestConfigValidationRejectsSecurityDatabaseConflict(t *testing.T) {
	cfg := Default()
	cfg.SecurityDatabase = cfg.AppDatabase
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "security database") {
		t.Fatalf("expected security database conflict error, got %v", err)
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
		"listenAddress": "127.0.0.1:8443",
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
		"listenAddress": "127.0.0.1:8443",
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
				"listenAddress": "127.0.0.1:8443",
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
				"listenAddress": "127.0.0.1:8443",
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
