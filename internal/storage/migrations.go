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
		`CREATE TABLE IF NOT EXISTS applications (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			enabled INTEGER NOT NULL DEFAULT 0,
			spec_yaml TEXT NOT NULL,
			variables_json TEXT NOT NULL DEFAULT '{}',
			generation INTEGER NOT NULL DEFAULT 1,
			spec_hash TEXT NOT NULL DEFAULT '',
			job_id TEXT NOT NULL,
			namespace TEXT NOT NULL DEFAULT 'default',
			last_eval_id TEXT NOT NULL DEFAULT '',
			last_deployment_id TEXT NOT NULL DEFAULT '',
			last_error TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			UNIQUE(name)
		)`,
		`CREATE TABLE IF NOT EXISTS application_files (
			id TEXT PRIMARY KEY,
			application_id TEXT NOT NULL,
			path TEXT NOT NULL,
			kind TEXT NOT NULL CHECK(kind IN ('binary','template')),
			content_type TEXT NOT NULL DEFAULT '',
			size INTEGER NOT NULL DEFAULT 0,
			sha256 TEXT NOT NULL DEFAULT '',
			content BLOB,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			UNIQUE(application_id, path),
			FOREIGN KEY(application_id) REFERENCES applications(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS application_revisions (
			id TEXT PRIMARY KEY,
			application_id TEXT NOT NULL,
			generation INTEGER NOT NULL,
			spec_hash TEXT NOT NULL,
			spec_yaml TEXT NOT NULL,
			job_json TEXT NOT NULL,
			created_at TEXT NOT NULL,
			UNIQUE(application_id, generation),
			FOREIGN KEY(application_id) REFERENCES applications(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS dns_domains (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			provider TEXT NOT NULL CHECK(provider IN ('cloudflare')),
			api_token_secret TEXT NOT NULL DEFAULT '',
			account_id TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS certificates (
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
			auto_renew INTEGER NOT NULL DEFAULT 1,
			next_renew_at TEXT NOT NULL DEFAULT '',
			not_before TEXT NOT NULL DEFAULT '',
			not_after TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			FOREIGN KEY(domain_id) REFERENCES dns_domains(id)
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
	if err := s.ensureAppColumns(ctx, "servers", map[string]string{
		"traits": "TEXT NOT NULL DEFAULT '{}'",
	}); err != nil {
		return err
	}
	if err := s.ensureAppColumns(ctx, "certificates", map[string]string{
		"domain_id":     "TEXT NOT NULL DEFAULT ''",
		"prefix":        "TEXT NOT NULL DEFAULT '@'",
		"auto_renew":    "INTEGER NOT NULL DEFAULT 1",
		"next_renew_at": "TEXT NOT NULL DEFAULT ''",
	}); err != nil {
		return err
	}
	if err := s.ensureAppColumns(ctx, "dns_domains", map[string]string{
		"account_id": "TEXT NOT NULL DEFAULT ''",
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
