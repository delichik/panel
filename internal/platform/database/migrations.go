package database

import (
	"context"
	"database/sql"
)

func (s *Store) Migrate(ctx context.Context) error {
	app := []string{
		`PRAGMA foreign_keys = ON`,
		`CREATE TABLE IF NOT EXISTS credentials (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			type TEXT NOT NULL CHECK(type IN ('password','private_key')),
			username TEXT NOT NULL,
			secret_ciphertext TEXT NOT NULL DEFAULT '',
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
			docker_host TEXT NOT NULL DEFAULT 'unix:///var/run/docker.sock',
			traits TEXT NOT NULL DEFAULT '{}',
			variables_json TEXT NOT NULL DEFAULT '{}',
			notes TEXT NOT NULL DEFAULT '',
			os_id TEXT NOT NULL DEFAULT '',
			os_version_id TEXT NOT NULL DEFAULT '',
			os_pretty_name TEXT NOT NULL DEFAULT '',
			os_supported INTEGER NOT NULL DEFAULT 0,
			architecture_os TEXT NOT NULL DEFAULT '',
			architecture_arch TEXT NOT NULL DEFAULT '',
			architecture_machine TEXT NOT NULL DEFAULT '',
			reachable INTEGER NOT NULL DEFAULT 0,
			sudo_passwordless INTEGER NOT NULL DEFAULT 0,
			sudo_last_checked_at TEXT,
			privilege_mode TEXT NOT NULL DEFAULT '',
			privilege_last_checked_at TEXT,
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
		`CREATE TABLE IF NOT EXISTS fail2ban_configs (
			server_id TEXT PRIMARY KEY,
			config_yaml TEXT NOT NULL,
			managed INTEGER NOT NULL DEFAULT 0,
			updated_at TEXT NOT NULL,
			FOREIGN KEY(server_id) REFERENCES servers(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS image_updates (
			server_id TEXT NOT NULL,
			reference TEXT NOT NULL,
			local_digest TEXT NOT NULL DEFAULT '',
			latest_digest TEXT NOT NULL DEFAULT '',
			update_available INTEGER NOT NULL DEFAULT 0,
			last_error TEXT NOT NULL DEFAULT '',
			checked_at TEXT NOT NULL,
			PRIMARY KEY(server_id, reference),
			FOREIGN KEY(server_id) REFERENCES servers(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS image_refreshes (
			server_id TEXT PRIMARY KEY,
			refreshed_at TEXT NOT NULL,
			FOREIGN KEY(server_id) REFERENCES servers(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS application_reconcile_states (
			instance_id TEXT PRIMARY KEY,
			application_id TEXT NOT NULL,
			server_id TEXT NOT NULL,
			observed_at TEXT NOT NULL,
			reconcile_failures INTEGER NOT NULL DEFAULT 0,
			reconcile_next_run_at TEXT NOT NULL DEFAULT '',
			reconcile_success_streak INTEGER NOT NULL DEFAULT 0,
			FOREIGN KEY(application_id) REFERENCES applications(id) ON DELETE CASCADE,
			FOREIGN KEY(server_id) REFERENCES servers(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS container_observations (
			server_id TEXT NOT NULL,
			container_id TEXT NOT NULL,
			sample_at TEXT NOT NULL,
			container_json TEXT NOT NULL,
			managed INTEGER NOT NULL DEFAULT 0,
			application_id TEXT NOT NULL DEFAULT '',
			instance_id TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL,
			PRIMARY KEY(server_id, container_id),
			FOREIGN KEY(server_id) REFERENCES servers(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_container_observations_server_sample ON container_observations(server_id, sample_at)`,
		`CREATE TABLE IF NOT EXISTS applications (
			id TEXT PRIMARY KEY,
			kind TEXT NOT NULL DEFAULT 'application',
			name TEXT NOT NULL,
			enabled INTEGER NOT NULL DEFAULT 0,
			deletion_requested INTEGER NOT NULL DEFAULT 0,
			spec_yaml TEXT NOT NULL,
			variables_json TEXT NOT NULL DEFAULT '{}',
			resolved_variables_json TEXT NOT NULL DEFAULT '{}',
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
		`CREATE TABLE IF NOT EXISTS application_lifecycle_operations (
			id TEXT PRIMARY KEY,
			application_id TEXT NOT NULL,
			type TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'pending',
			task_id TEXT NOT NULL DEFAULT '',
			generation INTEGER NOT NULL DEFAULT 0,
			spec_hash TEXT NOT NULL DEFAULT '',
			trigger TEXT NOT NULL DEFAULT '',
			error TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			started_at TEXT,
			finished_at TEXT,
			updated_at TEXT NOT NULL,
			FOREIGN KEY(application_id) REFERENCES applications(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_application_lifecycle_operations_app_created ON application_lifecycle_operations(application_id, created_at)`,
		`CREATE TABLE IF NOT EXISTS application_lifecycle_targets (
			id TEXT PRIMARY KEY,
			operation_id TEXT NOT NULL,
			application_id TEXT NOT NULL,
			server_id TEXT NOT NULL,
			action TEXT NOT NULL DEFAULT 'apply',
			state TEXT NOT NULL DEFAULT 'planned',
			status TEXT NOT NULL DEFAULT 'pending',
			target_key TEXT NOT NULL DEFAULT '',
			desired_state TEXT NOT NULL DEFAULT 'running',
			desired_generation INTEGER NOT NULL DEFAULT 0,
			desired_spec_hash TEXT NOT NULL DEFAULT '',
			priority INTEGER NOT NULL DEFAULT 0,
			attempt INTEGER NOT NULL DEFAULT 0,
			next_run_at TEXT NOT NULL DEFAULT '',
			lease_owner TEXT NOT NULL DEFAULT '',
			lease_expires_at TEXT NOT NULL DEFAULT '',
			claimed_task_id TEXT NOT NULL DEFAULT '',
			instance_id TEXT NOT NULL DEFAULT '',
			container_name TEXT NOT NULL DEFAULT '',
			container_id TEXT NOT NULL DEFAULT '',
			stage TEXT NOT NULL DEFAULT '',
			error TEXT NOT NULL DEFAULT '',
			error_code TEXT NOT NULL DEFAULT '',
			error_message TEXT NOT NULL DEFAULT '',
			error_detail TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			started_at TEXT,
			finished_at TEXT,
			updated_at TEXT NOT NULL,
			UNIQUE(operation_id, server_id),
			FOREIGN KEY(operation_id) REFERENCES application_lifecycle_operations(id) ON DELETE CASCADE,
			FOREIGN KEY(application_id) REFERENCES applications(id) ON DELETE CASCADE,
			FOREIGN KEY(server_id) REFERENCES servers(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_application_lifecycle_targets_operation ON application_lifecycle_targets(operation_id, server_id)`,
		`CREATE TABLE IF NOT EXISTS application_instances (
			id TEXT PRIMARY KEY,
			application_id TEXT NOT NULL,
			server_id TEXT NOT NULL,
			container_name TEXT NOT NULL,
			container_id TEXT NOT NULL DEFAULT '',
			desired_state TEXT NOT NULL DEFAULT 'running',
			status TEXT NOT NULL DEFAULT 'pending',
			runtime_spec_json TEXT NOT NULL DEFAULT '{}',
			last_deployed_generation INTEGER NOT NULL DEFAULT 0,
			last_error TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			UNIQUE(application_id, server_id),
			FOREIGN KEY(application_id) REFERENCES applications(id) ON DELETE CASCADE,
			FOREIGN KEY(server_id) REFERENCES servers(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS facility_app_configs (
			id TEXT PRIMARY KEY,
			deployment_server_ids_json TEXT NOT NULL DEFAULT '[]',
			image TEXT NOT NULL DEFAULT '',
			panel_entry_json TEXT NOT NULL DEFAULT '{}',
			static_sites_json TEXT NOT NULL DEFAULT '[]',
			last_error TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS facility_static_assets (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			kind TEXT NOT NULL CHECK(kind IN ('uploaded_file','uploaded_bundle')),
			filename TEXT NOT NULL DEFAULT '',
			size INTEGER NOT NULL DEFAULT 0,
			sha256 TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
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
		`CREATE TABLE IF NOT EXISTS overview_card_configurations (
			id TEXT PRIMARY KEY CHECK(id = 'default'),
			cards_json TEXT NOT NULL DEFAULT '[]',
			updated_at TEXT NOT NULL
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
	if err := s.resetLegacyTaskTables(ctx); err != nil {
		return err
	}
	task := []string{
		`PRAGMA foreign_keys = ON`,
		`CREATE TABLE IF NOT EXISTS tasks (
			id TEXT PRIMARY KEY,
			operation_id TEXT NOT NULL DEFAULT '',
			type TEXT NOT NULL,
			parent_task_id TEXT NOT NULL DEFAULT '',
			child_index INTEGER NOT NULL DEFAULT 0,
			child_count INTEGER NOT NULL DEFAULT 0,
			execution_mode TEXT NOT NULL DEFAULT '',
			concurrency_key TEXT NOT NULL DEFAULT '',
			schedule_key TEXT NOT NULL DEFAULT '',
			server_id TEXT NOT NULL DEFAULT '',
			node_id TEXT NOT NULL DEFAULT '',
			resource_type TEXT NOT NULL DEFAULT '',
			resource_id TEXT NOT NULL DEFAULT '',
			trigger_type TEXT NOT NULL DEFAULT '',
			trigger_resource_type TEXT NOT NULL DEFAULT '',
			trigger_resource_id TEXT NOT NULL DEFAULT '',
			trigger_task_id TEXT NOT NULL DEFAULT '',
			triggered_by TEXT NOT NULL DEFAULT '',
			params_json TEXT NOT NULL DEFAULT '{}',
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
		`CREATE INDEX IF NOT EXISTS idx_tasks_type_status ON tasks(type,status)`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_concurrency_status ON tasks(concurrency_key,status)`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_parent ON tasks(parent_task_id,child_index)`,
	}
	for _, stmt := range task {
		if _, err := s.taskDB.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	if err := s.ensureAppColumns(ctx, "credentials", map[string]string{
		"name":              "TEXT NOT NULL DEFAULT ''",
		"type":              "TEXT NOT NULL DEFAULT 'password'",
		"username":          "TEXT NOT NULL DEFAULT ''",
		"secret_ciphertext": "TEXT NOT NULL DEFAULT ''",
		"password_secret":   "TEXT NOT NULL DEFAULT ''",
		"private_key_path":  "TEXT NOT NULL DEFAULT ''",
		"passphrase_secret": "TEXT NOT NULL DEFAULT ''",
		"created_at":        "TEXT NOT NULL DEFAULT ''",
		"updated_at":        "TEXT NOT NULL DEFAULT ''",
	}); err != nil {
		return err
	}
	if err := s.ensureAppColumns(ctx, "applications", map[string]string{
		"kind":                       "TEXT NOT NULL DEFAULT 'application'",
		"deletion_requested":         "INTEGER NOT NULL DEFAULT 0",
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
	if err := s.ensureAppColumns(ctx, "application_reconcile_states", map[string]string{
		"reconcile_failures":       "INTEGER NOT NULL DEFAULT 0",
		"reconcile_next_run_at":    "TEXT NOT NULL DEFAULT ''",
		"reconcile_success_streak": "INTEGER NOT NULL DEFAULT 0",
	}); err != nil {
		return err
	}
	if err := s.migrateApplicationLifecycleTargets(ctx); err != nil {
		return err
	}
	if err := s.dropAppColumnIfExists(ctx, "applications", "persistent_path"); err != nil {
		return err
	}
	if err := s.ensureAppColumns(ctx, "facility_app_configs", map[string]string{
		"deployment_server_ids_json": "TEXT NOT NULL DEFAULT '[]'",
		"image":                      "TEXT NOT NULL DEFAULT ''",
		"panel_entry_json":           "TEXT NOT NULL DEFAULT '{}'",
		"static_sites_json":          "TEXT NOT NULL DEFAULT '[]'",
		"last_error":                 "TEXT NOT NULL DEFAULT ''",
		"updated_at":                 "TEXT NOT NULL DEFAULT ''",
	}); err != nil {
		return err
	}
	if err := s.ensureAppColumns(ctx, "facility_static_assets", map[string]string{
		"name":       "TEXT NOT NULL DEFAULT ''",
		"kind":       "TEXT NOT NULL DEFAULT 'uploaded_file'",
		"filename":   "TEXT NOT NULL DEFAULT ''",
		"size":       "INTEGER NOT NULL DEFAULT 0",
		"sha256":     "TEXT NOT NULL DEFAULT ''",
		"created_at": "TEXT NOT NULL DEFAULT ''",
		"updated_at": "TEXT NOT NULL DEFAULT ''",
	}); err != nil {
		return err
	}
	if err := s.ensureAppColumns(ctx, "fail2ban_configs", map[string]string{
		"managed": "INTEGER NOT NULL DEFAULT 0",
	}); err != nil {
		return err
	}
	if err := s.ensureAppColumns(ctx, "servers", map[string]string{
		"name":                      "TEXT NOT NULL DEFAULT ''",
		"host":                      "TEXT NOT NULL DEFAULT ''",
		"port":                      "INTEGER NOT NULL DEFAULT 22",
		"ssh_username":              "TEXT NOT NULL DEFAULT ''",
		"credential_id":             "TEXT NOT NULL DEFAULT ''",
		"docker_host":               "TEXT NOT NULL DEFAULT 'unix:///var/run/docker.sock'",
		"traits":                    "TEXT NOT NULL DEFAULT '{}'",
		"variables_json":            "TEXT NOT NULL DEFAULT '{}'",
		"notes":                     "TEXT NOT NULL DEFAULT ''",
		"os_id":                     "TEXT NOT NULL DEFAULT ''",
		"os_version_id":             "TEXT NOT NULL DEFAULT ''",
		"os_pretty_name":            "TEXT NOT NULL DEFAULT ''",
		"os_supported":              "INTEGER NOT NULL DEFAULT 0",
		"architecture_os":           "TEXT NOT NULL DEFAULT ''",
		"architecture_arch":         "TEXT NOT NULL DEFAULT ''",
		"architecture_machine":      "TEXT NOT NULL DEFAULT ''",
		"reachable":                 "INTEGER NOT NULL DEFAULT 0",
		"sudo_passwordless":         "INTEGER NOT NULL DEFAULT 0",
		"sudo_last_checked_at":      "TEXT",
		"privilege_mode":            "TEXT NOT NULL DEFAULT ''",
		"privilege_last_checked_at": "TEXT",
		"last_checked_at":           "TEXT",
		"last_error":                "TEXT NOT NULL DEFAULT ''",
		"created_at":                "TEXT NOT NULL DEFAULT ''",
		"updated_at":                "TEXT NOT NULL DEFAULT ''",
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
			load_1 REAL NOT NULL DEFAULT 0,
			load_5 REAL NOT NULL DEFAULT 0,
			load_15 REAL NOT NULL DEFAULT 0,
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
	if err := ensureColumns(ctx, s.metricsDB, "metrics_snapshots", map[string]string{
		"load_1":  "REAL NOT NULL DEFAULT 0",
		"load_5":  "REAL NOT NULL DEFAULT 0",
		"load_15": "REAL NOT NULL DEFAULT 0",
	}); err != nil {
		return err
	}
	return nil
}

func (s *Store) resetLegacyTaskTables(ctx context.Context) error {
	rows, err := s.taskDB.QueryContext(ctx, `PRAGMA table_info(tasks)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	hasTaskTable := false
	requiredColumns := map[string]bool{
		"parent_task_id":  false,
		"child_index":     false,
		"child_count":     false,
		"execution_mode":  false,
		"concurrency_key": false,
		"schedule_key":    false,
		"triggered_by":    false,
		"params_json":     false,
	}
	for rows.Next() {
		hasTaskTable = true
		var cid int
		var name, typ string
		var notNull, pk int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		if _, ok := requiredColumns[name]; ok {
			requiredColumns[name] = true
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if !hasTaskTable {
		return nil
	}
	for _, found := range requiredColumns {
		if !found {
			return s.dropTaskTables(ctx)
		}
	}
	return nil
}

func (s *Store) dropTaskTables(ctx context.Context) error {
	for _, stmt := range []string{
		`DROP TABLE IF EXISTS task_logs`,
		`DROP TABLE IF EXISTS task_steps`,
		`DROP TABLE IF EXISTS tasks`,
	} {
		if _, err := s.taskDB.ExecContext(ctx, stmt); err != nil {
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
			secret_ciphertext=COALESCE(secret_ciphertext, ''),
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
			docker_host=COALESCE(NULLIF(docker_host, ''), 'unix:///var/run/docker.sock'),
			variables_json=COALESCE(variables_json, '{}'),
			notes=COALESCE(notes, ''),
			os_id=COALESCE(os_id, ''),
			os_version_id=COALESCE(os_version_id, ''),
			os_pretty_name=COALESCE(os_pretty_name, ''),
			os_supported=COALESCE(os_supported, 0),
			architecture_os=COALESCE(NULLIF(architecture_os, ''), CASE WHEN json_extract(traits, '$.sys.architecture') IS NOT NULL THEN 'linux' ELSE '' END),
			architecture_arch=COALESCE(NULLIF(architecture_arch, ''), CASE
				WHEN lower(json_extract(traits, '$.sys.architecture')) IN ('amd64','x86_64') THEN 'amd64'
				WHEN lower(json_extract(traits, '$.sys.architecture')) IN ('arm64','aarch64') THEN 'arm64'
				ELSE '' END),
			architecture_machine=COALESCE(NULLIF(architecture_machine, ''), json_extract(traits, '$.sys.architecture'), ''),
			reachable=COALESCE(reachable, 0),
			sudo_passwordless=COALESCE(sudo_passwordless, 0),
			privilege_mode=CASE
				WHEN privilege_mode IN ('root','passwordless_sudo','none') THEN privilege_mode
				WHEN lower(trim(COALESCE(ssh_username, ''))) = 'root' THEN 'root'
				WHEN trim(COALESCE(ssh_username, '')) = '' AND EXISTS (
					SELECT 1 FROM credentials
					WHERE credentials.id = servers.credential_id
					  AND lower(trim(COALESCE(credentials.username, ''))) = 'root'
				) THEN 'root'
				WHEN COALESCE(sudo_passwordless, 0) = 1 THEN 'passwordless_sudo'
				ELSE 'none' END,
			privilege_last_checked_at=COALESCE(privilege_last_checked_at, sudo_last_checked_at, last_checked_at),
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

func (s *Store) migrateApplicationLifecycleTargets(ctx context.Context) error {
	if err := s.ensureAppColumns(ctx, "application_lifecycle_targets", map[string]string{
		"action":             "TEXT NOT NULL DEFAULT 'apply'",
		"state":              "TEXT NOT NULL DEFAULT 'planned'",
		"target_key":         "TEXT NOT NULL DEFAULT ''",
		"desired_generation": "INTEGER NOT NULL DEFAULT 0",
		"desired_spec_hash":  "TEXT NOT NULL DEFAULT ''",
		"priority":           "INTEGER NOT NULL DEFAULT 0",
		"attempt":            "INTEGER NOT NULL DEFAULT 0",
		"next_run_at":        "TEXT NOT NULL DEFAULT ''",
		"lease_owner":        "TEXT NOT NULL DEFAULT ''",
		"lease_expires_at":   "TEXT NOT NULL DEFAULT ''",
		"claimed_task_id":    "TEXT NOT NULL DEFAULT ''",
		"error_code":         "TEXT NOT NULL DEFAULT ''",
		"error_message":      "TEXT NOT NULL DEFAULT ''",
		"error_detail":       "TEXT NOT NULL DEFAULT ''",
	}); err != nil {
		return err
	}
	statements := []string{
		`UPDATE application_lifecycle_targets
			SET action=CASE
					WHEN action IN ('apply','stop','purge') THEN action
					WHEN desired_state='stopped' THEN 'stop'
					ELSE 'apply'
				END,
				state=CASE
					WHEN state IN ('planned','ready','claimed','preparing','applying','stopping','purging','verifying','succeeded','failed_retryable','failed','superseded','cancelled') THEN state
					WHEN status='pending' THEN 'planned'
					WHEN status='preparing' THEN 'preparing'
					WHEN status='deploying' AND desired_state='stopped' THEN 'stopping'
					WHEN status='deploying' THEN 'applying'
					WHEN status='running' THEN 'succeeded'
					WHEN status='failed' THEN 'failed'
					WHEN status='superseded' THEN 'superseded'
					ELSE 'planned'
				END,
				target_key=CASE
					WHEN target_key <> '' THEN target_key
					ELSE 'application:' || application_id || ':server:' || server_id
				END,
				desired_generation=CASE
					WHEN desired_generation > 0 THEN desired_generation
					ELSE COALESCE((SELECT generation FROM application_lifecycle_operations WHERE application_lifecycle_operations.id=application_lifecycle_targets.operation_id), 0)
				END,
				desired_spec_hash=CASE
					WHEN desired_spec_hash <> '' THEN desired_spec_hash
					ELSE COALESCE((SELECT spec_hash FROM application_lifecycle_operations WHERE application_lifecycle_operations.id=application_lifecycle_targets.operation_id), '')
				END,
				priority=CASE
					WHEN priority > 0 THEN priority
					WHEN action='purge' THEN 30
					WHEN action='stop' OR desired_state='stopped' THEN 20
					ELSE 10
				END,
				error_message=CASE WHEN error_message <> '' THEN error_message ELSE COALESCE(error, '') END,
				error_detail=CASE WHEN error_detail <> '' THEN error_detail ELSE COALESCE(error, '') END`,
		`WITH ranked AS (
			SELECT id,
				ROW_NUMBER() OVER (
					PARTITION BY target_key
					ORDER BY updated_at DESC, created_at DESC, id DESC
				) AS rn
			FROM application_lifecycle_targets
			WHERE target_key <> ''
			  AND state IN ('planned','ready','claimed','preparing','applying','stopping','purging','verifying','failed_retryable')
		)
		UPDATE application_lifecycle_targets
			SET state='superseded',
				status='superseded',
				error=CASE WHEN error <> '' THEN error ELSE 'Superseded during lifecycle state-machine migration' END,
				error_code=CASE WHEN error_code <> '' THEN error_code ELSE 'superseded' END,
				error_message=CASE WHEN error_message <> '' THEN error_message ELSE 'Superseded during lifecycle state-machine migration' END,
				error_detail=CASE WHEN error_detail <> '' THEN error_detail ELSE 'Older duplicate active target superseded before adding active target uniqueness' END,
				stage=CASE WHEN stage <> '' THEN stage ELSE 'superseded' END,
				finished_at=COALESCE(finished_at, strftime('%Y-%m-%dT%H:%M:%SZ','now')),
				updated_at=strftime('%Y-%m-%dT%H:%M:%SZ','now')
			WHERE id IN (SELECT id FROM ranked WHERE rn > 1)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_application_lifecycle_targets_active_key
			ON application_lifecycle_targets(target_key)
			WHERE target_key <> ''
			  AND state IN ('planned','ready','claimed','preparing','applying','stopping','purging','verifying','failed_retryable')`,
		`CREATE INDEX IF NOT EXISTS idx_application_lifecycle_targets_state_due
			ON application_lifecycle_targets(state, next_run_at)`,
		`CREATE INDEX IF NOT EXISTS idx_application_lifecycle_targets_app_server
			ON application_lifecycle_targets(application_id, server_id, state)`,
		`CREATE INDEX IF NOT EXISTS idx_application_lifecycle_targets_operation
			ON application_lifecycle_targets(operation_id, server_id)`,
	}
	for _, stmt := range statements {
		if _, err := s.appDB.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ensureAppColumns(ctx context.Context, table string, columns map[string]string) error {
	return ensureColumns(ctx, s.appDB, table, columns)
}

func (s *Store) dropAppColumnIfExists(ctx context.Context, table, column string) error {
	return dropColumnIfExists(ctx, s.appDB, table, column)
}

func (s *Store) ensureTaskColumns(ctx context.Context, table string, columns map[string]string) error {
	return ensureColumns(ctx, s.taskDB, table, columns)
}

func dropColumnIfExists(ctx context.Context, db *sql.DB, table, column string) error {
	rows, err := db.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	exists := false
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, pk int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		if name == column {
			exists = true
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if !exists {
		return nil
	}
	_, err = db.ExecContext(ctx, `ALTER TABLE `+table+` DROP COLUMN `+column)
	return err
}

func ensureColumns(ctx context.Context, db *sql.DB, table string, columns map[string]string) error {
	rows, err := db.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
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
		if _, err := db.ExecContext(ctx, `ALTER TABLE `+table+` ADD COLUMN `+name+` `+definition); err != nil {
			return err
		}
	}
	return nil
}
