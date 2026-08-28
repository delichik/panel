package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"panel/internal/platform/database/models"
	"panel/internal/platform/database/orm"
)

func (s *Store) Migrate(ctx context.Context) error {
	// 1. Drop legacy task tables before the ORM takes over the log DB: their
	//    schema predates the task state machine and cannot be upgraded in
	//    place.
	if err := orm.RunSteps(ctx, s.logDB, []orm.Step{legacyResetTaskTablesStep}); err != nil {
		return fmt.Errorf("reset legacy task tables: %w", err)
	}
	// 1.5. AppDB pre-destructive migration: reverse proxy routes must be
	//      backfilled before the destructive schema sync drops the legacy
	//      applications.reverse_proxy_json / facility_app_configs.domains_json
	//      columns that the unified reverse_proxy_routes table replaces.
	if err := orm.RunSteps(ctx, s.appDB, preAppMigrationSteps()); err != nil {
		return fmt.Errorf("pre-migrate reverse proxy routes: %w", err)
	}
	// 2. Per-database schema management: each library is migrated with the
	//    model list of that library only, with destructive mode enabled.
	dbs := []struct {
		db     *sql.DB
		models []any
	}{
		{s.appDB, models.AppModels()},
		{s.logDB, models.LogModels()},
		{s.coordDB, models.CoordinationModels()},
		{s.metricsDB, models.MetricsModels()},
	}
	for _, pass := range dbs {
		if _, err := orm.AutoMigrateModels(ctx, pass.db, pass.models, orm.WithDestructive(true)); err != nil {
			return fmt.Errorf("auto migrate schema: %w", err)
		}
	}
	// 2.5. Generic blank-time cleanup (global): every nullable time column of
	//      every registered model is normalized so '' becomes NULL. Runs on
	//      every start so legacy writers (and future regressions) can never
	//      leave rows that break scans with "orm: cannot parse time \"\"".
	for _, pass := range dbs {
		if err := orm.NormalizeBlankTimeColumns(ctx, pass.db, pass.models); err != nil {
			return fmt.Errorf("normalize blank time columns: %w", err)
		}
	}
	// application_revisions used to be written to LogDB. Copy the immutable
	// snapshots forward before new planners start creating AppDB jobs. The
	// legacy table remains readable for older diagnostics, but it is no longer
	// part of the control-plane write path.
	if err := s.migrateApplicationRevisionsToAppDB(ctx); err != nil {
		return fmt.Errorf("migrate application revisions: %w", err)
	}
	// 3. Cheap idempotent normalization that must re-run on every start so
	//    rows inserted between restarts stay normalized.
	if err := normalizeAppDefaultsOn(ctx, s.appDB); err != nil {
		return err
	}
	// 4. One-time data migrations (recorded in orm_migrations; each step
	//    keeps its original guard so it is a no-op on fresh databases).
	if err := orm.RunSteps(ctx, s.appDB, appMigrationSteps()); err != nil {
		return fmt.Errorf("app migration steps: %w", err)
	}
	if err := orm.RunSteps(ctx, s.logDB, logMigrationSteps()); err != nil {
		return fmt.Errorf("log migration steps: %w", err)
	}
	if err := orm.RunSteps(ctx, s.coordDB, coordMigrationSteps()); err != nil {
		return fmt.Errorf("coordination migration steps: %w", err)
	}
	// 5. Certificate scope constraint rebuild: it needs PRAGMA foreign_keys
	//    toggled off around the table rebuild (legacy rows can carry a
	//    dangling domain_id), which is not possible inside the RunSteps
	//    transaction. Its guard makes it a no-op once applied.
	if err := s.migrateCertificateScopeConstraint(ctx); err != nil {
		return fmt.Errorf("migrate certificate scope constraint: %w", err)
	}
	// 6. Idempotent extra index creation (composite/partial/composite UNIQUE
	//    indexes that orm tags cannot express). Runs after the steps so
	//    unique indexes only see deduplicated data.
	for _, pass := range dbs {
		if err := createExtraIndexes(ctx, pass.db, models.ExtraIndexDDLFor(pass.models)); err != nil {
			return err
		}
	}
	// 7. Second AutoMigrateModels pass (non-destructive): recreates model
	//    indexes lost by step table rebuilds and refreshes the orm_meta
	//    snapshots those rebuilds invalidated. It must not drop columns:
	//    legacy columns (e.g. dns_domains.api_token_secret) are consumed by
	//    module-level one-time migrations that run after Open, and the ORM
	//    would otherwise drop them before those migrations can read them.
	for _, pass := range dbs {
		if _, err := orm.AutoMigrateModels(ctx, pass.db, pass.models); err != nil {
			return fmt.Errorf("auto migrate refresh: %w", err)
		}
	}
	return nil
}

