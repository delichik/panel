package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type Config struct {
	ListenAddress               string      `json:"listenAddress"`
	AdminUsername               string      `json:"adminUsername"`
	AdminPasswordHash           string      `json:"adminPasswordHash"`
	SessionSecret               string      `json:"sessionSecret"`
	DataRoot                    string      `json:"dataRoot"`
	AppDatabase                 string      `json:"appDatabase"`
	MetricsDatabase             string      `json:"metricsDatabase"`
	RemoteCommandTimeoutSeconds int         `json:"remoteCommandTimeoutSeconds"`
	Nomad                       NomadConfig `json:"nomad"`
}

type NomadConfig struct {
	Address    string `json:"address"`
	Token      string `json:"token"`
	Namespace  string `json:"namespace"`
	Region     string `json:"region"`
	Datacenter string `json:"datacenter"`
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
		SessionSecret:               "change-me-panel-session-secret",
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
	}
}

func Load(path string) (Config, error) {
	cfg := Default()
	if path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return Config{}, err
		}
		decoder := json.NewDecoder(bytes.NewReader(b))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&cfg); err != nil {
			return Config{}, err
		}
	}
	applyNomadDefaults(&cfg)
	applyEnv(&cfg)
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
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
	setString("PANEL_SESSION_SECRET", &cfg.SessionSecret)
	setString("PANEL_DATA_ROOT", &cfg.DataRoot)
	setString("PANEL_APP_DATABASE", &cfg.AppDatabase)
	setString("PANEL_METRICS_DATABASE", &cfg.MetricsDatabase)
	setInt("PANEL_REMOTE_COMMAND_TIMEOUT_SECONDS", &cfg.RemoteCommandTimeoutSeconds)
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
	if len(c.SessionSecret) < 16 {
		return errors.New("session secret must be at least 16 characters")
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
	return nil
}

func (c Config) RemoteTimeout() time.Duration {
	return time.Duration(c.RemoteCommandTimeoutSeconds) * time.Second
}
