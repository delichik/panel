package storage

import "context"

func (s *Store) Migrate(ctx context.Context) error {
	app := []string{
		`PRAGMA foreign_keys = ON`,
		`CREATE TABLE IF NOT EXISTS credentials (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			type TEXT NOT NULL CHECK(type IN ('password','private_key')),
			username TEXT NOT NULL,
			password_secret TEXT NOT NULL DEFAULT '',
			private_key_path TEXT NOT NULL DEFAULT '',
			passphrase_secret TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS servers (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			host TEXT NOT NULL,
			port INTEGER NOT NULL,
			ssh_username TEXT NOT NULL DEFAULT '',
			credential_id TEXT NOT NULL,
			traits TEXT NOT NULL DEFAULT '{}',
			notes TEXT NOT NULL DEFAULT '',
			os_id TEXT NOT NULL DEFAULT '',
			os_version_id TEXT NOT NULL DEFAULT '',
			os_pretty_name TEXT NOT NULL DEFAULT '',
			os_supported INTEGER NOT NULL DEFAULT 0,
			reachable INTEGER NOT NULL DEFAULT 0,
			sudo_passwordless INTEGER NOT NULL DEFAULT 0,
			sudo_last_checked_at TEXT,
			last_checked_at TEXT,
			last_error TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			FOREIGN KEY(credential_id) REFERENCES credentials(id)
		)`,
		`CREATE TABLE IF NOT EXISTS package_updates (
			server_id TEXT NOT NULL,
			name TEXT NOT NULL,
			installed_version TEXT NOT NULL,
			candidate_version TEXT NOT NULL,
			source TEXT NOT NULL DEFAULT '',
			refreshed_at TEXT NOT NULL,
			PRIMARY KEY(server_id, name),
			FOREIGN KEY(server_id) REFERENCES servers(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS package_refreshes (
			server_id TEXT PRIMARY KEY,
			refreshed_at TEXT NOT NULL,
			FOREIGN KEY(server_id) REFERENCES servers(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS docker_capabilities (
			server_id TEXT PRIMARY KEY,
			docker_installed INTEGER NOT NULL DEFAULT 0,
			docker_version TEXT NOT NULL DEFAULT '',
			compose_installed INTEGER NOT NULL DEFAULT 0,
			compose_version TEXT NOT NULL DEFAULT '',
			include_supported INTEGER NOT NULL DEFAULT 0,
			supported INTEGER NOT NULL DEFAULT 0,
			last_checked_at TEXT NOT NULL,
			last_error TEXT NOT NULL DEFAULT '',
			stale INTEGER NOT NULL DEFAULT 0,
			FOREIGN KEY(server_id) REFERENCES servers(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS docker_runtime_cache (
			server_id TEXT NOT NULL,
			resource TEXT NOT NULL,
			payload TEXT NOT NULL,
			refreshed_at TEXT NOT NULL,
			PRIMARY KEY(server_id, resource),
			FOREIGN KEY(server_id) REFERENCES servers(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS container_runtime_cache (
			id TEXT PRIMARY KEY,
			node_id TEXT NOT NULL,
			service_id TEXT NOT NULL DEFAULT '',
			container_id TEXT NOT NULL DEFAULT '',
			name TEXT NOT NULL DEFAULT '',
			image TEXT NOT NULL DEFAULT '',
			state TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT '',
			health TEXT NOT NULL DEFAULT '',
			ports_json TEXT NOT NULL DEFAULT '[]',
			labels_json TEXT NOT NULL DEFAULT '{}',
			managed INTEGER NOT NULL DEFAULT 0,
			observed_at TEXT NOT NULL,
			stale INTEGER NOT NULL DEFAULT 0,
			error TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS tasks (
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
			triggered_by TEXT NOT NULL DEFAULT '',
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
		)`,
		`CREATE TABLE IF NOT EXISTS task_steps (
			id TEXT PRIMARY KEY,
			task_id TEXT NOT NULL,
			step TEXT NOT NULL,
			status TEXT NOT NULL,
			percentage REAL NOT NULL DEFAULT 0,
			metadata_json TEXT NOT NULL DEFAULT '{}',
			started_at TEXT,
			finished_at TEXT,
			error TEXT NOT NULL DEFAULT '',
			UNIQUE(task_id, step),
			FOREIGN KEY(task_id) REFERENCES tasks(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS operation_locks (
			scope TEXT NOT NULL,
			resource_id TEXT NOT NULL,
			owner_task_id TEXT NOT NULL,
			expires_at TEXT NOT NULL,
			heartbeat_at TEXT NOT NULL,
			PRIMARY KEY(scope, resource_id)
		)`,
		`CREATE TABLE IF NOT EXISTS task_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			task_id TEXT NOT NULL,
			time TEXT NOT NULL,
			stream TEXT NOT NULL,
			line TEXT NOT NULL,
			FOREIGN KEY(task_id) REFERENCES tasks(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS runtime_settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS container_services (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			enabled INTEGER NOT NULL DEFAULT 0,
			compose_service_yaml TEXT NOT NULL,
			variables_json TEXT NOT NULL DEFAULT '{}',
			selector_json TEXT NOT NULL DEFAULT '{}',
			generation INTEGER NOT NULL DEFAULT 1,
			spec_revision TEXT NOT NULL DEFAULT '',
			spec_hash TEXT NOT NULL DEFAULT '',
			last_error TEXT NOT NULL DEFAULT '',
			last_task_id TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			UNIQUE(name)
		)`,
		`CREATE TABLE IF NOT EXISTS container_service_files (
			id TEXT PRIMARY KEY,
			service_id TEXT NOT NULL,
			path TEXT NOT NULL,
			kind TEXT NOT NULL CHECK(kind IN ('binary','template')),
			content_type TEXT NOT NULL DEFAULT '',
			size INTEGER NOT NULL DEFAULT 0,
			sha256 TEXT NOT NULL DEFAULT '',
			content BLOB,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			UNIQUE(service_id, path),
			FOREIGN KEY(service_id) REFERENCES container_services(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_container_services_name ON container_services(name)`,
	}
	for _, stmt := range app {
		if _, err := s.appDB.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	if err := s.ensureAppColumns(ctx, "tasks", map[string]string{
		"operation_id":          "TEXT NOT NULL DEFAULT ''",
		"node_id":               "TEXT NOT NULL DEFAULT ''",
		"resource_type":         "TEXT NOT NULL DEFAULT ''",
		"resource_id":           "TEXT NOT NULL DEFAULT ''",
		"trigger_type":          "TEXT NOT NULL DEFAULT ''",
		"trigger_resource_type": "TEXT NOT NULL DEFAULT ''",
		"trigger_resource_id":   "TEXT NOT NULL DEFAULT ''",
		"trigger_task_id":       "TEXT NOT NULL DEFAULT ''",
		"triggered_by":          "TEXT NOT NULL DEFAULT ''",
		"retry_count":           "INTEGER NOT NULL DEFAULT 0",
		"max_retries":           "INTEGER NOT NULL DEFAULT 0",
		"next_run_at":           "TEXT",
	}); err != nil {
		return err
	}
	if err := s.ensureAppColumns(ctx, "docker_capabilities", map[string]string{
		"include_supported": "INTEGER NOT NULL DEFAULT 0",
	}); err != nil {
		return err
	}
	if err := s.ensureAppColumns(ctx, "servers", map[string]string{
		"traits": "TEXT NOT NULL DEFAULT '{}'",
	}); err != nil {
		return err
	}
	metrics := []string{
		`CREATE TABLE IF NOT EXISTS metrics_snapshots (
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
			load_average TEXT NOT NULL DEFAULT '',
			uptime_seconds INTEGER NOT NULL DEFAULT 0,
			hostname TEXT NOT NULL DEFAULT '',
			kernel_version TEXT NOT NULL DEFAULT '',
			os_version TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_metrics_snapshots_server_time ON metrics_snapshots(server_id, time)`,
	}
	for _, stmt := range metrics {
		if _, err := s.metricsDB.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ensureAppColumns(ctx context.Context, table string, columns map[string]string) error {
	rows, err := s.appDB.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	existing := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, pk int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		existing[name] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for name, definition := range columns {
		if existing[name] {
			continue
		}
		if _, err := s.appDB.ExecContext(ctx, `ALTER TABLE `+table+` ADD COLUMN `+name+` `+definition); err != nil {
			return err
		}
	}
	return nil
}
