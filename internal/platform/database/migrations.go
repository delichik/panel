package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
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
		`CREATE TABLE IF NOT EXISTS panel_installation (
			id TEXT PRIMARY KEY CHECK(id = 'default'),
			host_server_id TEXT UNIQUE,
			pending_server_id TEXT UNIQUE,
			stage TEXT NOT NULL DEFAULT '',
			last_error TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			FOREIGN KEY(host_server_id) REFERENCES servers(id),
			FOREIGN KEY(pending_server_id) REFERENCES servers(id) ON DELETE SET NULL
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
			version INTEGER NOT NULL DEFAULT 1,
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
		`CREATE TABLE IF NOT EXISTS application_edit_sessions (
			id TEXT PRIMARY KEY,
			application_id TEXT NOT NULL DEFAULT '',
			owner_id TEXT NOT NULL,
			client_draft_key TEXT NOT NULL DEFAULT '',
			state TEXT NOT NULL,
			base_resource_version INTEGER NOT NULL DEFAULT 0,
			base_resource_updated_at TEXT NOT NULL DEFAULT '',
			draft_json TEXT NOT NULL DEFAULT '{}',
			revision INTEGER NOT NULL DEFAULT 1,
			preview_token TEXT NOT NULL DEFAULT '',
			preview_revision INTEGER NOT NULL DEFAULT 0,
			preview_expires_at TEXT NOT NULL DEFAULT '',
			commit_lease_owner TEXT NOT NULL DEFAULT '',
			commit_lease_expires_at TEXT NOT NULL DEFAULT '',
			commit_idempotency_key TEXT NOT NULL DEFAULT '',
			commit_application_id TEXT NOT NULL DEFAULT '',
			commit_result_json TEXT NOT NULL DEFAULT '',
			conflict_json TEXT NOT NULL DEFAULT '',
			idle_expires_at TEXT NOT NULL,
			absolute_expires_at TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			committed_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_application_edit_sessions_recoverable ON application_edit_sessions(owner_id,application_id,state,updated_at)`,
		`CREATE INDEX IF NOT EXISTS idx_application_edit_sessions_expiry ON application_edit_sessions(state,idle_expires_at,absolute_expires_at)`,
		`CREATE TABLE IF NOT EXISTS application_edit_session_files (
			session_id TEXT NOT NULL,
			file_key TEXT NOT NULL,
			path TEXT NOT NULL,
			kind TEXT NOT NULL CHECK(kind IN ('binary','template','archive')),
			content_type TEXT NOT NULL DEFAULT '',
			size INTEGER NOT NULL DEFAULT 0,
			sha256 TEXT NOT NULL DEFAULT '',
			blob_path TEXT NOT NULL,
			state TEXT NOT NULL DEFAULT 'ready',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY(session_id,file_key),
			UNIQUE(session_id,path),
			FOREIGN KEY(session_id) REFERENCES application_edit_sessions(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS application_edit_session_operations (
			session_id TEXT NOT NULL,
			client_operation_id TEXT NOT NULL,
			idempotency_key TEXT NOT NULL,
			request_hash TEXT NOT NULL,
			result_json TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			PRIMARY KEY(session_id,client_operation_id),
			UNIQUE(session_id,idempotency_key),
			FOREIGN KEY(session_id) REFERENCES application_edit_sessions(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS application_files (
			id TEXT PRIMARY KEY,
			application_id TEXT NOT NULL,
			path TEXT NOT NULL,
			kind TEXT NOT NULL CHECK(kind IN ('binary','template','archive')),
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
			version INTEGER NOT NULL DEFAULT 1,
			deployment_server_ids_json TEXT NOT NULL DEFAULT '[]',
			panel_entry_json TEXT NOT NULL DEFAULT '{}',
			domains_json TEXT NOT NULL DEFAULT '[]',
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
		`CREATE TABLE IF NOT EXISTS facility_edit_sessions (
			id TEXT PRIMARY KEY,
			owner_id TEXT NOT NULL,
			client_draft_key TEXT NOT NULL DEFAULT '',
			state TEXT NOT NULL,
			base_resource_version INTEGER NOT NULL DEFAULT 0,
			draft_json TEXT NOT NULL DEFAULT '{}',
			revision INTEGER NOT NULL DEFAULT 1,
			preview_token TEXT NOT NULL DEFAULT '',
			preview_revision INTEGER NOT NULL DEFAULT 0,
			preview_expires_at TEXT NOT NULL DEFAULT '',
			commit_lease_owner TEXT NOT NULL DEFAULT '',
			commit_lease_expires_at TEXT NOT NULL DEFAULT '',
			commit_idempotency_key TEXT NOT NULL DEFAULT '',
			commit_result_json TEXT NOT NULL DEFAULT '',
			manifest_path TEXT NOT NULL DEFAULT '',
			conflict_json TEXT NOT NULL DEFAULT '',
			idle_expires_at TEXT NOT NULL,
			absolute_expires_at TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			committed_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_facility_edit_sessions_recoverable ON facility_edit_sessions(owner_id,client_draft_key,state,updated_at)`,
		`CREATE INDEX IF NOT EXISTS idx_facility_edit_sessions_expiry ON facility_edit_sessions(state,idle_expires_at,absolute_expires_at)`,
		`CREATE TABLE IF NOT EXISTS facility_edit_session_assets (
			session_id TEXT NOT NULL,
			asset_key TEXT NOT NULL,
			source_asset_id TEXT NOT NULL DEFAULT '',
			name TEXT NOT NULL,
			kind TEXT NOT NULL CHECK(kind IN ('uploaded_file','uploaded_bundle')),
			filename TEXT NOT NULL DEFAULT '',
			size INTEGER NOT NULL DEFAULT 0,
			sha256 TEXT NOT NULL DEFAULT '',
			content_sha256 TEXT NOT NULL DEFAULT '',
			blob_dir TEXT NOT NULL DEFAULT '',
			state TEXT NOT NULL DEFAULT 'ready',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY(session_id,asset_key),
			FOREIGN KEY(session_id) REFERENCES facility_edit_sessions(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS facility_edit_session_operations (
			session_id TEXT NOT NULL,
			client_operation_id TEXT NOT NULL,
			idempotency_key TEXT NOT NULL,
			request_hash TEXT NOT NULL,
			result_json TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			PRIMARY KEY(session_id,client_operation_id),
			UNIQUE(session_id,idempotency_key),
			FOREIGN KEY(session_id) REFERENCES facility_edit_sessions(id) ON DELETE CASCADE
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
			scope TEXT NOT NULL CHECK(scope IN ('single','wildcard','prefixes')),
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
			updated_at TEXT NOT NULL
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
			FOREIGN KEY(operation_id) REFERENCES application_lifecycle_operations(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_application_lifecycle_targets_operation ON application_lifecycle_targets(operation_id, server_id)`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_type_status ON tasks(type,status)`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_concurrency_status ON tasks(concurrency_key,status)`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_parent ON tasks(parent_task_id,child_index)`,
	}
	for _, stmt := range task {
		if _, err := s.logDB.ExecContext(ctx, stmt); err != nil {
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
		"version":                    "INTEGER NOT NULL DEFAULT 1",
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
	if err := s.ensureAppColumns(ctx, "application_edit_sessions", map[string]string{
		"base_resource_updated_at": "TEXT NOT NULL DEFAULT ''",
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
	if err := s.migrateReverseProxyConfiguration(ctx); err != nil {
		return err
	}
	if err := s.ensureAppColumns(ctx, "facility_app_configs", map[string]string{
		"version":                    "INTEGER NOT NULL DEFAULT 1",
		"deployment_server_ids_json": "TEXT NOT NULL DEFAULT '[]'",
		"panel_entry_json":           "TEXT NOT NULL DEFAULT '{}'",
		"domains_json":               "TEXT NOT NULL DEFAULT '[]'",
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
	if err := s.ensureAppColumns(ctx, "facility_edit_session_assets", map[string]string{
		"content_sha256": "TEXT NOT NULL DEFAULT ''",
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
	if err := s.migrateCertificateScopeConstraint(ctx); err != nil {
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
	rows, err := s.logDB.QueryContext(ctx, `PRAGMA table_info(tasks)`)
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
		if _, err := s.logDB.ExecContext(ctx, stmt); err != nil {
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
	if err := s.ensureLogColumns(ctx, "application_lifecycle_targets", map[string]string{
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
		if _, err := s.logDB.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ensureAppColumns(ctx context.Context, table string, columns map[string]string) error {
	return ensureColumns(ctx, s.appDB, table, columns)
}

type legacyFacilityRoutePath struct {
	Domain            string         `json:"domain"`
	Path              string         `json:"path"`
	RuleType          string         `json:"ruleType,omitempty"`
	RootPath          string         `json:"rootPath,omitempty"`
	SourceType        string         `json:"sourceType"`
	AssetID           string         `json:"assetId,omitempty"`
	RedirectURL       string         `json:"redirectUrl,omitempty"`
	RedirectCode      int            `json:"redirectCode,omitempty"`
	ProxyURL          string         `json:"proxyUrl,omitempty"`
	ProxySourceMode   string         `json:"proxySourceMode,omitempty"`
	DeploymentServers []string       `json:"deploymentServers,omitempty"`
	Options           map[string]any `json:"options,omitempty"`
}

type legacyFacilityDomainPolicy struct {
	Domain          string   `json:"domain"`
	EntryServerIDs  []string `json:"entryServerIds"`
	UpstreamMode    bool     `json:"upstreamMode"`
	Strategy        string   `json:"strategy"`
	PrimaryServerID string   `json:"primaryServerId"`
}

type migratedFacilityDomain struct {
	Domain          string                    `json:"domain"`
	OriginServerIDs []string                  `json:"originServerIds"`
	AnyAccess       migratedAnyAccess         `json:"anyAccess"`
	Paths           []legacyFacilityRoutePath `json:"paths"`
}

type migratedAnyAccess struct {
	Enabled               bool   `json:"enabled"`
	Strategy              string `json:"strategy"`
	PrimaryOriginServerID string `json:"primaryOriginServerId,omitempty"`
}

func (s *Store) migrateReverseProxyConfiguration(ctx context.Context) error {
	columns, err := databaseTableColumns(ctx, s.appDB, "facility_app_configs")
	if err != nil {
		return err
	}
	if !columns["image"] && !columns["static_sites_json"] && !columns["domain_policies_json"] {
		return nil
	}
	tx, err := s.appDB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	type facilityRow struct {
		ID, ServersRaw, PanelRaw, StaticRaw, PoliciesRaw, LastError, UpdatedAt string
		Domains                                                                []migratedFacilityDomain
	}
	staticColumn := `'[]'`
	if columns["static_sites_json"] {
		staticColumn = "static_sites_json"
	}
	policiesColumn := `'[]'`
	if columns["domain_policies_json"] {
		policiesColumn = "domain_policies_json"
	}
	rows, err := tx.QueryContext(ctx, fmt.Sprintf(`SELECT id,deployment_server_ids_json,panel_entry_json,%s,%s,last_error,updated_at FROM facility_app_configs`, staticColumn, policiesColumn))
	if err != nil {
		return err
	}
	facilities := []facilityRow{}
	globalGateways := []string{}
	owners := map[string]string{}
	for rows.Next() {
		var row facilityRow
		if err := rows.Scan(&row.ID, &row.ServersRaw, &row.PanelRaw, &row.StaticRaw, &row.PoliciesRaw, &row.LastError, &row.UpdatedAt); err != nil {
			rows.Close()
			return err
		}
		var gateways []string
		_ = json.Unmarshal([]byte(row.ServersRaw), &gateways)
		gateways = uniqueSortedDatabaseStrings(gateways)
		if row.ID == "reverse_proxy" {
			globalGateways = gateways
		}
		var sites []legacyFacilityRoutePath
		var policies []legacyFacilityDomainPolicy
		_ = json.Unmarshal([]byte(row.StaticRaw), &sites)
		_ = json.Unmarshal([]byte(row.PoliciesRaw), &policies)
		policyByDomain := map[string]legacyFacilityDomainPolicy{}
		for _, policy := range policies {
			domain := normalizeProxyDomain(policy.Domain)
			if domain != "" {
				policyByDomain[domain] = policy
			}
		}
		domainIndex := map[string]int{}
		originSets := map[string]map[string]struct{}{}
		for _, site := range sites {
			domain := normalizeProxyDomain(site.Domain)
			if domain == "" {
				continue
			}
			index, exists := domainIndex[domain]
			if !exists {
				origins := uniqueSortedDatabaseStrings(policyByDomain[domain].EntryServerIDs)
				if len(origins) == 0 {
					origins = uniqueSortedDatabaseStrings(site.DeploymentServers)
				}
				if len(origins) == 0 {
					origins = append([]string(nil), gateways...)
				}
				policy := policyByDomain[domain]
				strategy := normalizeMigratedStrategy(policy.Strategy)
				primary := strings.TrimSpace(policy.PrimaryServerID)
				if !policy.UpstreamMode || strategy != "primary_backup" {
					primary = ""
				}
				row.Domains = append(row.Domains, migratedFacilityDomain{Domain: domain, OriginServerIDs: origins, AnyAccess: migratedAnyAccess{Enabled: policy.UpstreamMode, Strategy: strategy, PrimaryOriginServerID: primary}, Paths: []legacyFacilityRoutePath{}})
				index = len(row.Domains) - 1
				domainIndex[domain] = index
				originSets[domain] = stringSet(origins)
			}
			for _, id := range site.DeploymentServers {
				id = strings.TrimSpace(id)
				if id != "" && len(policyByDomain[domain].EntryServerIDs) == 0 {
					originSets[domain][id] = struct{}{}
				}
			}
			site.Domain = ""
			site.DeploymentServers = nil
			row.Domains[index].Paths = append(row.Domains[index].Paths, site)
		}
		for domain, index := range domainIndex {
			row.Domains[index].OriginServerIDs = sortedDatabaseSet(originSets[domain])
			if len(row.Domains[index].OriginServerIDs) == 0 {
				rows.Close()
				return fmt.Errorf("reverse proxy migration: facility domain %q has no valid origin server", domain)
			}
			if err := reserveProxyDomain(owners, domain, "facility route"); err != nil {
				rows.Close()
				return err
			}
		}
		var panel map[string]any
		if json.Unmarshal([]byte(row.PanelRaw), &panel) == nil && boolJSONValue(panel["enabled"]) {
			if err := reserveProxyDomain(owners, normalizeProxyDomain(stringJSONValue(panel["domain"])), "Panel entry"); err != nil {
				rows.Close()
				return err
			}
		}
		sort.Slice(row.Domains, func(i, j int) bool { return row.Domains[i].Domain < row.Domains[j].Domain })
		facilities = append(facilities, row)
	}
	if err := rows.Close(); err != nil {
		return err
	}

	appRows, err := tx.QueryContext(ctx, `SELECT id,name,deployment_mode,deployment_server_ids_json,reverse_proxy_json FROM applications WHERE kind <> 'facility'`)
	if err != nil {
		return err
	}
	type appUpdate struct{ ID, Raw string }
	updates := []appUpdate{}
	for appRows.Next() {
		var appID, appName, mode, deploymentsRaw, proxyRaw string
		if err := appRows.Scan(&appID, &appName, &mode, &deploymentsRaw, &proxyRaw); err != nil {
			appRows.Close()
			return err
		}
		var rules []map[string]any
		if err := json.Unmarshal([]byte(proxyRaw), &rules); err != nil {
			appRows.Close()
			return fmt.Errorf("reverse proxy migration: application %q has invalid proxy configuration: %w", appName, err)
		}
		var deployments []string
		_ = json.Unmarshal([]byte(deploymentsRaw), &deployments)
		validOrigins := globalGateways
		if strings.TrimSpace(mode) == "selected" {
			validOrigins = intersectDatabaseStrings(globalGateways, deployments)
		}
		merged := []map[string]any{}
		byDomain := map[string]int{}
		for _, rule := range rules {
			domain := normalizeProxyDomain(stringJSONValue(rule["domain"]))
			if domain == "" {
				continue
			}
			owner := "application " + appName
			if err := reserveProxyDomain(owners, domain, owner); err != nil {
				if existing, ok := byDomain[domain]; !ok || !strings.Contains(err.Error(), owner) {
					appRows.Close()
					return err
				} else if !sameMigratedProxyTarget(merged[existing], rule) {
					appRows.Close()
					return fmt.Errorf("reverse proxy migration: application %q has multiple targets for domain %q", appName, domain)
				}
			}
			origins := stringSliceJSONValue(rule["originServerIds"])
			if len(origins) == 0 {
				origins = append([]string(nil), validOrigins...)
			}
			origins = intersectDatabaseStrings(globalGateways, origins)
			if len(origins) == 0 {
				appRows.Close()
				return fmt.Errorf("reverse proxy migration: application %q domain %q has no valid origin server", appName, domain)
			}
			rule["domain"] = domain
			rule["originServerIds"] = origins
			if _, ok := rule["anyAccess"]; !ok {
				rule["anyAccess"] = map[string]any{"enabled": false, "strategy": "round_robin"}
			}
			delete(rule, "entryServerIds")
			delete(rule, "upstreamMode")
			delete(rule, "strategy")
			delete(rule, "primaryServerId")
			if index, exists := byDomain[domain]; exists {
				merged[index]["paths"] = append(anySliceJSONValue(merged[index]["paths"]), anySliceJSONValue(rule["paths"])...)
				continue
			}
			byDomain[domain] = len(merged)
			merged = append(merged, rule)
		}
		raw, err := json.Marshal(merged)
		if err != nil {
			appRows.Close()
			return err
		}
		updates = append(updates, appUpdate{ID: appID, Raw: string(raw)})
	}
	if err := appRows.Close(); err != nil {
		return err
	}
	for _, update := range updates {
		if _, err := tx.ExecContext(ctx, `UPDATE applications SET reverse_proxy_json=? WHERE id=?`, update.Raw, update.ID); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `CREATE TABLE facility_app_configs_new (
		id TEXT PRIMARY KEY,
		version INTEGER NOT NULL DEFAULT 1,
		deployment_server_ids_json TEXT NOT NULL DEFAULT '[]',
		panel_entry_json TEXT NOT NULL DEFAULT '{}',
		domains_json TEXT NOT NULL DEFAULT '[]',
		last_error TEXT NOT NULL DEFAULT '',
		updated_at TEXT NOT NULL
	)`); err != nil {
		return err
	}
	for _, row := range facilities {
		domainsRaw, err := json.Marshal(row.Domains)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO facility_app_configs_new(id,version,deployment_server_ids_json,panel_entry_json,domains_json,last_error,updated_at) VALUES(?,?,?,?,?,?,?)`, row.ID, 1, row.ServersRaw, row.PanelRaw, string(domainsRaw), row.LastError, row.UpdatedAt); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DROP TABLE facility_app_configs`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `ALTER TABLE facility_app_configs_new RENAME TO facility_app_configs`); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) migrateCertificateScopeConstraint(ctx context.Context) error {
	var createSQL string
	if err := s.appDB.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE type='table' AND name='certificates'`).Scan(&createSQL); err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return err
	}
	if strings.Contains(createSQL, "'prefixes'") {
		return nil
	}
	if _, err := s.appDB.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		return err
	}
	defer s.appDB.ExecContext(ctx, `PRAGMA foreign_keys = ON`)
	tx, err := s.appDB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `CREATE TABLE certificates_new (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		domain_id TEXT NOT NULL DEFAULT '',
		domain TEXT NOT NULL,
		prefix TEXT NOT NULL DEFAULT '@',
		scope TEXT NOT NULL CHECK(scope IN ('single','wildcard','prefixes')),
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
	)`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO certificates_new(id,name,domain_id,domain,prefix,scope,domains_json,variable_name,certificate_path,private_key_path,issuer,status,last_error,auto_renew,next_renew_at,not_before,not_after,created_at,updated_at)
		SELECT id,name,domain_id,domain,prefix,scope,domains_json,variable_name,certificate_path,private_key_path,issuer,status,last_error,auto_renew,next_renew_at,not_before,not_after,created_at,updated_at FROM certificates`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DROP TABLE certificates`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `ALTER TABLE certificates_new RENAME TO certificates`); err != nil {
		return err
	}
	return tx.Commit()
}

