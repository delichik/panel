package storage

import (
	"database/sql"
	"path/filepath"
	"testing"

	"panel/internal/config"
)

func TestOpenCreatesSeparateSchemas(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	store, err := Open(cfg)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	if _, err := store.AppDB().Exec(`INSERT INTO tasks(id,type,status,created_at) VALUES('task_test','x','queued','now')`); err != nil {
		t.Fatalf("app schema missing tasks table: %v", err)
	}
	if _, err := store.MetricsDB().Exec(`INSERT INTO metrics_snapshots(server_id,time,cpu_usage_percent,memory_used_bytes,memory_total_bytes,disk_used_bytes,disk_total_bytes,network_rx_bps,network_tx_bps) VALUES('srv','now',1,2,3,4,5,6,7)`); err != nil {
		t.Fatalf("metrics schema missing snapshots table: %v", err)
	}
	if _, err := store.AppDB().Exec(`INSERT INTO metrics_snapshots(server_id,time,cpu_usage_percent,memory_used_bytes,memory_total_bytes,disk_used_bytes,disk_total_bytes,network_rx_bps,network_tx_bps) VALUES('srv','now',1,2,3,4,5,6,7)`); err == nil {
		t.Fatal("metrics table must not exist in app database")
	}
}

func TestOpenAllowsConcurrentAppConnections(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	store, err := Open(cfg)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	if got := store.AppDB().Stats().MaxOpenConnections; got < 2 {
		t.Fatalf("app database should not be single-connection, got %d", got)
	}
}

func TestFreshSchemaUsesApplicationTables(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	store, err := Open(cfg)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	for _, table := range []string{"applications", "application_files", "application_revisions"} {
		if !tableExists(t, store.AppDB(), table) {
			t.Fatalf("expected table %q to exist", table)
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

func TestMigrateNormalizesLegacyNullDefaults(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")

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
