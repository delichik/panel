package database

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"panel/internal/platform/config"
)

// TestPurgeRemovedTailscaleStateMigrationStripsSecretAndTraits 模拟从仍带
// Tailscale 状态的旧版本升级：一次性迁移必须删除 runtime_settings 里的认证
// 密钥/标签，剥掉 servers.traits 里的 tailscale.* 与 agent.certificate.hosts，
// 同时保留无关设置与 traits；三个 tailscale_* 意图列由破坏性 schema 同步删除。
func TestPurgeRemovedTailscaleStateMigrationStripsSecretAndTraits(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.LogDatabase = filepath.Join(dir, "log.db")
	cfg.CoordinationDatabase = filepath.Join(dir, "coordination.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")

	store, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	// 模拟升级前的数据库：一次性迁移尚未执行。
	if _, err := store.AppDB().Exec(`DELETE FROM orm_migrations WHERE id='purge_removed_tailscale_state'`); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	now := "2026-11-19T00:00:00Z"
	if _, err := store.AppDB().Exec(`INSERT INTO credentials(id,name,type,username,created_at,updated_at) VALUES('cred','c','password','du',?,?)`, now, now); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	traits := `{"agent.enabled":"true","agent.url":"https://203.0.113.10:9786","tailscale.status":"running","tailscale.ipv4":"100.64.0.7","agent.certificate.hosts":"203.0.113.10,100.64.0.7"}`
	if _, err := store.AppDB().Exec(`INSERT INTO servers(id,name,host,port,credential_id,traits,created_at,updated_at) VALUES('srv','s','203.0.113.10',22,'cred',?,?,?)`, traits, now, now); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	for _, item := range []struct{ key, value string }{
		{"tailscale.authKey", "tskey-auth-secret"},
		{"tailscale.tags", "tag:web"},
		{"log.level", "info"},
	} {
		if _, err := store.AppDB().Exec(`INSERT INTO runtime_settings(key,value,updated_at) VALUES(?,?,?)`, item.key, item.value, now); err != nil {
			_ = store.Close()
			t.Fatal(err)
		}
	}
	// 旧 schema 的三个意图列：破坏性 AutoMigrate 应在本次升级中删除它们。
	legacyColumns := []string{"tailscale_enabled", "tailscale_prefer_agent", "tailscale_prefer_interconnect"}
	for _, name := range legacyColumns {
		if _, err := store.AppDB().Exec(`ALTER TABLE servers ADD COLUMN ` + name + ` INTEGER NOT NULL DEFAULT 0`); err != nil {
			_ = store.Close()
			t.Fatal(err)
		}
	}
	// 旧版本的 orm_meta 快照把这些列记为受管列；破坏性同步只会删除受管列，
	// 因此必须把快照也改成升级前的形态，才能真实模拟旧库。
	var schema string
	if err := store.AppDB().QueryRow(`SELECT schema FROM orm_meta WHERE table_name='servers'`).Scan(&schema); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	var snap map[string]any
	if err := json.Unmarshal([]byte(schema), &snap); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	columns, _ := snap["columns"].([]any)
	for _, name := range legacyColumns {
		columns = append(columns, map[string]any{"name": name, "type": "INTEGER", "not_null": true, "default": "0"})
	}
	snap["columns"] = columns
	encoded, err := json.Marshal(snap)
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	if _, err := store.AppDB().Exec(`UPDATE orm_meta SET schema=? WHERE table_name='servers'`, string(encoded)); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	var count int
	if err := store.AppDB().QueryRow(`SELECT COUNT(1) FROM runtime_settings WHERE key IN ('tailscale.authKey','tailscale.tags')`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("tailscale runtime settings were not purged: %d rows left", count)
	}
	var level string
	if err := store.AppDB().QueryRow(`SELECT value FROM runtime_settings WHERE key='log.level'`).Scan(&level); err != nil {
		t.Fatal(err)
	}
	if level != "info" {
		t.Fatalf("unrelated runtime setting changed: %q", level)
	}

	var raw string
	if err := store.AppDB().QueryRow(`SELECT traits FROM servers WHERE id='srv'`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var decoded map[string]string
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatalf("traits are not a JSON object: %v (%s)", err, raw)
	}
	if decoded["agent.url"] != "https://203.0.113.10:9786" || decoded["agent.enabled"] != "true" {
		t.Fatalf("unrelated traits were changed: %s", raw)
	}
	for key := range decoded {
		if strings.HasPrefix(key, "tailscale.") || key == "agent.certificate.hosts" {
			t.Fatalf("removed feature trait %q still present: %s", key, raw)
		}
	}

	tableColumns, err := databaseTableColumnsOn(context.Background(), store.AppDB(), "servers")
	if err != nil {
		t.Fatal(err)
	}
	for _, column := range []string{"tailscale_enabled", "tailscale_prefer_agent", "tailscale_prefer_interconnect"} {
		if tableColumns[column] {
			t.Fatalf("legacy column %q survived the destructive schema sync", column)
		}
	}
}
