package storage

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"panel/internal/config"
)

func TestOpenCreatesSeparateSchemas(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.TaskDatabase = filepath.Join(dir, "tasks.db")
	store, err := Open(cfg)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	if _, err := store.TaskDB().Exec(`INSERT INTO tasks(id,type,status,created_at) VALUES('task_test','x','queued','now')`); err != nil {
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
	cfg.TaskDatabase = filepath.Join(dir, "tasks.db")
	store, err := Open(cfg)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	if got := store.AppDB().Stats().MaxOpenConnections; got != 4 {
		t.Fatalf("app database max open connections = %d, want 4", got)
	}
	if got := store.TaskDB().Stats().MaxOpenConnections; got != 4 {
		t.Fatalf("task database max open connections = %d, want 4", got)
	}
	if got := store.MetricsDB().Stats().MaxOpenConnections; got != 4 {
		t.Fatalf("metrics database max open connections = %d, want 4", got)
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
	cfg.TaskDatabase = filepath.Join(dir, "tasks.db")
	store, err := Open(cfg)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	for _, table := range []string{"applications", "application_files", "application_revisions", "auth_state", "overview_card_configurations", "image_updates", "image_refreshes", "application_reconcile_states"} {
		if !tableExists(t, store.AppDB(), table) {
			t.Fatalf("expected table %q to exist", table)
		}
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

func TestMigrateNormalizesLegacyNullDefaults(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.TaskDatabase = filepath.Join(dir, "tasks.db")

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

	var serverName, serverHost, serverSSHUser, credentialID, traits, notes, osID, osVersionID, osPrettyName, lastError, serverCreated, serverUpdated string
	var port, osSupported, reachable, sudo int
	if err := store.AppDB().QueryRow(`SELECT name,host,port,ssh_username,credential_id,traits,notes,os_id,os_version_id,os_pretty_name,os_supported,reachable,sudo_passwordless,last_error,created_at,updated_at FROM servers WHERE id='srv_legacy'`).
		Scan(&serverName, &serverHost, &port, &serverSSHUser, &credentialID, &traits, &notes, &osID, &osVersionID, &osPrettyName, &osSupported, &reachable, &sudo, &lastError, &serverCreated, &serverUpdated); err != nil {
		t.Fatal(err)
	}
	if serverName != "" || serverHost != "" || port != 22 || serverSSHUser != "" || credentialID != "cred_legacy" || traits != "{}" || notes != "" || osID != "" || osVersionID != "" || osPrettyName != "" || osSupported != 0 || reachable != 0 || sudo != 0 || lastError != "" || serverCreated == "" || serverUpdated == "" {
		t.Fatalf("server defaults not normalized: name=%q host=%q port=%d ssh=%q credential=%q traits=%q notes=%q os=%q/%q/%q supported=%d reachable=%d sudo=%d lastError=%q created=%q updated=%q", serverName, serverHost, port, serverSSHUser, credentialID, traits, notes, osID, osVersionID, osPrettyName, osSupported, reachable, sudo, lastError, serverCreated, serverUpdated)
	}
}

func TestMigrateDropsLegacyTaskHistory(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.TaskDatabase = filepath.Join(dir, "tasks.db")

	db, err := sql.Open("sqlite", sqliteDSN(cfg.TaskDatabase))
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

	taskColumns := tableColumns(t, store.TaskDB(), "tasks")
	for _, required := range []string{"params_json", "parent_task_id", "concurrency_key", "schedule_key"} {
		if !taskColumns[required] {
			t.Fatalf("migrated task schema is missing %q", required)
		}
	}
	var count int
	if err := store.TaskDB().QueryRow(`SELECT COUNT(*) FROM tasks`).Scan(&count); err != nil {
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
