package dns

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"panel/internal/platform/config"
	storage "panel/internal/platform/database"
	"panel/internal/platform/secrets"
)

func TestMigrateProviderCredentialsEncryptsLegacyTokenAndDropsProviderColumns(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.LogDatabase = filepath.Join(dir, "log.db")

	db, err := sql.Open("sqlite", cfg.AppDatabase)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE dns_domains (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL UNIQUE,
		provider TEXT NOT NULL,
		api_token_secret TEXT NOT NULL DEFAULT '',
		account_id TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO dns_domains(id,name,provider,api_token_secret,account_id,created_at,updated_at)
		VALUES('dnsdom_legacy','example.com','cloudflare','legacy-token','legacy-account','now','now')`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.AppDB().Exec(`INSERT INTO certificates(id,name,domain_id,domain,prefix,scope,domains_json,variable_name,certificate_path,private_key_path,created_at,updated_at)
		VALUES('cert_legacy','Legacy','dnsdom_legacy','example.com','@','single','[]','legacy_cert','','','now','now')`); err != nil {
		t.Fatal(err)
	}
	secrets, err := secretstore.Open(cfg, store.AppDB())
	if err != nil {
		t.Fatal(err)
	}
	if err := MigrateProviderCredentials(context.Background(), store.AppDB(), secrets); err != nil {
		t.Fatal(err)
	}

	columns := tableColumns(t, store.AppDB(), "dns_domains")
	for _, removed := range []string{"api_token_secret", "account_id"} {
		if columns[removed] {
			t.Fatalf("legacy column %q still exists: %#v", removed, columns)
		}
	}
	for _, required := range []string{"provider_config_json", "provider_secret_ciphertext"} {
		if !columns[required] {
			t.Fatalf("new column %q missing: %#v", required, columns)
		}
	}

	var ciphertext string
	if err := store.AppDB().QueryRow(`SELECT provider_secret_ciphertext FROM dns_domains WHERE id='dnsdom_legacy'`).Scan(&ciphertext); err != nil {
		t.Fatal(err)
	}
	if ciphertext == "" || ciphertext == "legacy-token" {
		t.Fatalf("credential was not encrypted: %q", ciphertext)
	}
	resolved, err := NewService(store.AppDB(), secrets).ResolveDomain(context.Background(), "dnsdom_legacy")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.APIToken != "legacy-token" {
		t.Fatalf("resolved token = %q", resolved.APIToken)
	}
	var certificateDomainID string
	if err := store.AppDB().QueryRow(`SELECT domain_id FROM certificates WHERE id='cert_legacy'`).Scan(&certificateDomainID); err != nil {
		t.Fatal(err)
	}
	if certificateDomainID != "dnsdom_legacy" {
		t.Fatalf("certificate domain id = %q", certificateDomainID)
	}
}

func tableColumns(t *testing.T, db *sql.DB, table string) map[string]bool {
	t.Helper()
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	columns := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, pk int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			t.Fatal(err)
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return columns
}
