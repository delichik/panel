package dns

import (
	"context"
	"database/sql"
	"strings"

	"panel/internal/platform/secrets"
)

type legacyDomainCredentials struct {
	id         string
	provider   string
	apiToken   string
	configJSON string
	ciphertext string
}

func MigrateProviderCredentials(ctx context.Context, db *sql.DB, secrets *secretstore.Store) error {
	hasLegacyToken, err := tableHasColumn(ctx, db, "dns_domains", "api_token_secret")
	if err != nil {
		return err
	}
	hasLegacyAccount, err := tableHasColumn(ctx, db, "dns_domains", "account_id")
	if err != nil || (!hasLegacyToken && !hasLegacyAccount) {
		return err
	}
	tokenColumn := `''`
	if hasLegacyToken {
		tokenColumn = "api_token_secret"
	}
	rows, err := db.QueryContext(ctx, `SELECT id,provider,`+tokenColumn+`,provider_config_json,provider_secret_ciphertext FROM dns_domains`)
	if err != nil {
		return err
	}
	var domains []legacyDomainCredentials
	for rows.Next() {
		var domain legacyDomainCredentials
		if err := rows.Scan(&domain.id, &domain.provider, &domain.apiToken, &domain.configJSON, &domain.ciphertext); err != nil {
			_ = rows.Close()
			return err
		}
		if strings.TrimSpace(domain.configJSON) == "" {
			domain.configJSON = "{}"
		}
		if strings.TrimSpace(domain.ciphertext) == "" {
			if strings.TrimSpace(domain.apiToken) != "" {
				domain.ciphertext, err = encryptProviderCredentials(secrets, domain.id, domain.provider, domain.apiToken)
				if err != nil {
					_ = rows.Close()
					return err
				}
			}
			// hasLegacyAccount without a legacy token column and without an
			// existing ciphertext: leave the credential empty instead of
			// failing startup with a confusing "token required" error. The
			// domain will report a clear credential error when actually used.
		}
		domains = append(domains, domain)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}

	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys=OFF`); err != nil {
		return err
	}
	defer conn.ExecContext(context.Background(), `PRAGMA foreign_keys=ON`)
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	rollback := func(err error) error {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.ExecContext(ctx, `CREATE TABLE dns_domains_new (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL UNIQUE,
		provider TEXT NOT NULL CHECK(provider IN ('cloudflare')),
		provider_config_json TEXT NOT NULL DEFAULT '{}',
		provider_secret_ciphertext TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`); err != nil {
		return rollback(err)
	}
	for _, domain := range domains {
		if _, err := tx.ExecContext(ctx, `INSERT INTO dns_domains_new(id,name,provider,provider_config_json,provider_secret_ciphertext,created_at,updated_at)
			SELECT id,name,provider,?,?,created_at,updated_at FROM dns_domains WHERE id=?`,
			domain.configJSON, domain.ciphertext, domain.id); err != nil {
			return rollback(err)
		}
	}
	if _, err := tx.ExecContext(ctx, `DROP TABLE dns_domains`); err != nil {
		return rollback(err)
	}
	if _, err := tx.ExecContext(ctx, `ALTER TABLE dns_domains_new RENAME TO dns_domains`); err != nil {
		return rollback(err)
	}
	return tx.Commit()
}

func tableHasColumn(ctx context.Context, db *sql.DB, table, column string) (bool, error) {
	rows, err := db.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, pk int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}