// legacyResetTaskTablesStep drops task history tables whose schema predates
// the task state machine. It must run before AutoMigrateModels takes over
// the log database.
var legacyResetTaskTablesStep = orm.Step{
	ID: "legacy_reset_task_tables",
	Run: func(ctx context.Context, tx *sql.Tx) error {
		return resetLegacyTaskTablesOn(ctx, tx)
	},
}

// appMigrationSteps are the one-time data migrations for the app database.
// Each keeps its original guard so it is a no-op once applied or on fresh
// databases.
// preAppMigrationSteps run before the destructive ORM schema sync so the
// legacy reverse proxy columns still exist while their data moves into the
// unified reverse_proxy_routes table.
func preAppMigrationSteps() []orm.Step {
	return []orm.Step{
		{ID: "legacy_migrate_reverse_proxy_configuration", Run: func(ctx context.Context, tx *sql.Tx) error {
			return migrateReverseProxyConfigurationOn(ctx, tx)
		}},
		{ID: "migrate_reverse_proxy_routes_table", Run: func(ctx context.Context, tx *sql.Tx) error {
			return migrateReverseProxyRoutesTableOn(ctx, tx)
		}},
	}
}
func appMigrationSteps() []orm.Step {
	return []orm.Step{
		{ID: "legacy_backfill_server_privilege_mode", Run: func(ctx context.Context, tx *sql.Tx) error {
			return backfillServerPrivilegeModeOn(ctx, tx)
		}},
		{ID: "legacy_migrate_application_file_names", Run: func(ctx context.Context, tx *sql.Tx) error {
			return migrateApplicationFileNamesOn(ctx, tx)
		}},

		{ID: "legacy_migrate_facility_asset_names", Run: func(ctx context.Context, tx *sql.Tx) error {
			return migrateFacilityAssetNamesOn(ctx, tx)
		}},
		{ID: "migrate_storage_share_servers", Run: func(ctx context.Context, tx *sql.Tx) error {
			return migrateStorageShareServersOn(ctx, tx)
		}},
		{ID: "fix_reverse_proxy_routes_facility_app_id", Run: func(ctx context.Context, tx *sql.Tx) error {
			return fixReverseProxyRoutesFacilityAppIDOn(ctx, tx)
		}},
		{ID: "purge_orphan_application_reconcile_states", Run: func(ctx context.Context, tx *sql.Tx) error {
			return purgeOrphanApplicationReconcileStatesOn(ctx, tx)
		}},
	}
}

// purgeOrphanApplicationReconcileStatesOn 删除 application_reconcile_states 中
// application_id 已不在 applications 表里的孤儿行（应用删除后残留的托管容器
// 仍可能保留旧标签）。这些行会让 SaveReportedContainers 在
// application_reconcile_states.application_id 外键上失败，回滚整笔观测事务，
// 导致该服务器全部容器观测无法落库、协调巡检按过期观测反复重建容器。
func purgeOrphanApplicationReconcileStatesOn(ctx context.Context, q migrationExecutor) error {
	_, err := q.ExecContext(ctx, `DELETE FROM application_reconcile_states
		WHERE application_id NOT IN (SELECT id FROM applications)`)
	return err
}