func databaseTableColumns(ctx context.Context, db *sql.DB, table string) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var cid, notNull, pk int
		var name, typ string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return nil, err
		}
		out[name] = true
	}
	return out, rows.Err()
}

func reserveProxyDomain(owners map[string]string, domain, owner string) error {
	if domain == "" {
		return nil
	}
	if existing, ok := owners[domain]; ok {
		return fmt.Errorf("reverse proxy migration: domain %q is owned by both %s and %s", domain, existing, owner)
	}
	owners[domain] = owner
	return nil
}

func normalizeProxyDomain(value string) string { return strings.ToLower(strings.TrimSpace(value)) }

func normalizeMigratedStrategy(value string) string {
	switch strings.TrimSpace(value) {
	case "primary_backup", "ip_hash":
		return strings.TrimSpace(value)
	default:
		return "round_robin"
	}
}

func uniqueSortedDatabaseStrings(values []string) []string {
	return sortedDatabaseSet(stringSet(values))
}

func stringSet(values []string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out[value] = struct{}{}
		}
	}
	return out
}

func sortedDatabaseSet(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func intersectDatabaseStrings(left, right []string) []string {
	rightSet := stringSet(right)
	out := []string{}
	for _, value := range uniqueSortedDatabaseStrings(left) {
		if _, ok := rightSet[value]; ok {
			out = append(out, value)
		}
	}
	return out
}

func stringJSONValue(value any) string  { text, _ := value.(string); return text }
func boolJSONValue(value any) bool      { flag, _ := value.(bool); return flag }
func anySliceJSONValue(value any) []any { items, _ := value.([]any); return items }
func stringSliceJSONValue(value any) []string {
	items := anySliceJSONValue(value)
	out := make([]string, 0, len(items))
	for _, item := range items {
		if text, ok := item.(string); ok {
			out = append(out, text)
		}
	}
	return out
}

func sameMigratedProxyTarget(left, right map[string]any) bool {
	return stringJSONValue(left["targetType"]) == stringJSONValue(right["targetType"]) && fmt.Sprint(left["targetPort"]) == fmt.Sprint(right["targetPort"])
}

func (s *Store) ensureLogColumns(ctx context.Context, table string, columns map[string]string) error {
	return ensureColumns(ctx, s.logDB, table, columns)
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
