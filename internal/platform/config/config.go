package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type Config struct {
	ListenAddress               string     `json:"listenAddress"`
	AdminUsername               string     `json:"adminUsername"`
	AdminPasswordHash           string     `json:"adminPasswordHash"`
	JWTSecret                   string     `json:"jwtSecret"`
	DataRoot                    string     `json:"dataRoot"`
	AppDatabase                 string     `json:"appDatabase"`
	LogDatabase                 string     `json:"logDatabase"`
	CoordinationDatabase        string     `json:"coordinationDatabase"`
	MetricsDatabase             string     `json:"metricsDatabase"`
	RemoteCommandTimeoutSeconds int        `json:"remoteCommandTimeoutSeconds"`
	Certificates                CertConfig `json:"certificates"`
}

type rawConfig struct {
	ListenAddress               string      `json:"listenAddress"`
	AdminUsername               string      `json:"adminUsername"`
	AdminPasswordHash           string      `json:"adminPasswordHash"`
	JWTSecret                   string      `json:"jwtSecret"`
	DataRoot                    string      `json:"dataRoot"`
	AppDatabase                 string      `json:"appDatabase"`
	LogDatabase                 string      `json:"logDatabase"`
	CoordinationDatabase        string      `json:"coordinationDatabase"`
	LegacyTaskDatabase          string      `json:"taskDatabase"`
	MetricsDatabase             string      `json:"metricsDatabase"`
	RemoteCommandTimeoutSeconds *int        `json:"remoteCommandTimeoutSeconds"`
	Certificates                *CertConfig `json:"certificates"`
}

type CertConfig struct {
	ACMEDirectoryURL           string `json:"acmeDirectoryUrl"`
	Email                      string `json:"email"`
	DNSPropagationDelaySeconds int    `json:"dnsPropagationDelaySeconds"`
}

const defaultAdminPassword = "admin"

func defaultAdminHash() string {
	hash, err := bcrypt.GenerateFromPassword([]byte(defaultAdminPassword), bcrypt.DefaultCost)
	if err != nil {
		panic(err)
	}
	return string(hash)
}

func Default() Config {
	return Config{
		ListenAddress:               "127.0.0.1:8080",
		AdminUsername:               "admin",
		AdminPasswordHash:           defaultAdminHash(),
		JWTSecret:                   "change-me-panel-jwt-secret",
		DataRoot:                    "data",
		AppDatabase:                 filepath.Join("data", "db", "app.db"),
		LogDatabase:                 filepath.Join("data", "db", "log.db"),
		CoordinationDatabase:        filepath.Join("data", "db", "coordination.db"),
		MetricsDatabase:             filepath.Join("data", "db", "metrics.db"),
		RemoteCommandTimeoutSeconds: 30,
		Certificates: CertConfig{
			ACMEDirectoryURL:           "https://acme-v02.api.letsencrypt.org/directory",
			DNSPropagationDelaySeconds: 30,
		},
	}
}

func Load(path string) (Config, error) {
	cfg := Default()
	baseDir := ""
	if path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return Config{}, err
		}
		absPath, err := filepath.Abs(path)
		if err != nil {
			return Config{}, err
		}
		baseDir = filepath.Dir(absPath)
		decoder := json.NewDecoder(bytes.NewReader(b))
		decoder.DisallowUnknownFields()
		var raw rawConfig
		if err := decoder.Decode(&raw); err != nil {
			return Config{}, err
		}
		applyRawConfig(&cfg, raw)
	}
	applyCertificateDefaults(&cfg)
	applyEnv(&cfg)
	applyPathBase(&cfg, baseDir)
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func applyPathBase(cfg *Config, baseDir string) {
	if baseDir == "" {
		if cwd, err := os.Getwd(); err == nil {
			baseDir = cwd
		}
	}
	cfg.DataRoot = absolutizePath(baseDir, cfg.DataRoot)
	cfg.AppDatabase = absolutizePath(baseDir, cfg.AppDatabase)
	cfg.LogDatabase = absolutizePath(baseDir, cfg.LogDatabase)
	cfg.CoordinationDatabase = absolutizePath(baseDir, cfg.CoordinationDatabase)
	cfg.MetricsDatabase = absolutizePath(baseDir, cfg.MetricsDatabase)
}

func absolutizePath(baseDir, value string) string {
	if value == "" || filepath.IsAbs(value) || strings.HasPrefix(filepath.ToSlash(value), "file:") {
		return value
	}
	if baseDir == "" {
		return value
	}
	return filepath.Clean(filepath.Join(baseDir, value))
}

