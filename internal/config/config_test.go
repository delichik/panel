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

func TestConfigValidationRejectsWeakJWTSecret(t *testing.T) {
	cfg := Default()
	cfg.JWTSecret = "short"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected weak jwt secret validation error")
	}
}

func TestLoadNomadConfig(t *testing.T) {
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
			"address": "https://nomad.service:4646",
			"token": "root-token",
			"namespace": "apps",
			"region": "global",
			"datacenter": "dc1"
		}
	}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Nomad.Address != "https://nomad.service:4646" || cfg.Nomad.Token != "root-token" || cfg.Nomad.Namespace != "apps" || cfg.Nomad.Region != "global" || cfg.Nomad.Datacenter != "dc1" {
		t.Fatalf("nomad config = %#v", cfg.Nomad)
	}
}

func TestLoadNomadConfigDefaults(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Nomad.Address != "http://127.0.0.1:4646" || cfg.Nomad.Token != "" || cfg.Nomad.Namespace != "default" || cfg.Nomad.Region != "global" || cfg.Nomad.Datacenter != "dc1" {
		t.Fatalf("nomad defaults = %#v", cfg.Nomad)
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
