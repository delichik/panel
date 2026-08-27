package database

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"panel/internal/platform/config"
)

func TestMigrateReverseProxyConfigurationUpdatesApplicationOrigins(t *testing.T) {
	db := openLegacyReverseProxyMigrationDB(t)
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO facility_app_configs VALUES('reverse_proxy','["srv-a","srv-b"]','nginx:legacy','{}','[]','[]','','2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO applications VALUES('app-1','website','application','selected','["srv-b"]','[{"domain":"app.example.test","targetType":"local","targetPort":8080,"paths":[{"path":"/"}]}]')`); err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := migrateReverseProxyConfigurationOn(context.Background(), tx); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var raw string
	if err := db.QueryRow(`SELECT reverse_proxy_json FROM applications WHERE id='app-1'`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, `"originServerIds":["srv-b"]`) || !strings.Contains(raw, `"anyAccess":{"enabled":false,"strategy":"round_robin"}`) {
		t.Fatalf("migrated application proxy = %s", raw)
	}
}

func TestMigrateReverseProxyConfigurationRejectsDomainOwnerConflict(t *testing.T) {
	db := openLegacyReverseProxyMigrationDB(t)
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO facility_app_configs VALUES('reverse_proxy','["srv-a"]','nginx:legacy','{}','[{"domain":"shared.example.test","path":"/","ruleType":"redirect","redirectUrl":"https://target.example.test","deploymentServers":["srv-a"]}]','[]','','2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO applications VALUES('app-1','website','application','selected','["srv-a"]','[{"domain":"shared.example.test","targetType":"local","targetPort":8080,"paths":[{"path":"/"}]}]')`); err != nil {
		t.Fatal(err)
	}
	tx, txErr := db.Begin()
	if txErr != nil {
		t.Fatal(txErr)
	}
	defer tx.Rollback()
	err := migrateReverseProxyConfigurationOn(context.Background(), tx)
	if err == nil || !strings.Contains(err.Error(), "owned by both") {
		t.Fatalf("conflict error = %v", err)
	}
}

func openLegacyReverseProxyMigrationDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(filepath.Join(t.TempDir(), "migration.db")))
	if err != nil {
		t.Fatal(err)
	}
	statements := []string{
		`CREATE TABLE facility_app_configs(id TEXT PRIMARY KEY,deployment_server_ids_json TEXT NOT NULL,image TEXT NOT NULL,panel_entry_json TEXT NOT NULL,static_sites_json TEXT NOT NULL,domain_policies_json TEXT NOT NULL,last_error TEXT NOT NULL,updated_at TEXT NOT NULL)`,
		`CREATE TABLE applications(id TEXT PRIMARY KEY,name TEXT NOT NULL,kind TEXT NOT NULL,deployment_mode TEXT NOT NULL,deployment_server_ids_json TEXT NOT NULL,reverse_proxy_json TEXT NOT NULL)`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func TestOpenCreatesDirectoryForFileDSN(t *testing.T) {
	dir := t.TempDir()
	dbDir := filepath.Join(dir, "nested", "db")
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = "file:" + filepath.ToSlash(filepath.Join(dbDir, "app.db")) + "?cache=shared"
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.CoordinationDatabase = filepath.Join(dir, "coordination.db")
	cfg.LogDatabase = filepath.Join(dir, "log.db")
	cfg.CoordinationDatabase = filepath.Join(dir, "coordination.db")
	store, err := Open(cfg)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	if _, err := os.Stat(dbDir); err != nil {
		t.Fatalf("database directory was not created from file: DSN: %v", err)
	}
}
func TestOpenCreatesSeparateSchemas(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.CoordinationDatabase = filepath.Join(dir, "coordination.db")
	cfg.LogDatabase = filepath.Join(dir, "log.db")
	cfg.CoordinationDatabase = filepath.Join(dir, "coordination.db")
	store, err := Open(cfg)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	if _, err := store.LogDB().Exec(`INSERT INTO tasks(id,type,status,created_at) VALUES('task_test','x','queued','now')`); err != nil {
		t.Fatalf("task schema missing tasks table: %v", err)
	}
	if _, err := store.AppDB().Exec(`INSERT INTO tasks(id,type,status,created_at) VALUES('task_test','x','queued','now')`); err == nil {
		t.Fatal("tasks table must not exist in app database")
	}
	if _, err := store.MetricsDB().Exec(`INSERT INTO metrics_snapshots(server_id,time,cpu_usage_percent,memory_used_bytes,memory_total_bytes,disk_used_bytes,disk_total_bytes,network_rx_bps,network_tx_bps) VALUES('srv','now',1,2,3,4,5,6,7)`); err != nil {
		t.Fatalf("metrics schema missing snapshots table: %v", err)
	}
	if _, err := store.AppDB().Exec(`INSERT INTO metrics_snapshots(server_id,time,cpu_usage_percent,memory_used_bytes,memory_total_bytes,disk_used_bytes,disk_total_bytes,network_rx_bps,network_tx_bps) VALUES('srv','now',1,2,3,4,5,6,7)`); err == nil {
		t.Fatal("metrics table must not exist in app database")
	}
}

func TestOpenUsesSmallSQLiteConnectionPool(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.CoordinationDatabase = filepath.Join(dir, "coordination.db")
	cfg.LogDatabase = filepath.Join(dir, "log.db")
	cfg.CoordinationDatabase = filepath.Join(dir, "coordination.db")
	store, err := Open(cfg)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	if got := store.AppDB().Stats().MaxOpenConnections; got != 4 {
		t.Fatalf("app database max open connections = %d, want 4", got)
	}
	if got := store.LogDB().Stats().MaxOpenConnections; got != 4 {
		t.Fatalf("log database max open connections = %d, want 4", got)
	}
	if got := store.MetricsDB().Stats().MaxOpenConnections; got != 4 {
		t.Fatalf("metrics database max open connections = %d, want 4", got)
	}
}

func TestMigrateAddsLoadColumnsToLegacyMetricsSchema(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.CoordinationDatabase = filepath.Join(dir, "coordination.db")
	cfg.LogDatabase = filepath.Join(dir, "log.db")
	cfg.CoordinationDatabase = filepath.Join(dir, "coordination.db")

	db, err := sql.Open("sqlite", sqliteDSN(cfg.MetricsDatabase))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE metrics_snapshots (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		server_id TEXT NOT NULL,
		time TEXT NOT NULL,
		cpu_usage_percent REAL NOT NULL,
		memory_used_bytes INTEGER NOT NULL,
		memory_total_bytes INTEGER NOT NULL,
		disk_used_bytes INTEGER NOT NULL,
		disk_total_bytes INTEGER NOT NULL,
		network_rx_bps REAL NOT NULL,
		network_tx_bps REAL NOT NULL,
		load_average TEXT NOT NULL DEFAULT ''
	)`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	columns := tableColumns(t, store.MetricsDB(), "metrics_snapshots")
	for _, required := range []string{"load_1", "load_5", "load_15"} {
		if !columns[required] {
			t.Fatalf("migrated metrics schema is missing %q", required)
		}
	}
}

func TestMigrateAddsConstrainedFacilityContentModeToLegacyRows(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.CoordinationDatabase = filepath.Join(dir, "coordination.db")
	cfg.LogDatabase = filepath.Join(dir, "log.db")
	cfg.CoordinationDatabase = filepath.Join(dir, "coordination.db")
	db, err := sql.Open("sqlite", sqliteDSN(cfg.AppDatabase))
	if err != nil {
		t.Fatal(err)
	}
	statements := []string{
		`CREATE TABLE facility_static_assets (id TEXT PRIMARY KEY,name TEXT NOT NULL,kind TEXT NOT NULL,filename TEXT NOT NULL DEFAULT '',size INTEGER NOT NULL DEFAULT 0,sha256 TEXT NOT NULL DEFAULT '',created_at TEXT NOT NULL,updated_at TEXT NOT NULL)`,
		`CREATE TABLE facility_edit_session_assets (session_id TEXT NOT NULL,asset_key TEXT NOT NULL,source_asset_id TEXT NOT NULL DEFAULT '',name TEXT NOT NULL,kind TEXT NOT NULL,filename TEXT NOT NULL DEFAULT '',size INTEGER NOT NULL DEFAULT 0,sha256 TEXT NOT NULL DEFAULT '',content_sha256 TEXT NOT NULL DEFAULT '',blob_dir TEXT NOT NULL DEFAULT '',state TEXT NOT NULL DEFAULT 'ready',created_at TEXT NOT NULL,updated_at TEXT NOT NULL,PRIMARY KEY(session_id,asset_key))`,
		`INSERT INTO facility_static_assets(id,name,kind,filename,created_at,updated_at) VALUES('a','a','uploaded_file','a.bin','now','now')`,
		`INSERT INTO facility_edit_session_assets(session_id,asset_key,name,kind,filename,created_at,updated_at) VALUES('s','a','a','uploaded_file','a.bin','now','now')`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, table := range []string{"facility_static_assets", "facility_edit_session_assets"} {
		var mode string
		if err := store.AppDB().QueryRow(`SELECT content_mode FROM ` + table + ` LIMIT 1`).Scan(&mode); err != nil {
			t.Fatal(err)
		}
		if mode != "binary" {
			t.Fatalf("%s migrated mode=%q", table, mode)
		}
		if _, err := store.AppDB().Exec(`UPDATE ` + table + ` SET content_mode='invalid'`); err == nil {
			t.Fatalf("%s accepted invalid mode", table)
		}
	}
	if _, err := store.AppDB().Exec(`INSERT INTO facility_static_assets(id,name,kind,filename,created_at,updated_at) VALUES('b','b','uploaded_file','b.bin','now','now')`); err != nil {
		t.Fatal(err)
	}
	var mode string
	if err := store.AppDB().QueryRow(`SELECT content_mode FROM facility_static_assets WHERE id='b'`).Scan(&mode); err != nil || mode != "binary" {
		t.Fatalf("default mode=%q err=%v", mode, err)
	}
	if _, err := store.AppDB().Exec(`INSERT INTO facility_edit_session_assets(session_id,asset_key,name,kind,filename,created_at,updated_at) VALUES('s','b','b','uploaded_file','b.bin','now','now')`); err != nil {
		t.Fatal(err)
	}
	if err := store.AppDB().QueryRow(`SELECT content_mode FROM facility_edit_session_assets WHERE asset_key='b'`).Scan(&mode); err != nil || mode != "binary" {
		t.Fatalf("session asset default mode=%q err=%v", mode, err)
	}
}

func TestSQLiteDSNAddsDefaultPragmasToFileURI(t *testing.T) {
	dsn := sqliteDSN("file:custom.db?cache=shared")
	if !strings.HasPrefix(dsn, "file:custom.db?") {
		t.Fatalf("unexpected dsn prefix: %q", dsn)
	}
	for _, want := range []string{"busy_timeout%285000%29", "journal_mode%28WAL%29", "foreign_keys%28ON%29", "cache=shared"} {
		if !strings.Contains(dsn, want) {
			t.Fatalf("dsn %q missing %q", dsn, want)
		}
	}
}

func TestSQLiteDSNPreservesExplicitJournalMode(t *testing.T) {
	dsn := sqliteDSN("file:custom.db?_pragma=journal_mode(DELETE)")
	if strings.Contains(dsn, "journal_mode%28WAL%29") {
		t.Fatalf("dsn should not override explicit journal mode: %q", dsn)
	}
	if !strings.Contains(dsn, "journal_mode%28DELETE%29") {
		t.Fatalf("dsn should keep explicit journal mode: %q", dsn)
	}
}

func TestFreshSchemaUsesApplicationTables(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.CoordinationDatabase = filepath.Join(dir, "coordination.db")
	cfg.LogDatabase = filepath.Join(dir, "log.db")
	cfg.CoordinationDatabase = filepath.Join(dir, "coordination.db")
	store, err := Open(cfg)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	for _, table := range []string{"applications", "application_files", "auth_state", "overview_card_configurations", "image_updates", "image_refreshes", "application_reconcile_states", "application_revisions", "jobs"} {
		if !tableExists(t, store.AppDB(), table) {
			t.Fatalf("expected table %q to exist", table)
		}
	}
	for _, table := range []string{"tasks", "task_steps", "task_logs", "application_revisions", "key_asset_exports"} {
		if !tableExists(t, store.LogDB(), table) {
			t.Fatalf("expected log table %q to exist", table)
		}
	}
	for _, table := range []string{"application_lifecycle_operations", "application_lifecycle_targets", "application_target_stages"} {
		if tableExists(t, store.CoordDB(), table) {
			t.Fatalf("legacy coordination table %q must be dropped", table)
		}
	}
	for _, table := range []string{"application_lifecycle_operations", "application_lifecycle_targets", "application_target_stages", "key_asset_exports"} {
		if tableExists(t, store.AppDB(), table) {
			t.Fatalf("fresh app schema must not contain log/coordination table %q", table)
		}
	}
	applicationColumns := tableColumns(t, store.AppDB(), "applications")
	if applicationColumns["persistent_path"] {
		t.Fatal("fresh applications schema must not contain persistent_path")
	}
	dnsColumns := tableColumns(t, store.AppDB(), "dns_domains")
	for _, required := range []string{"provider_config_json", "provider_secret_ciphertext"} {
		if !dnsColumns[required] {
			t.Fatalf("fresh DNS schema is missing %q", required)
		}
	}
	for _, legacy := range []string{"api_token_secret", "account_id"} {
		if dnsColumns[legacy] {
			t.Fatalf("fresh DNS schema must not contain legacy column %q", legacy)
		}
	}
	credentialColumns := tableColumns(t, store.AppDB(), "credentials")
	if !credentialColumns["secret_ciphertext"] {
		t.Fatal("fresh credentials schema is missing secret_ciphertext")
	}
	fail2banColumns := tableColumns(t, store.AppDB(), "fail2ban_configs")
	if !fail2banColumns["managed"] {
		t.Fatal("fresh fail2ban schema is missing managed")
	}
	serverColumns := tableColumns(t, store.AppDB(), "servers")
	for _, required := range []string{"privilege_mode", "privilege_last_checked_at"} {
		if !serverColumns[required] {
			t.Fatalf("fresh server schema is missing %q", required)
		}
	}
	metricColumns := tableColumns(t, store.MetricsDB(), "metrics_snapshots")
	for _, required := range []string{"load_1", "load_5", "load_15"} {
		if !metricColumns[required] {
			t.Fatalf("fresh metrics schema is missing %q", required)
		}
	}
	for _, table := range []string{
		"docker_capabilities",
		"docker_runtime_cache",
		"container_runtime_cache",
		"operation_" + "locks",
		"container_services",
		"container_service_files",
		"container_service_" + string([]byte{'p', 'l', 'a', 'c', 'e', 'm', 'e', 'n', 't', 's'}),
	} {
		if tableExists(t, store.AppDB(), table) {
			t.Fatalf("old orchestration table %q must not exist in fresh schema", table)
		}
	}
}

func TestMigrateAllowsCertificatePrefixesScope(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.CoordinationDatabase = filepath.Join(dir, "coordination.db")
	cfg.LogDatabase = filepath.Join(dir, "log.db")
	cfg.CoordinationDatabase = filepath.Join(dir, "coordination.db")

	db, err := sql.Open("sqlite", sqliteDSN(cfg.AppDatabase))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE certificates (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		domain_id TEXT NOT NULL DEFAULT '',
		domain TEXT NOT NULL,
		prefix TEXT NOT NULL DEFAULT '@',
		scope TEXT NOT NULL CHECK(scope IN ('single','wildcard')),
		domains_json TEXT NOT NULL DEFAULT '[]',
		variable_name TEXT NOT NULL UNIQUE,
		certificate_path TEXT NOT NULL,
		private_key_path TEXT NOT NULL,
		issuer TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL DEFAULT 'pending',
		last_error TEXT NOT NULL DEFAULT '',
		auto_renew INTEGER NOT NULL DEFAULT 1,
		next_renew_at TEXT NOT NULL DEFAULT '',
		not_before TEXT NOT NULL DEFAULT '',
		not_after TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO certificates(id,name,domain,prefix,scope,domains_json,variable_name,certificate_path,private_key_path,created_at,updated_at)
		VALUES('cert_legacy','Legacy','example.com','@','single','["example.com"]','legacy_cert','','','now','now')`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := Open(cfg)
	if err != nil {
		t.Fatalf("open migrated store: %v", err)
	}
	defer store.Close()
	if _, err := store.AppDB().Exec(`INSERT INTO dns_domains(id,name,provider,provider_config_json,provider_secret_ciphertext,created_at,updated_at)
		VALUES('dnsdom_1','example.com','cloudflare','{}','secret','now','now')`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppDB().Exec(`INSERT INTO certificates(id,name,domain,domain_id,prefix,scope,domains_json,variable_name,certificate_path,private_key_path,created_at,updated_at)
		VALUES('cert_prefixes','Prefixes','example.com','dnsdom_1', '@,api','prefixes','["example.com","api.example.com"]','prefix_cert','','','now','now')`); err != nil {
		t.Fatalf("migrated certificates scope constraint rejected prefixes: %v", err)
	}
}

func TestMigrateApplicationFilePathsToOpaqueNames(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, statement := range []string{
		`CREATE TABLE application_files (id TEXT PRIMARY KEY, path TEXT NOT NULL)`,
		`CREATE TABLE application_edit_session_files (session_id TEXT NOT NULL, file_key TEXT NOT NULL, path TEXT NOT NULL)`,
		`INSERT INTO application_files(id,path) VALUES('file-1','config/app.conf')`,
		`INSERT INTO application_edit_session_files(session_id,file_key,path) VALUES('session-1','file-1','templates/site.conf')`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := migrateApplicationFileNamesOn(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	for table, want := range map[string]string{
		"application_files":              "config/app.conf",
		"application_edit_session_files": "templates/site.conf",
	} {
		columns := tableColumns(t, db, table)
		if !columns["name"] || columns["path"] {
			t.Fatalf("%s columns after migration = %#v", table, columns)
		}
		var got string
		if err := db.QueryRow(`SELECT name FROM ` + table).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("%s name = %q, want %q", table, got, want)
		}
	}
}

func TestMigrateFacilityAssetNamesAddsScopedUniqueIndexes(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, statement := range []string{
		`CREATE TABLE facility_static_assets (id TEXT PRIMARY KEY, name TEXT NOT NULL, filename TEXT NOT NULL, created_at TEXT NOT NULL)`,
		`CREATE TABLE facility_edit_session_assets (session_id TEXT NOT NULL, asset_key TEXT NOT NULL, name TEXT NOT NULL, filename TEXT NOT NULL, created_at TEXT NOT NULL, PRIMARY KEY(session_id,asset_key))`,
		`INSERT INTO facility_static_assets VALUES('asset-a','site','a.zip','2026-01-01')`,
		`INSERT INTO facility_static_assets VALUES('asset-b','site','b.zip','2026-01-02')`,
		`INSERT INTO facility_edit_session_assets VALUES('session-a','asset-a','draft','a.zip','2026-01-01')`,
		`INSERT INTO facility_edit_session_assets VALUES('session-a','asset-b','draft','b.zip','2026-01-02')`,
		`INSERT INTO facility_edit_session_assets VALUES('session-b','asset-c','draft','c.zip','2026-01-03')`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := migrateFacilityAssetNamesOn(context.Background(), tx); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	assertNames := func(query string, want []string) {
		t.Helper()
		rows, err := db.Query(query)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		got := []string{}
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				t.Fatal(err)
			}
			got = append(got, name)
		}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("names = %#v, want %#v", got, want)
		}
	}
	assertNames(`SELECT name FROM facility_static_assets ORDER BY created_at,id`, []string{"site", "site-2"})
	assertNames(`SELECT name FROM facility_edit_session_assets ORDER BY session_id,created_at,asset_key`, []string{"draft", "draft-2", "draft"})
	if _, err := db.Exec(`INSERT INTO facility_static_assets VALUES('asset-c','site','c.zip','2026-01-03')`); err == nil {
		t.Fatal("expected facility static asset name uniqueness violation")
	}
	if _, err := db.Exec(`INSERT INTO facility_edit_session_assets VALUES('session-a','asset-c','draft','c.zip','2026-01-03')`); err == nil {
		t.Fatal("expected facility edit session asset name uniqueness violation")
	}
}

func TestMigrateAddsFail2BanManagedColumn(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.CoordinationDatabase = filepath.Join(dir, "coordination.db")
	cfg.LogDatabase = filepath.Join(dir, "log.db")
	cfg.CoordinationDatabase = filepath.Join(dir, "coordination.db")

	db, err := sql.Open("sqlite", sqliteDSN(cfg.AppDatabase))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE fail2ban_configs (
		server_id TEXT PRIMARY KEY,
		config_yaml TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := Open(cfg)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	columns := tableColumns(t, store.AppDB(), "fail2ban_configs")
	if !columns["managed"] {
		t.Fatal("migrated fail2ban schema is missing managed")
	}
}


func TestMigrateNormalizesLegacyNullDefaults(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.CoordinationDatabase = filepath.Join(dir, "coordination.db")
	cfg.LogDatabase = filepath.Join(dir, "log.db")
	cfg.CoordinationDatabase = filepath.Join(dir, "coordination.db")

	db, err := sql.Open("sqlite", sqliteDSN(cfg.AppDatabase))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE credentials (
		id TEXT PRIMARY KEY,
		name TEXT,
		type TEXT,
		username TEXT,
		password_secret TEXT,
		private_key_path TEXT,
		passphrase_secret TEXT,
		created_at TEXT,
		updated_at TEXT
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO credentials(id,name,type,username,password_secret,private_key_path,passphrase_secret,created_at,updated_at)
		VALUES('cred_legacy',NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE servers (
		id TEXT PRIMARY KEY,
		name TEXT,
		host TEXT,
		port INTEGER,
		ssh_username TEXT,
		credential_id TEXT,
		traits TEXT,
		notes TEXT,
		os_id TEXT,
		os_version_id TEXT,
		os_pretty_name TEXT,
		os_supported INTEGER,
		reachable INTEGER,
		sudo_passwordless INTEGER,
		sudo_last_checked_at TEXT,
		last_checked_at TEXT,
		last_error TEXT,
		created_at TEXT,
		updated_at TEXT
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO servers(id,name,host,port,ssh_username,credential_id,traits,notes,os_id,os_version_id,os_pretty_name,os_supported,reachable,sudo_passwordless,sudo_last_checked_at,last_checked_at,last_error,created_at,updated_at)
		VALUES('srv_legacy',NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL)`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := Open(cfg)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	var credName, credType, credUsername, credCreated, credUpdated string
	if err := store.AppDB().QueryRow(`SELECT name,type,username,created_at,updated_at FROM credentials WHERE id='cred_legacy'`).
		Scan(&credName, &credType, &credUsername, &credCreated, &credUpdated); err != nil {
		t.Fatal(err)
	}
	if credName != "" || credType != "password" || credUsername != "" || credCreated == "" || credUpdated == "" {
		t.Fatalf("credential defaults not normalized: name=%q type=%q username=%q created=%q updated=%q", credName, credType, credUsername, credCreated, credUpdated)
	}

	var serverName, serverHost, serverSSHUser, credentialID, traits, notes, osID, osVersionID, osPrettyName, privilegeMode, lastError, serverCreated, serverUpdated string
	var port, osSupported, reachable, sudo int
	if err := store.AppDB().QueryRow(`SELECT name,host,port,ssh_username,credential_id,traits,notes,os_id,os_version_id,os_pretty_name,os_supported,reachable,sudo_passwordless,privilege_mode,last_error,created_at,updated_at FROM servers WHERE id='srv_legacy'`).
		Scan(&serverName, &serverHost, &port, &serverSSHUser, &credentialID, &traits, &notes, &osID, &osVersionID, &osPrettyName, &osSupported, &reachable, &sudo, &privilegeMode, &lastError, &serverCreated, &serverUpdated); err != nil {
		t.Fatal(err)
	}
	if serverName != "" || serverHost != "" || port != 22 || serverSSHUser != "" || credentialID != "cred_legacy" || traits != "{}" || notes != "" || osID != "" || osVersionID != "" || osPrettyName != "" || osSupported != 0 || reachable != 0 || sudo != 0 || privilegeMode != "none" || lastError != "" || serverCreated == "" || serverUpdated == "" {
		t.Fatalf("server defaults not normalized: name=%q host=%q port=%d ssh=%q credential=%q traits=%q notes=%q os=%q/%q/%q supported=%d reachable=%d sudo=%d privilege=%q lastError=%q created=%q updated=%q", serverName, serverHost, port, serverSSHUser, credentialID, traits, notes, osID, osVersionID, osPrettyName, osSupported, reachable, sudo, privilegeMode, lastError, serverCreated, serverUpdated)
	}
}

func TestMigrateRebuildsLegacyFacilityRoutesAsDomains(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.CoordinationDatabase = filepath.Join(dir, "coordination.db")
	cfg.LogDatabase = filepath.Join(dir, "log.db")
	cfg.CoordinationDatabase = filepath.Join(dir, "coordination.db")

	db, err := sql.Open("sqlite", sqliteDSN(cfg.AppDatabase))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE facility_app_configs (
		id TEXT PRIMARY KEY,
		deployment_server_ids_json TEXT NOT NULL DEFAULT '[]',
		image TEXT NOT NULL DEFAULT '',
		panel_entry_json TEXT NOT NULL DEFAULT '{}',
		static_sites_json TEXT NOT NULL DEFAULT '[]',
		last_error TEXT NOT NULL DEFAULT '',
		updated_at TEXT NOT NULL DEFAULT ''
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO facility_app_configs(id,deployment_server_ids_json,image,panel_entry_json,static_sites_json,last_error,updated_at)
		VALUES('reverse_proxy','["srv-edge"]','nginx:legacy','{}','[{"domain":"legacy.example.test","path":"/"}]','','2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := Open(cfg)
	if err != nil {
		t.Fatalf("open migrated store: %v", err)
	}
	defer store.Close()
	columns := tableColumns(t, store.AppDB(), "facility_app_configs")
	if columns["domains_json"] || columns["domain_policies_json"] || columns["static_sites_json"] || columns["image"] {
		t.Fatalf("unexpected migrated facility columns: %#v", columns)
	}
	var routeDomain, routeAppID, routePaths string
	if err := store.AppDB().QueryRow(`SELECT domain, app_id, paths_json FROM reverse_proxy_routes WHERE domain='legacy.example.test'`).Scan(&routeDomain, &routeAppID, &routePaths); err != nil {
		t.Fatal(err)
	}
	if routeAppID != "facility-reverse-proxy" || !strings.Contains(routePaths, `"path":"/"`) {
		t.Fatalf("migrated facility route app_id=%q paths=%q", routeAppID, routePaths)
	}
}

func TestMigratePreservesPasswordlessSudoAsPrivilegeMode(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.CoordinationDatabase = filepath.Join(dir, "coordination.db")
	cfg.LogDatabase = filepath.Join(dir, "log.db")
	cfg.CoordinationDatabase = filepath.Join(dir, "coordination.db")

	store, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppDB().Exec(`INSERT INTO credentials(id,name,type,username,created_at,updated_at) VALUES('cred','c','password','du','now','now')`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppDB().Exec(`INSERT INTO servers(id,name,host,port,credential_id,sudo_passwordless,privilege_mode,created_at,updated_at) VALUES('srv','s','h',22,'cred',1,'','now','now')`); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var mode string
	if err := store.AppDB().QueryRow(`SELECT privilege_mode FROM servers WHERE id='srv'`).Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if mode != "passwordless_sudo" {
		t.Fatalf("privilege mode = %q, want passwordless_sudo", mode)
	}
}

func TestMigrateRecognizesRootSSHUserAsPrivilegeMode(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.CoordinationDatabase = filepath.Join(dir, "coordination.db")
	cfg.LogDatabase = filepath.Join(dir, "log.db")
	cfg.CoordinationDatabase = filepath.Join(dir, "coordination.db")

	store, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppDB().Exec(`INSERT INTO credentials(id,name,type,username,created_at,updated_at) VALUES('cred','c','password','root','now','now')`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppDB().Exec(`INSERT INTO servers(id,name,host,port,ssh_username,credential_id,sudo_passwordless,privilege_mode,created_at,updated_at) VALUES('srv','s','h',22,'','cred',0,'','now','now')`); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var mode string
	if err := store.AppDB().QueryRow(`SELECT privilege_mode FROM servers WHERE id='srv'`).Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if mode != "root" {
		t.Fatalf("privilege mode = %q, want root", mode)
	}
}

func TestMigrateDropsLegacyTaskHistory(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.CoordinationDatabase = filepath.Join(dir, "coordination.db")
	cfg.LogDatabase = filepath.Join(dir, "log.db")
	cfg.CoordinationDatabase = filepath.Join(dir, "coordination.db")

	db, err := sql.Open("sqlite", sqliteDSN(cfg.LogDatabase))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE tasks (
		id TEXT PRIMARY KEY,
		operation_id TEXT NOT NULL DEFAULT '',
		type TEXT NOT NULL,
		server_id TEXT NOT NULL DEFAULT '',
		node_id TEXT NOT NULL DEFAULT '',
		resource_type TEXT NOT NULL DEFAULT '',
		resource_id TEXT NOT NULL DEFAULT '',
		trigger_type TEXT NOT NULL DEFAULT '',
		trigger_resource_type TEXT NOT NULL DEFAULT '',
		trigger_resource_id TEXT NOT NULL DEFAULT '',
		trigger_task_id TEXT NOT NULL DEFAULT '',
		metadata_json TEXT NOT NULL DEFAULT '{}',
		status TEXT NOT NULL,
		stage TEXT NOT NULL DEFAULT '',
		percentage REAL,
		summary TEXT NOT NULL DEFAULT '',
		error TEXT NOT NULL DEFAULT '',
		retry_count INTEGER NOT NULL DEFAULT 0,
		max_retries INTEGER NOT NULL DEFAULT 0,
		next_run_at TEXT,
		created_at TEXT NOT NULL,
		started_at TEXT,
		finished_at TEXT
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE task_steps (id TEXT PRIMARY KEY, task_id TEXT NOT NULL, step TEXT NOT NULL, status TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE task_logs (id INTEGER PRIMARY KEY AUTOINCREMENT, task_id TEXT NOT NULL, time TEXT NOT NULL, stream TEXT NOT NULL, line TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO tasks(id,type,status,created_at) VALUES('task_legacy','server_connectivity_test','queued','now')`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := Open(cfg)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	taskColumns := tableColumns(t, store.LogDB(), "tasks")
	for _, required := range []string{"params_json", "parent_task_id", "concurrency_key", "schedule_key"} {
		if !taskColumns[required] {
			t.Fatalf("migrated task schema is missing %q", required)
		}
	}
	var count int
	if err := store.LogDB().QueryRow(`SELECT COUNT(*) FROM tasks`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("legacy task history should be dropped, got %d row(s)", count)
	}
}

func tableExists(t *testing.T, db *sql.DB, table string) bool {
	t.Helper()
	var name string
	err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&name)
	if err == sql.ErrNoRows {
		return false
	}
	if err != nil {
		t.Fatalf("query table %q: %v", table, err)
	}
	return true
}

func indexExists(t *testing.T, db *sql.DB, index string) bool {
	t.Helper()
	var name string
	err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'index' AND name = ?`, index).Scan(&name)
	if err == sql.ErrNoRows {
		return false
	}
	if err != nil {
		t.Fatalf("query index %q: %v", index, err)
	}
	return true
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

func TestMigrateMovesReverseProxyRoutesToUnifiedTable(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.CoordinationDatabase = filepath.Join(dir, "coordination.db")
	cfg.LogDatabase = filepath.Join(dir, "log.db")
	cfg.CoordinationDatabase = filepath.Join(dir, "coordination.db")

	db, err := sql.Open("sqlite", sqliteDSN(cfg.AppDatabase))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE applications (
		id TEXT PRIMARY KEY,
		version INTEGER NOT NULL DEFAULT 1,
		kind TEXT NOT NULL DEFAULT 'application',
		name TEXT NOT NULL,
		enabled INTEGER NOT NULL DEFAULT 0,
		deletion_requested INTEGER NOT NULL DEFAULT 0,
		spec_yaml TEXT NOT NULL,
		deployment_mode TEXT NOT NULL DEFAULT 'all',
		deployment_server_ids_json TEXT NOT NULL DEFAULT '[]',
		reverse_proxy_json TEXT NOT NULL DEFAULT '[]',
		generation INTEGER NOT NULL DEFAULT 1,
		spec_hash TEXT NOT NULL DEFAULT '',
		job_id TEXT NOT NULL,
		namespace TEXT NOT NULL DEFAULT 'default',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE facility_app_configs (
		id TEXT PRIMARY KEY,
		version INTEGER NOT NULL DEFAULT 1,
		deployment_server_ids_json TEXT NOT NULL DEFAULT '[]',
		panel_entry_json TEXT NOT NULL DEFAULT '{}',
		domains_json TEXT NOT NULL DEFAULT '[]',
		last_error TEXT NOT NULL DEFAULT '',
		updated_at TEXT NOT NULL
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO applications(id,name,enabled,spec_yaml,deployment_mode,deployment_server_ids_json,reverse_proxy_json,generation,spec_hash,job_id,namespace,created_at,updated_at) VALUES('app-1','web',1,'name: web\nimage: nginx\n','all','["srv-a"]','[{"domain":"app.example.test","targetType":"local","targetPort":8080,"originServerIds":["srv-a"],"anyAccess":{"enabled":false,"strategy":"round_robin"},"paths":[{"path":"/","webSocket":"off"}]}]',1,'hash','job','default','now','now')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO facility_app_configs(id,version,deployment_server_ids_json,panel_entry_json,domains_json,last_error,updated_at) VALUES('reverse_proxy',1,'["srv-a"]','{}','[{"domain":"site.example.test","originServerIds":["srv-a"],"anyAccess":{"enabled":false,"strategy":"round_robin"},"paths":[{"path":"/","ruleType":"redirect","redirectUrl":"https://target.example.test"}]}]','','now')`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := Open(cfg)
	if err != nil {
		t.Fatalf("open migrated store: %v", err)
	}
	defer store.Close()

	var appRouteCount int
	if err := store.AppDB().QueryRow(`SELECT COUNT(*) FROM reverse_proxy_routes WHERE app_id='app-1'`).Scan(&appRouteCount); err != nil {
		t.Fatal(err)
	}
	if appRouteCount != 1 {
		t.Fatalf("application route count = %d, want 1", appRouteCount)
	}
	var facilityRouteCount int
	if err := store.AppDB().QueryRow(`SELECT COUNT(*) FROM reverse_proxy_routes WHERE app_id='facility-reverse-proxy'`).Scan(&facilityRouteCount); err != nil {
		t.Fatal(err)
	}
	if facilityRouteCount != 1 {
		t.Fatalf("facility route count = %d, want 1", facilityRouteCount)
	}
	var routeDomain, routePaths string
	if err := store.AppDB().QueryRow(`SELECT domain, paths_json FROM reverse_proxy_routes WHERE domain='app.example.test'`).Scan(&routeDomain, &routePaths); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(routePaths, `"path":"/"`) || !strings.Contains(routePaths, `"webSocket":"off"`) {
		t.Fatalf("application route paths = %q", routePaths)
	}
	appColumns := tableColumns(t, store.AppDB(), "applications")
	if appColumns["reverse_proxy_json"] {
		t.Fatal("applications.reverse_proxy_json must be dropped after migration")
	}
	facilityColumns := tableColumns(t, store.AppDB(), "facility_app_configs")
	if facilityColumns["domains_json"] {
		t.Fatal("facility_app_configs.domains_json must be dropped after migration")
	}
}