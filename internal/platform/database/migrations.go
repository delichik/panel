package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

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
	// 2. Per-database schema management: each library is migrated with the
	//    model list of that library only, with destructive mode enabled.
	dbs := []struct {
		db     *sql.DB
		models []any
	}{
		{s.appDB, models.AppModels()},
		{s.logDB, models.LogModels()},
		{s.metricsDB, models.MetricsModels()},
	}
	for _, pass := range dbs {
		if _, err := orm.AutoMigrateModels(ctx, pass.db, pass.models, orm.WithDestructive(true)); err != nil {
			return fmt.Errorf("auto migrate schema: %w", err)
		}
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
func appMigrationSteps() []orm.Step {
	return []orm.Step{
		{ID: "legacy_migrate_application_file_names", Run: func(ctx context.Context, tx *sql.Tx) error {
			return migrateApplicationFileNamesOn(ctx, tx)
		}},
		{ID: "legacy_migrate_reverse_proxy_configuration", Run: func(ctx context.Context, tx *sql.Tx) error {
			return migrateReverseProxyConfigurationOn(ctx, tx)
		}},
		{ID: "legacy_migrate_facility_asset_names", Run: func(ctx context.Context, tx *sql.Tx) error {
			return migrateFacilityAssetNamesOn(ctx, tx)
		}},
	}
}

// logMigrationSteps are the one-time data migrations for the log database.
func logMigrationSteps() []orm.Step {
	return []orm.Step{
		{ID: "legacy_migrate_application_lifecycle_targets", Run: func(ctx context.Context, tx *sql.Tx) error {
			return migrateApplicationLifecycleTargetsOn(ctx, tx)
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

func (s *Store) resetLegacyTaskTables(ctx context.Context) error {
	return resetLegacyTaskTablesOn(ctx, s.logDB)
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

func (s *Store) dropTaskTables(ctx context.Context) error {
	return dropTaskTablesOn(ctx, s.logDB)
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

func (s *Store) normalizeAppDefaults(ctx context.Context) error {
	return normalizeAppDefaultsOn(ctx, s.appDB)
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
	}
	for _, stmt := range statements {
		if _, err := q.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) migrateApplicationLifecycleTargets(ctx context.Context) error {
	return migrateApplicationLifecycleTargetsOn(ctx, s.logDB)
}

func migrateApplicationLifecycleTargetsOn(ctx context.Context, q migrationExecutor) error {
	if err := ensureColumnsOn(ctx, q, "application_lifecycle_targets", map[string]string{
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
		if _, err := q.ExecContext(ctx, stmt); err != nil {
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
	tx, err := s.appDB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := migrateReverseProxyConfigurationOn(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
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
	return nil
}

func (s *Store) migrateApplicationFileNames(ctx context.Context) error {
	return migrateApplicationFileNamesOn(ctx, s.appDB)
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

func (s *Store) migrateFacilityAssetNames(ctx context.Context) error {
	tx, err := s.appDB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := migrateFacilityAssetNamesOn(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
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

func databaseTableColumns(ctx context.Context, db *sql.DB, table string) (map[string]bool, error) {
	return databaseTableColumnsOn(ctx, db, table)
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

func (s *Store) ensureLogColumns(ctx context.Context, table string, columns map[string]string) error {
	return ensureColumns(ctx, s.logDB, table, columns)
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