// backfillServerPrivilegeModeOn fixes servers that were inherited from older
// versions with sudo_passwordless=1 but privilege_mode left at 'none'. The
// mode is the source of truth for privileged operations, so these rows must
// be promoted to passwordless_sudo.
func backfillServerPrivilegeModeOn(ctx context.Context, q migrationExecutor) error {
	_, err := q.ExecContext(ctx, `UPDATE servers
		SET privilege_mode='passwordless_sudo',
			privilege_last_checked_at=COALESCE(privilege_last_checked_at, sudo_last_checked_at, last_checked_at),
			updated_at=COALESCE(NULLIF(updated_at, ''), strftime('%Y-%m-%dT%H:%M:%SZ','now'))
		WHERE sudo_passwordless=1
		  AND (privilege_mode IS NULL OR privilege_mode='' OR privilege_mode='none')`)
	return err
}

// logMigrationSteps are the one-time data migrations for the log database.
func logMigrationSteps() []orm.Step {
	return []orm.Step{}
}

// coordMigrationSteps are the one-time data migrations for the coordination
// database. The legacy lifecycle tables are dropped here; the application
// deployment control plane now lives entirely in AppDB (jobs/instances).
func coordMigrationSteps() []orm.Step {
	return []orm.Step{
		{ID: "drop_legacy_lifecycle_tables", Run: func(ctx context.Context, tx *sql.Tx) error {
			for _, table := range []string{"application_target_stages", "application_lifecycle_targets", "application_lifecycle_operations"} {
				if _, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS `+table); err != nil {
					return err
				}
			}
			return nil
		}},
	}
}

// migrationExecutor is satisfied by both *sql.DB and *sql.Tx so legacy
// migration bodies can run against a database or inside a migration step.
type migrationExecutor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// createExtraIndexes idempotently creates the composite/partial/composite
// UNIQUE indexes declared by models through ExtraIndexDDL (CREATE [UNIQUE]
// INDEX IF NOT EXISTS). It runs after the data migration steps so unique
// indexes only see deduplicated/normalized data.
func createExtraIndexes(ctx context.Context, db *sql.DB, ddlByTable map[string][]string) error {
	for table, ddlList := range ddlByTable {
		for _, ddl := range ddlList {
			if _, err := db.ExecContext(ctx, ddl); err != nil {
				return fmt.Errorf("create extra index on %s: %w", table, err)
			}
		}
	}
	return nil
}

func resetLegacyTaskTablesOn(ctx context.Context, q migrationExecutor) error {
	rows, err := q.QueryContext(ctx, `PRAGMA table_info(tasks)`)
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
			return dropTaskTablesOn(ctx, q)
		}
	}
	return nil
}

func dropTaskTablesOn(ctx context.Context, q migrationExecutor) error {
	for _, stmt := range []string{
		`DROP TABLE IF EXISTS task_logs`,
		`DROP TABLE IF EXISTS task_steps`,
		`DROP TABLE IF EXISTS tasks`,
	} {
		if _, err := q.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func normalizeAppDefaultsOn(ctx context.Context, q migrationExecutor) error {
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
		`UPDATE application_instances SET
			desired_state=CASE WHEN desired_state IN ('running','stopped','purged') THEN desired_state ELSE 'running' END,
			desired_generation=CASE WHEN desired_generation > 0 THEN desired_generation ELSE COALESCE(last_deployed_generation, 0) END,
			desired_spec_hash=COALESCE(NULLIF(desired_spec_hash, ''), json_extract(runtime_spec_json, '$.specHash'), ''),
			desired_spec_json=CASE WHEN trim(COALESCE(desired_spec_json, '')) IN ('', 'null') THEN COALESCE(NULLIF(runtime_spec_json, ''), '{}') ELSE desired_spec_json END,
			observed_state=CASE
				WHEN observed_state IN ('running','stopped','missing','failed','unknown') THEN observed_state
				WHEN status IN ('running','stopped','missing','failed','unknown') THEN status
				ELSE 'unknown' END,
			observed_container_name=COALESCE(NULLIF(observed_container_name, ''), container_name, ''),
			observed_container_id=COALESCE(NULLIF(observed_container_id, ''), container_id, ''),
			observed_generation=CASE WHEN observed_generation > 0 THEN observed_generation ELSE COALESCE(last_deployed_generation, 0) END,
			observed_spec_hash=COALESCE(NULLIF(observed_spec_hash, ''), desired_spec_hash, ''),
			observed_source=COALESCE(NULLIF(observed_source, ''), 'legacy'),
			observed_sequence=COALESCE(observed_sequence, 0),
			last_reconcile_job_id=COALESCE(last_reconcile_job_id, ''),
			last_error_code=COALESCE(last_error_code, ''),
			last_error_class=COALESCE(last_error_class, ''),
			last_error_message=COALESCE(last_error_message, ''),
			last_error_detail=COALESCE(last_error_detail, ''),
			last_error=COALESCE(last_error, ''),
			updated_at=COALESCE(NULLIF(updated_at, ''), ` + nowExpr + `)`,
	}
	for _, stmt := range statements {
		if _, err := q.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) migrateApplicationRevisionsToAppDB(ctx context.Context) error {
	if s == nil || s.appDB == nil || s.logDB == nil {
		return nil
	}
	rows, err := s.logDB.QueryContext(ctx, `SELECT id,application_id,generation,spec_hash,rendered_runtime_spec,managed_file_manifest,image_reference,resolved_image_digest,spec_yaml,job_json,created_at FROM application_revisions`)
	if err != nil {
		// A fresh/older LogDB can legitimately have no legacy revision table.
		if strings.Contains(strings.ToLower(err.Error()), "no such table") {
			return nil
		}
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, applicationID, specHash, runtimeSpec, manifest, imageReference, digest, specYAML, jobJSON, createdAt string
		var generation int
		if err := rows.Scan(&id, &applicationID, &generation, &specHash, &runtimeSpec, &manifest, &imageReference, &digest, &specYAML, &jobJSON, &createdAt); err != nil {
			return err
		}
		if _, err := s.appDB.ExecContext(ctx, `INSERT OR IGNORE INTO application_revisions(id,application_id,generation,spec_hash,rendered_runtime_spec,managed_file_manifest,image_reference,resolved_image_digest,spec_yaml,job_json,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
			id, applicationID, generation, specHash, runtimeSpec, manifest, imageReference, digest, specYAML, jobJSON, createdAt); err != nil {
			// A deleted application may still have a legacy revision row. It is
			// safe to skip that orphan because AppDB enforces the new FK.
			if strings.Contains(strings.ToLower(err.Error()), "foreign key") {
				continue
			}
			return err
		}
	}
	return rows.Err()
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

func migrateReverseProxyConfigurationOn(ctx context.Context, tx *sql.Tx) error {
	columns, err := databaseTableColumnsOn(ctx, tx, "facility_app_configs")
	if err != nil {
		return err
	}
	if !columns["image"] && !columns["static_sites_json"] && !columns["domain_policies_json"] {
		return nil
	}

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

	appColumns, err := databaseTableColumnsOn(ctx, tx, "applications")
	if err != nil {
		return err
	}
	if appColumns["reverse_proxy_json"] {
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
	return nil
}

// migrateReverseProxyRoutesTableOn moves application reverse proxy rules and
// facility reverse proxy domains into the unified reverse_proxy_routes table,
// then drops the legacy JSON columns. It is guarded per table/column so it is
// a no-op on fresh databases.
// fixReverseProxyRoutesFacilityAppIDOn repairs rows written by the first
// release of the unified routes migration, which used the underscored
// facility_reverse_proxy placeholder instead of the facility application id
// facility-reverse-proxy.
func fixReverseProxyRoutesFacilityAppIDOn(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `UPDATE reverse_proxy_routes SET app_id='facility-reverse-proxy' WHERE app_id='facility_reverse_proxy'`); err != nil {
		return err
	}
	return nil
}
func migrateReverseProxyRoutesTableOn(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS reverse_proxy_routes (
		domain TEXT PRIMARY KEY,
		app_id TEXT NOT NULL,
		origin_server_ids TEXT NOT NULL DEFAULT '[]',
		any_access_json TEXT NOT NULL DEFAULT '{}',
		target_type TEXT NOT NULL DEFAULT '',
		target_port INTEGER NOT NULL DEFAULT 0,
		target_container TEXT NOT NULL DEFAULT '',
		paths_json TEXT NOT NULL DEFAULT '[]',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_reverse_proxy_routes_app_id ON reverse_proxy_routes(app_id)`); err != nil {
		return err
	}
	if err := backfillApplicationReverseProxyRoutesOn(ctx, tx); err != nil {
		return err
	}
	return backfillFacilityReverseProxyRoutesOn(ctx, tx)
}

func backfillApplicationReverseProxyRoutesOn(ctx context.Context, tx *sql.Tx) error {
	columns, err := databaseTableColumnsOn(ctx, tx, "applications")
	if err != nil {
		return err
	}
	if !columns["reverse_proxy_json"] {
		return nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT id, reverse_proxy_json FROM applications WHERE kind <> 'facility'`)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	type routeRow struct {
		Domain     string
		AppID      string
		Origins    string
		AnyAccess  string
		TargetType string
		TargetPort int
		Paths      string
	}
	routes := []routeRow{}
	for rows.Next() {
		var appID, raw string
		if err := rows.Scan(&appID, &raw); err != nil {
			rows.Close()
			return err
		}
		var rules []map[string]any
		if err := json.Unmarshal([]byte(raw), &rules); err != nil {
			rows.Close()
			return fmt.Errorf("reverse proxy migration: application %q has invalid proxy configuration: %w", appID, err)
		}
		for _, rule := range rules {
			domain := normalizeProxyDomain(stringJSONValue(rule["domain"]))
			if domain == "" {
				continue
			}
			routes = append(routes, routeRow{
				Domain:     domain,
				AppID:      appID,
				Origins:    marshalProxyJSONList(rule["originServerIds"]),
				AnyAccess:  marshalProxyJSONObject(rule["anyAccess"]),
				TargetType: stringJSONValue(rule["targetType"]),
				TargetPort: intJSONValue(rule["targetPort"]),
				Paths:      marshalProxyJSONList(rule["paths"]),
			})
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, route := range routes {
		if _, err := tx.ExecContext(ctx, `INSERT INTO reverse_proxy_routes(domain,app_id,origin_server_ids,any_access_json,target_type,target_port,paths_json,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`,
			route.Domain, route.AppID, route.Origins, route.AnyAccess, route.TargetType, route.TargetPort, route.Paths, now, now); err != nil {
			return fmt.Errorf("reverse proxy migration: insert application route %q for %q: %w", route.Domain, route.AppID, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `ALTER TABLE applications DROP COLUMN reverse_proxy_json`); err != nil {
		return fmt.Errorf("reverse proxy migration: drop applications.reverse_proxy_json: %w", err)
	}
	return nil
}

func backfillFacilityReverseProxyRoutesOn(ctx context.Context, tx *sql.Tx) error {
	columns, err := databaseTableColumnsOn(ctx, tx, "facility_app_configs")
	if err != nil {
		return err
	}
	if !columns["domains_json"] {
		return nil
	}
	var raw string
	err = tx.QueryRowContext(ctx, `SELECT domains_json FROM facility_app_configs WHERE id=?`, "reverse_proxy").Scan(&raw)
	if err == sql.ErrNoRows {
		if _, err := tx.ExecContext(ctx, `ALTER TABLE facility_app_configs DROP COLUMN domains_json`); err != nil {
			return fmt.Errorf("reverse proxy migration: drop facility_app_configs.domains_json: %w", err)
		}
		return nil
	}
	if err != nil {
		return err
	}
	var domains []map[string]any
	if err := json.Unmarshal([]byte(raw), &domains); err != nil {
		return fmt.Errorf("reverse proxy migration: facility configuration has invalid domains: %w", err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, domain := range domains {
		name := normalizeProxyDomain(stringJSONValue(domain["domain"]))
		if name == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO reverse_proxy_routes(domain,app_id,origin_server_ids,any_access_json,target_type,target_port,paths_json,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`,
			name, "facility-reverse-proxy", marshalProxyJSONList(domain["originServerIds"]), marshalProxyJSONObject(domain["anyAccess"]), "", 0, marshalProxyJSONList(domain["paths"]), now, now); err != nil {
			return fmt.Errorf("reverse proxy migration: insert facility route %q: %w", name, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `ALTER TABLE facility_app_configs DROP COLUMN domains_json`); err != nil {
		return fmt.Errorf("reverse proxy migration: drop facility_app_configs.domains_json: %w", err)
	}
	return nil
}

func marshalProxyJSONList(value any) string {
	if value == nil {
		return "[]"
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return "[]"
	}
	return string(raw)
}

func marshalProxyJSONObject(value any) string {
	if value == nil {
		return "{}"
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func intJSONValue(value any) int {
	if number, ok := value.(float64); ok {
		return int(number)
	}
	return 0
}
func migrateApplicationFileNamesOn(ctx context.Context, q migrationExecutor) error {
	for _, table := range []string{"application_files", "application_edit_session_files"} {
		columns, err := databaseTableColumnsOn(ctx, q, table)
		if err != nil {
			return err
		}
		if columns["name"] || !columns["path"] {
			continue
		}
		if _, err := q.ExecContext(ctx, `ALTER TABLE `+table+` RENAME COLUMN path TO name`); err != nil {
			return fmt.Errorf("rename %s.path to name: %w", table, err)
		}
	}
	return nil
}

func migrateFacilityAssetNamesOn(ctx context.Context, tx *sql.Tx) error {
	if err := makeFacilityAssetNamesUnique(ctx, tx, "facility_static_assets", "id", ""); err != nil {
		return err
	}
	if err := makeFacilityAssetNamesUnique(ctx, tx, "facility_edit_session_assets", "asset_key", "session_id"); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `CREATE UNIQUE INDEX IF NOT EXISTS idx_facility_static_assets_name ON facility_static_assets(name)`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `CREATE UNIQUE INDEX IF NOT EXISTS idx_facility_edit_session_assets_name ON facility_edit_session_assets(session_id,name)`); err != nil {
		return err
	}
	return nil
}

func makeFacilityAssetNamesUnique(ctx context.Context, tx *sql.Tx, table, idColumn, scopeColumn string) error {
	query := `SELECT ` + idColumn + `,name,filename`
	if scopeColumn != "" {
		query += `,` + scopeColumn
	}
	query += ` FROM ` + table + ` ORDER BY created_at,` + idColumn
	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	type assetNameRow struct{ id, name, filename, scope string }
	items := []assetNameRow{}
	for rows.Next() {
		var item assetNameRow
		if scopeColumn == "" {
			err = rows.Scan(&item.id, &item.name, &item.filename)
		} else {
			err = rows.Scan(&item.id, &item.name, &item.filename, &item.scope)
		}
		if err != nil {
			rows.Close()
			return err
		}
		items = append(items, item)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	used := map[string]map[string]struct{}{}
	for _, item := range items {
		if used[item.scope] == nil {
			used[item.scope] = map[string]struct{}{}
		}
		base := strings.TrimSpace(item.name)
		if base == "" {
			base = strings.TrimSpace(item.filename)
		}
		if base == "" {
			base = item.id
		}
		name := base
		for suffix := 2; ; suffix++ {
			if _, exists := used[item.scope][name]; !exists {
				break
			}
			name = fmt.Sprintf("%s-%d", base, suffix)
		}
		used[item.scope][name] = struct{}{}
		if name != item.name {
			update := `UPDATE ` + table + ` SET name=? WHERE ` + idColumn + `=?`
			args := []any{name, item.id}
			if scopeColumn != "" {
				update += ` AND ` + scopeColumn + `=?`
				args = append(args, item.scope)
			}
			if _, err := tx.ExecContext(ctx, update, args...); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Store) migrateCertificateScopeConstraint(ctx context.Context) error {
	// PRAGMA foreign_keys is a per-connection setting, so pin this migration to
	// one dedicated connection; otherwise the OFF toggle and the transaction
	// could run on different pooled connections, leaving FK enforcement off or
	// leaking an ON toggle on a shared connection.
	conn, err := s.appDB.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	var createSQL string
	if err := conn.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE type='table' AND name='certificates'`).Scan(&createSQL); err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return err
	}
	if strings.Contains(createSQL, "'prefixes'") {
		return nil
	}
	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		return err
	}
	defer func() {
		_, _ = conn.ExecContext(context.Background(), `PRAGMA foreign_keys = ON`)
	}()
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := migrateCertificateScopeConstraintOn(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

func migrateCertificateScopeConstraintOn(ctx context.Context, tx *sql.Tx) error {
	var createSQL string
	if err := tx.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE type='table' AND name='certificates'`).Scan(&createSQL); err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return err
	}
	if strings.Contains(createSQL, "'prefixes'") {
		return nil
	}
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
	return nil
}

func databaseTableColumnsOn(ctx context.Context, q migrationExecutor, table string) (map[string]bool, error) {
	rows, err := q.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
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

func ensureColumns(ctx context.Context, db *sql.DB, table string, columns map[string]string) error {
	return ensureColumnsOn(ctx, db, table, columns)
}

func ensureColumnsOn(ctx context.Context, q migrationExecutor, table string, columns map[string]string) error {
	rows, err := q.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
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
		if _, err := q.ExecContext(ctx, `ALTER TABLE `+table+` ADD COLUMN `+name+` `+definition); err != nil {
			return err
		}
	}
	return nil
}

// migrateStorageShareServersOn 把存储共享设施的旧配置迁移为
// 「每台存储服务器各自根目录」的 servers_json 结构，并移除不允许同一
// 应用/节点使用多台存储服务器的旧唯一索引。
func migrateStorageShareServersOn(ctx context.Context, q migrationExecutor) error {
	if err := ensureColumnsOn(ctx, q, "storage_share_configs", map[string]string{"servers_json": "TEXT NOT NULL DEFAULT '[]'"}); err != nil {
		return err
	}
	if err := ensureColumnsOn(ctx, q, "storage_share_partitions", map[string]string{
		"target":      "TEXT NOT NULL DEFAULT ''",
		"volume_name": "TEXT NOT NULL DEFAULT ''",
	}); err != nil {
		return err
	}
	columns, err := databaseTableColumnsOn(ctx, q, "storage_share_configs")
	if err != nil {
		return err
	}
	hasServerIDs := columns["server_ids_json"]
	hasServerID := columns["server_id"]
	hasRoot := columns["root"]
	if hasServerIDs || hasServerID {
		query := `SELECT id`
		if hasServerIDs {
			query += `, server_ids_json`
		} else {
			query += `, ''`
		}
		if hasServerID {
			query += `, server_id`
		} else {
			query += `, ''`
		}
		if hasRoot {
			query += `, root`
		} else {
			query += `, ''`
		}
		query += `, servers_json FROM storage_share_configs`
		rows, err := q.QueryContext(ctx, query)
		if err != nil {
			return err
		}
		type pendingRow struct {
			id        string
			serverIDs []string
			serverID  string
			root      string
		}
		var pending []pendingRow
		for rows.Next() {
			var id, serverIDsRaw, serverID, root, serversJSON string
			if err := rows.Scan(&id, &serverIDsRaw, &serverID, &root, &serversJSON); err != nil {
				_ = rows.Close()
				return err
			}
			trimmed := strings.TrimSpace(serversJSON)
			if trimmed != "" && trimmed != "[]" && trimmed != "null" {
				continue
			}
			row := pendingRow{id: id, serverID: serverID, root: root}
			if hasServerIDs {
				_ = json.Unmarshal([]byte(serverIDsRaw), &row.serverIDs)
			}
			pending = append(pending, row)
		}
		if err := rows.Close(); err != nil {
			return err
		}
		if err := rows.Err(); err != nil {
			return err
		}
		for _, row := range pending {
			ids := row.serverIDs
			if len(ids) == 0 && row.serverID != "" {
				ids = []string{row.serverID}
			}
			if len(ids) == 0 {
				continue
			}
			entries := make([]map[string]any, 0, len(ids))
			for _, id := range ids {
				entries = append(entries, map[string]any{"serverId": id, "root": row.root})
			}
			raw, _ := json.Marshal(entries)
			if _, err := q.ExecContext(ctx, `UPDATE storage_share_configs SET servers_json=? WHERE id=?`, string(raw), row.id); err != nil {
				return err
			}
		}
	}
	if _, err := q.ExecContext(ctx, `DROP INDEX IF EXISTS uq_storage_share_partitions_application_server`); err != nil {
		return err
	}
	if _, err := q.ExecContext(ctx, `DROP INDEX IF EXISTS uq_storage_share_partitions_storage_application_server`); err != nil {
		return err
	}
	if err := backfillStorageSharePartitionServer(ctx, q); err != nil {
		return err
	}
	return nil
}

// backfillStorageSharePartitionServer 回填旧版本分区记录缺失的存储服务器，并把
// 旧路径格式 root/<node>/<app> 重写为 root/<storageServer>/<node>/<app>。
func backfillStorageSharePartitionServer(ctx context.Context, q migrationExecutor) error {
	columns, err := databaseTableColumnsOn(ctx, q, "storage_share_partitions")
	if err != nil {
		return err
	}
	if !columns["storage_server_id"] || !columns["path"] {
		return nil
	}
	configColumns, err := databaseTableColumnsOn(ctx, q, "storage_share_configs")
	if err != nil {
		return err
	}
	if !configColumns["servers_json"] {
		return nil
	}
	type configServer struct {
		ServerID string `json:"serverId"`
		Root     string `json:"root"`
	}
	rows, err := q.QueryContext(ctx, `SELECT servers_json FROM storage_share_configs WHERE id='storage-share'`)
	if err != nil {
		return err
	}
	var servers []configServer
	if rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			_ = rows.Close()
			return err
		}
		_ = json.Unmarshal([]byte(raw), &servers)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(servers) != 1 {
		// 多服务器或未配置时无法安全推断旧分区归属，跳过回填。
		return nil
	}
	serverID := servers[0].ServerID
	root := servers[0].Root
	if serverID == "" || root == "" {
		return nil
	}
	partitionRows, err := q.QueryContext(ctx, `SELECT id, path FROM storage_share_partitions WHERE storage_server_id=''`)
	if err != nil {
		return err
	}
	type partitionRow struct {
		id   string
		path string
	}
	var pending []partitionRow
	for partitionRows.Next() {
		var id, pathValue string
		if err := partitionRows.Scan(&id, &pathValue); err != nil {
			_ = partitionRows.Close()
			return err
		}
		pending = append(pending, partitionRow{id: id, path: pathValue})
	}
	if err := partitionRows.Close(); err != nil {
		return err
	}
	if err := partitionRows.Err(); err != nil {
		return err
	}
	prefix := root + "/"
	for _, row := range pending {
		if !strings.HasPrefix(row.path, prefix) {
			continue
		}
		rest := strings.TrimPrefix(row.path, prefix)
		// 旧格式 root/<node>/<app> 恰好两段；新格式已含 storageServer 段则跳过。
		if strings.Count(rest, "/") != 1 {
			continue
		}
		newPath := prefix + serverID + "/" + rest
		if _, err := q.ExecContext(ctx, `UPDATE storage_share_partitions SET storage_server_id=?, path=? WHERE id=?`, serverID, newPath, row.id); err != nil {
			return err
		}
	}
	return nil
}
