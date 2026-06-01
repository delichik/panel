package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type Config struct {
	ListenAddress               string      `json:"listenAddress"`
	AdminUsername               string      `json:"adminUsername"`
	AdminPasswordHash           string      `json:"adminPasswordHash"`
	JWTSecret                   string      `json:"jwtSecret"`
	DataRoot                    string      `json:"dataRoot"`
	AppDatabase                 string      `json:"appDatabase"`
	MetricsDatabase             string      `json:"metricsDatabase"`
	RemoteCommandTimeoutSeconds int         `json:"remoteCommandTimeoutSeconds"`
	Nomad                       NomadConfig `json:"nomad"`
	Certificates                CertConfig  `json:"certificates"`
}

type NomadConfig struct {
	Address    string `json:"address"`
	Token      string `json:"token"`
	Namespace  string `json:"namespace"`
	Region     string `json:"region"`
	Datacenter string `json:"datacenter"`
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
		MetricsDatabase:             filepath.Join("data", "db", "metrics.db"),
		RemoteCommandTimeoutSeconds: 30,
		Nomad: NomadConfig{
			Address:    "http://127.0.0.1:4646",
			Namespace:  "default",
			Region:     "global",
			Datacenter: "dc1",
		},
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
		if err := decoder.Decode(&cfg); err != nil {
			return Config{}, err
		}
	}
	applyNomadDefaults(&cfg)
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

func applyNomadDefaults(cfg *Config) {
	defaults := Default().Nomad
	if cfg.Nomad.Address == "" {
		cfg.Nomad.Address = defaults.Address
	}
	if cfg.Nomad.Namespace == "" {
		cfg.Nomad.Namespace = defaults.Namespace
	}
	if cfg.Nomad.Region == "" {
		cfg.Nomad.Region = defaults.Region
	}
	if cfg.Nomad.Datacenter == "" {
		cfg.Nomad.Datacenter = defaults.Datacenter
	}
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
	setInt := func(key string, target *int) {
		if v := os.Getenv(key); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				*target = n
			}
		}
	}
	setString("PANEL_LISTEN_ADDRESS", &cfg.ListenAddress)
	setString("PANEL_ADMIN_USERNAME", &cfg.AdminUsername)
	setString("PANEL_ADMIN_PASSWORD_HASH", &cfg.AdminPasswordHash)
	setString("PANEL_JWT_SECRET", &cfg.JWTSecret)
	setString("PANEL_DATA_ROOT", &cfg.DataRoot)
	setString("PANEL_APP_DATABASE", &cfg.AppDatabase)
	setString("PANEL_METRICS_DATABASE", &cfg.MetricsDatabase)
	setInt("PANEL_REMOTE_COMMAND_TIMEOUT_SECONDS", &cfg.RemoteCommandTimeoutSeconds)
	setString("PANEL_CERT_ACME_DIRECTORY_URL", &cfg.Certificates.ACMEDirectoryURL)
	setString("PANEL_CERT_EMAIL", &cfg.Certificates.Email)
	setInt("PANEL_CERT_DNS_PROPAGATION_DELAY_SECONDS", &cfg.Certificates.DNSPropagationDelaySeconds)
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
	if c.DataRoot == "" || c.AppDatabase == "" || c.MetricsDatabase == "" {
		return errors.New("data root and database paths are required")
	}
	if c.AppDatabase == c.MetricsDatabase {
		return errors.New("app database and metrics database must be different")
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