func applyCertificateDefaults(cfg *Config) {
	defaults := Default().Certificates
	if cfg.Certificates.ACMEDirectoryURL == "" {
		cfg.Certificates.ACMEDirectoryURL = defaults.ACMEDirectoryURL
	}
	if cfg.Certificates.DNSPropagationDelaySeconds == 0 {
		cfg.Certificates.DNSPropagationDelaySeconds = defaults.DNSPropagationDelaySeconds
	}
}

func applyEnv(cfg *Config) {
	setString := func(key string, target *string) {
		if v := os.Getenv(key); v != "" {
			*target = v
		}
	}
	setString("PANEL_LISTEN_ADDRESS", &cfg.ListenAddress)
	setString("PANEL_DATA_ROOT", &cfg.DataRoot)
	setString("PANEL_APP_DATABASE", &cfg.AppDatabase)
	setString("PANEL_LOG_DATABASE", &cfg.LogDatabase)
	setString("PANEL_TASK_DATABASE", &cfg.LogDatabase)
	setString("PANEL_COORDINATION_DATABASE", &cfg.CoordinationDatabase)
	setString("PANEL_METRICS_DATABASE", &cfg.MetricsDatabase)
	setString("PANEL_CERT_ACME_DIRECTORY_URL", &cfg.Certificates.ACMEDirectoryURL)
}

func applyRawConfig(cfg *Config, raw rawConfig) {
	if raw.ListenAddress != "" {
		cfg.ListenAddress = raw.ListenAddress
	}
	if raw.AdminUsername != "" {
		cfg.AdminUsername = raw.AdminUsername
	}
	if raw.AdminPasswordHash != "" {
		cfg.AdminPasswordHash = raw.AdminPasswordHash
	}
	if raw.JWTSecret != "" {
		cfg.JWTSecret = raw.JWTSecret
	}
	if raw.DataRoot != "" {
		cfg.DataRoot = raw.DataRoot
	}
	if raw.AppDatabase != "" {
		cfg.AppDatabase = raw.AppDatabase
	}
	if raw.LogDatabase != "" {
		cfg.LogDatabase = raw.LogDatabase
	} else if raw.LegacyTaskDatabase != "" {
		cfg.LogDatabase = raw.LegacyTaskDatabase
	}
	if raw.CoordinationDatabase != "" {
		cfg.CoordinationDatabase = raw.CoordinationDatabase
	}
	if raw.MetricsDatabase != "" {
		cfg.MetricsDatabase = raw.MetricsDatabase
	}
	if raw.RemoteCommandTimeoutSeconds != nil {
		cfg.RemoteCommandTimeoutSeconds = *raw.RemoteCommandTimeoutSeconds
	}
	if raw.Certificates != nil {
		cfg.Certificates = *raw.Certificates
	}
}

func (c Config) Validate() error {
	if c.ListenAddress == "" {
		return errors.New("listen address is required")
	}
	if c.AdminUsername == "" {
		return errors.New("admin username is required")
	}
	if c.AdminPasswordHash == "" {
		return errors.New("admin password hash is required")
	}
	if len(c.JWTSecret) < 16 {
		return errors.New("jwt secret must be at least 16 characters")
	}
	if c.DataRoot == "" || c.AppDatabase == "" || c.LogDatabase == "" || c.CoordinationDatabase == "" || c.MetricsDatabase == "" {
		return errors.New("data root and database paths are required")
	}
	if c.AppDatabase == c.MetricsDatabase {
		return errors.New("app database and metrics database must be different")
	}
	if c.AppDatabase == c.LogDatabase {
		return errors.New("app database and log database must be different")
	}
	if c.LogDatabase == c.MetricsDatabase {
		return errors.New("log database and metrics database must be different")
	}
	if c.CoordinationDatabase == c.AppDatabase || c.CoordinationDatabase == c.LogDatabase || c.CoordinationDatabase == c.MetricsDatabase {
		return errors.New("coordination database must be different from app, log and metrics databases")
	}
	if c.RemoteCommandTimeoutSeconds < 1 {
		return errors.New("remote command timeout must be positive")
	}
	if c.Certificates.DNSPropagationDelaySeconds < 0 {
		return errors.New("certificate DNS propagation delay cannot be negative")
	}
	return nil
}

func (c Config) RemoteTimeout() time.Duration {
	return time.Duration(c.RemoteCommandTimeoutSeconds) * time.Second
}
