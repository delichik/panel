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
			variables_json TEXT NOT NULL DEFAULT '{}',
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
			resolved_variables_json TEXT NOT NULL DEFAULT '{}',
			persistent_path TEXT NOT NULL DEFAULT '',
			deployment_mode TEXT NOT NULL DEFAULT 'all',
			deployment_server_ids_json TEXT NOT NULL DEFAULT '[]',
			reverse_proxy_json TEXT NOT NULL DEFAULT '[]',
			generation INTEGER NOT NULL DEFAULT 1,
			spec_hash TEXT NOT NULL DEFAULT '',
			image_reference TEXT NOT NULL DEFAULT '',
			image_digest TEXT NOT NULL DEFAULT '',
			image_latest_digest TEXT NOT NULL DEFAULT '',
			image_checked_at TEXT,
			image_update_available INTEGER NOT NULL DEFAULT 0,
			image_last_error TEXT NOT NULL DEFAULT '',
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
			provider_config_json TEXT NOT NULL DEFAULT '{}',
			provider_secret_ciphertext TEXT NOT NULL DEFAULT '',
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
			status TEXT NOT NULL DEFAULT 'pending',
			last_error TEXT NOT NULL DEFAULT '',
			auto_renew INTEGER NOT NULL DEFAULT 1,
			next_renew_at TEXT NOT NULL DEFAULT '',
			not_before TEXT NOT NULL DEFAULT '',
			not_after TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			FOREIGN KEY(domain_id) REFERENCES dns_domains(id)
		)`,
		`CREATE TABLE IF NOT EXISTS self_signed_certificates (
			id TEXT PRIMARY KEY,
			parent_ca_id TEXT NOT NULL DEFAULT '',
			kind TEXT NOT NULL CHECK(kind IN ('ca','leaf')),
			name TEXT NOT NULL,
			common_name TEXT NOT NULL,
			dns_names_json TEXT NOT NULL DEFAULT '[]',
			ip_addresses_json TEXT NOT NULL DEFAULT '[]',
			certificate_path TEXT NOT NULL,
			private_key_path TEXT NOT NULL,
			public_key_path TEXT NOT NULL,
			fingerprint TEXT NOT NULL DEFAULT '',
			not_before TEXT NOT NULL DEFAULT '',
			not_after TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS key_assets (
			id TEXT PRIMARY KEY,
			type TEXT NOT NULL CHECK(type IN ('ca_certificate','tls_certificate','ssh_key_pair')),
			name TEXT NOT NULL,
			parent_asset_id TEXT NOT NULL DEFAULT '',
			algorithm TEXT NOT NULL DEFAULT '',
			key_size INTEGER NOT NULL DEFAULT 0,
			common_name TEXT NOT NULL DEFAULT '',
			dns_names_json TEXT NOT NULL DEFAULT '[]',
			ip_addresses_json TEXT NOT NULL DEFAULT '[]',
			fingerprint TEXT NOT NULL DEFAULT '',
			certificate_ciphertext TEXT NOT NULL DEFAULT '',
			private_key_ciphertext TEXT NOT NULL DEFAULT '',
			public_key TEXT NOT NULL DEFAULT '',
			metadata_json TEXT NOT NULL DEFAULT '{}',
			not_before TEXT NOT NULL DEFAULT '',
			not_after TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_key_assets_name ON key_assets(name)`,
		`CREATE INDEX IF NOT EXISTS idx_key_assets_type ON key_assets(type)`,
		`CREATE INDEX IF NOT EXISTS idx_key_assets_parent_asset_id ON key_assets(parent_asset_id)`,
		`CREATE TABLE IF NOT EXISTS key_asset_exports (
			task_id TEXT PRIMARY KEY,
			filename TEXT NOT NULL,
			file_path TEXT NOT NULL,
			expires_at TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_key_asset_exports_expires_at ON key_asset_exports(expires_at)`,
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
		`CREATE TABLE IF NOT EXISTS auth_state (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS auth_accounts (
			id TEXT PRIMARY KEY CHECK(id = 'admin'),
			username TEXT NOT NULL,
			password_hash TEXT NOT NULL,
			password_change_required INTEGER NOT NULL DEFAULT 1,
			created_at TEXT NOT NULL,
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
	if err := s.ensureAppColumns(ctx, "credentials", map[string]string{
		"name":              "TEXT NOT NULL DEFAULT ''",
		"type":              "TEXT NOT NULL DEFAULT 'password'",
		"username":          "TEXT NOT NULL DEFAULT ''",
		"password_secret":   "TEXT NOT NULL DEFAULT ''",
		"private_key_path":  "TEXT NOT NULL DEFAULT ''",
		"passphrase_secret": "TEXT NOT NULL DEFAULT ''",
		"created_at":        "TEXT NOT NULL DEFAULT ''",
		"updated_at":        "TEXT NOT NULL DEFAULT ''",
	}); err != nil {
		return err
	}
	if err := s.ensureAppColumns(ctx, "applications", map[string]string{
		"persistent_path":            "TEXT NOT NULL DEFAULT ''",
		"resolved_variables_json":    "TEXT NOT NULL DEFAULT '{}'",
		"deployment_mode":            "TEXT NOT NULL DEFAULT 'all'",
		"deployment_server_ids_json": "TEXT NOT NULL DEFAULT '[]'",
		"reverse_proxy_json":         "TEXT NOT NULL DEFAULT '[]'",
		"image_reference":            "TEXT NOT NULL DEFAULT ''",
		"image_digest":               "TEXT NOT NULL DEFAULT ''",
		"image_latest_digest":        "TEXT NOT NULL DEFAULT ''",
		"image_checked_at":           "TEXT",
		"image_update_available":     "INTEGER NOT NULL DEFAULT 0",
		"image_last_error":           "TEXT NOT NULL DEFAULT ''",
	}); err != nil {
		return err
	}
	if err := s.ensureAppColumns(ctx, "servers", map[string]string{
		"name":                 "TEXT NOT NULL DEFAULT ''",
		"host":                 "TEXT NOT NULL DEFAULT ''",
		"port":                 "INTEGER NOT NULL DEFAULT 22",
		"ssh_username":         "TEXT NOT NULL DEFAULT ''",
		"credential_id":        "TEXT NOT NULL DEFAULT ''",
		"traits":               "TEXT NOT NULL DEFAULT '{}'",
		"variables_json":       "TEXT NOT NULL DEFAULT '{}'",
		"notes":                "TEXT NOT NULL DEFAULT ''",
		"os_id":                "TEXT NOT NULL DEFAULT ''",
		"os_version_id":        "TEXT NOT NULL DEFAULT ''",
		"os_pretty_name":       "TEXT NOT NULL DEFAULT ''",
		"os_supported":         "INTEGER NOT NULL DEFAULT 0",
		"reachable":            "INTEGER NOT NULL DEFAULT 0",
		"sudo_passwordless":    "INTEGER NOT NULL DEFAULT 0",
		"sudo_last_checked_at": "TEXT",
		"last_checked_at":      "TEXT",
		"last_error":           "TEXT NOT NULL DEFAULT ''",
		"created_at":           "TEXT NOT NULL DEFAULT ''",
		"updated_at":           "TEXT NOT NULL DEFAULT ''",
	}); err != nil {
		return err
	}
	if err := s.normalizeAppDefaults(ctx); err != nil {
		return err
	}
	if err := s.ensureAppColumns(ctx, "certificates", map[string]string{
		"domain_id":     "TEXT NOT NULL DEFAULT ''",
		"prefix":        "TEXT NOT NULL DEFAULT '@'",
		"status":        "TEXT NOT NULL DEFAULT 'pending'",
		"last_error":    "TEXT NOT NULL DEFAULT ''",
		"auto_renew":    "INTEGER NOT NULL DEFAULT 1",
		"next_renew_at": "TEXT NOT NULL DEFAULT ''",
	}); err != nil {
		return err
	}
	if err := s.ensureAppColumns(ctx, "dns_domains", map[string]string{
		"provider_config_json":       "TEXT NOT NULL DEFAULT '{}'",
		"provider_secret_ciphertext": "TEXT NOT NULL DEFAULT ''",
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

func (s *Store) normalizeAppDefaults(ctx context.Context) error {
	nowExpr := `strftime('%Y-%m-%dT%H:%M:%SZ','now')`
	statements := []string{
		`UPDATE credentials SET
			name=COALESCE(name, ''),
			type=CASE WHEN type IN ('password','private_key') THEN type ELSE 'password' END,
			username=COALESCE(username, ''),
			password_secret=COALESCE(password_secret, ''),
			private_key_path=COALESCE(private_key_path, ''),
			passphrase_secret=COALESCE(passphrase_secret, ''),
			created_at=COALESCE(NULLIF(created_at, ''), ` + nowExpr + `),
			updated_at=COALESCE(NULLIF(updated_at, ''), NULLIF(created_at, ''), ` + nowExpr + `)`,
		`UPDATE servers SET credential_id=(SELECT id FROM credentials WHERE id IS NOT NULL AND id != '' ORDER BY created_at DESC LIMIT 1)
			WHERE (credential_id IS NULL OR credential_id = '')
			  AND EXISTS (SELECT 1 FROM credentials WHERE id IS NOT NULL AND id != '')`,
		`UPDATE servers SET
			name=COALESCE(name, ''),
			host=COALESCE(host, ''),
			port=COALESCE(port, 22),
			ssh_username=COALESCE(ssh_username, ''),
			traits=COALESCE(traits, '{}'),
			variables_json=COALESCE(variables_json, '{}'),
			notes=COALESCE(notes, ''),
			os_id=COALESCE(os_id, ''),
			os_version_id=COALESCE(os_version_id, ''),
			os_pretty_name=COALESCE(os_pretty_name, ''),
			os_supported=COALESCE(os_supported, 0),
			reachable=COALESCE(reachable, 0),
			sudo_passwordless=COALESCE(sudo_passwordless, 0),
			last_error=COALESCE(last_error, ''),
			created_at=COALESCE(NULLIF(created_at, ''), ` + nowExpr + `),
			updated_at=COALESCE(NULLIF(updated_at, ''), NULLIF(created_at, ''), ` + nowExpr + `)`,
	}
	for _, stmt := range statements {
		if _, err := s.appDB.ExecContext(ctx, stmt); err != nil {
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
