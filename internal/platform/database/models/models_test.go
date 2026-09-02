package models_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"panel/internal/platform/config"
	"panel/internal/platform/database"
	"panel/internal/platform/database/models"
	"panel/internal/platform/database/orm"
)

// 各库存量表清单，与 migrations.go 中 app/task/metrics 三段一一对应。
var appTables = []string{
	"credentials", "servers", "package_updates", "package_refreshes",
	"fail2ban_configs", "image_updates", "image_refreshes", "application_reconcile_states",
	"container_observations", "docker_resource_snapshots", "dns_record_snapshots",
	"applications", "application_revisions", "jobs", "application_edit_sessions", "application_edit_session_files",
	"application_edit_session_operations", "application_files", "application_instances",
	"facility_app_configs", "facility_static_assets", "reverse_proxy_routes", "facility_edit_sessions",
	"facility_edit_session_assets", "facility_edit_session_operations", "storage_share_configs",
	"storage_share_partitions", "dns_domains",
	"certificates", "self_signed_certificates", "key_assets", "overview_card_configurations",
	"runtime_settings", "auth_state", "auth_accounts",
}

var logTables = []string{
	"tasks", "task_steps", "task_logs", "application_revisions",
	"runtime_events", "runtime_event_details", "key_asset_exports",
}

var coordTables = []string{}

var metricsTables = []string{"metrics_snapshots"}

func openMigratedStore(t *testing.T) *database.Store {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.CoordinationDatabase = filepath.Join(dir, "coordination.db")
	cfg.LogDatabase = filepath.Join(dir, "log.db")
	cfg.CoordinationDatabase = filepath.Join(dir, "coordination.db")
	store, err := database.Open(cfg)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		store.Close()
		t.Fatalf("store migrate: %v", err)
	}
	return store
}

func registerAllModels(t *testing.T) {
	t.Helper()
	if err := orm.Register(models.AllModels()...); err != nil {
		t.Fatalf("orm.Register: %v", err)
	}
}

func sortedStrings(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func assertEmpty(t *testing.T, dbName, field string, got []string) {
	t.Helper()
	if len(got) != 0 {
		t.Fatalf("%s: %s 应为空，实际: %v", dbName, field, got)
	}
}

func listTables(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' AND name NOT IN ('orm_meta','orm_migrations') ORDER BY name`)
	if err != nil {
		t.Fatalf("list tables: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan table name: %v", err)
		}
		out = append(out, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("list tables rows: %v", err)
	}
	return out
}

func indexColumns(t *testing.T, db *sql.DB, index string) []string {
	t.Helper()
	rows, err := db.Query(`PRAGMA index_info("` + strings.ReplaceAll(index, `"`, `""`) + `")`)
	if err != nil {
		t.Fatalf("index_info(%s): %v", index, err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var seqno, cid int
		var col sql.NullString
		if err := rows.Scan(&seqno, &cid, &col); err != nil {
			t.Fatalf("scan index_info(%s): %v", index, err)
		}
		if col.Valid {
			out = append(out, col.String)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("index_info(%s) rows: %v", index, err)
	}
	return out
}

type inexpressibleIndex struct {
	table   string
	name    string
	origin  string
	cols    []string
	unique  bool
	partial bool
}

// discoverInexpressibleIndexes 枚举 tables 中无法用 orm tag 表达的索引：
// 复合索引（列数 > 1）与部分索引（partial=1）。这些索引在接管时必然进入
// DriftReport.Pending，属于“模型无法表达”而非“模型遗漏”。
func discoverInexpressibleIndexes(t *testing.T, db *sql.DB, tables []string) []inexpressibleIndex {
	t.Helper()
	var out []inexpressibleIndex
	for _, table := range tables {
		rows, err := db.Query(`PRAGMA index_list("` + strings.ReplaceAll(table, `"`, `""`) + `")`)
		if err != nil {
			t.Fatalf("index_list(%s): %v", table, err)
		}
		for rows.Next() {
			var seq, unique, partial int
			var name, origin string
			if err := rows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
				rows.Close()
				t.Fatalf("scan index_list(%s): %v", table, err)
			}
			if origin == "pk" {
				continue
			}
			cols := indexColumns(t, db, name)
			if len(cols) > 1 || partial != 0 {
				out = append(out, inexpressibleIndex{table: table, name: name, origin: origin, cols: cols, unique: unique != 0, partial: partial != 0})
			}
		}
		if err := rows.Close(); err != nil {
			t.Fatalf("close index_list(%s): %v", table, err)
		}
	}
	return out
}

// TestTakeoverAllLegacyTables 验证：对全新库跑完 Store.Migrate（模型驱动的
// AutoMigrateModels + ExtraIndexDDL + Steps）后，用全部模型做非破坏 AutoMigrate
// 接管。CHECK 约束由 TableConstraints() 表达、复合/部分索引由 ExtraIndexDDL()
// 表达，因此两轮（接管 + 幂等同步）的 Pending / SkippedDestructive 都应为空。
func TestTakeoverAllLegacyTables(t *testing.T) {
	store := openMigratedStore(t)
	defer store.Close()
	registerAllModels(t)

	ctx := context.Background()
	cases := []struct {
		name   string
		db     *sql.DB
		tables []string
	}{
		{"app", store.AppDB(), appTables},
		{"log", store.LogDB(), logTables},
		{"coord", store.CoordDB(), coordTables},
		{"metrics", store.MetricsDB(), metricsTables},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// 接管前：库内表集合必须与 migrations.go 存量表完全一致。
			if got := sortedStrings(listTables(t, c.db)); !equalStrings(got, sortedStrings(c.tables)) {
				t.Fatalf("%s: 存量表集合不一致，got %v want %v", c.name, got, c.tables)
			}
			report, err := orm.AutoMigrate(ctx, c.db, orm.WithDestructive(false))
			if err != nil {
				t.Fatalf("%s: AutoMigrate: %v", c.name, err)
			}
			// 全局注册表包含三库全部模型，非本库模型会在本库被新建（ORM 无按库过滤），
			// 因此 AddedTables 必须恰好等于“全局模型表 - 本库存量表”。
			wantAdded := sortedStrings(allModelTables())
			wantAdded = subtractStrings(wantAdded, sortedStrings(c.tables))
			if got := sortedStrings(report.AddedTables); !equalStrings(got, wantAdded) {
				t.Fatalf("%s: AddedTables = %v, want %v", c.name, got, wantAdded)
			}
			assertEmpty(t, c.name, "DroppedTables", report.DroppedTables)
			assertEmpty(t, c.name, "AddedColumns", report.AddedColumns)
			assertEmpty(t, c.name, "DroppedColumns", report.DroppedColumns)
			assertEmpty(t, c.name, "AddedIndexes", report.AddedIndexes)
			assertEmpty(t, c.name, "DroppedIndexes", report.DroppedIndexes)
			assertEmpty(t, c.name, "RebuiltTables", report.RebuiltTables)
			assertEmpty(t, c.name, "SkippedDestructive", report.SkippedDestructive)
			assertEmpty(t, c.name, "Pending", report.Pending)

			// 第二轮：幂等，必须保持零差异。
			report2, err := orm.AutoMigrate(ctx, c.db, orm.WithDestructive(false))
			if err != nil {
				t.Fatalf("%s: AutoMigrate(2nd): %v", c.name, err)
			}
			assertEmpty(t, c.name, "2nd AddedTables", report2.AddedTables)
			assertEmpty(t, c.name, "2nd DroppedTables", report2.DroppedTables)
			assertEmpty(t, c.name, "2nd AddedColumns", report2.AddedColumns)
			assertEmpty(t, c.name, "2nd DroppedColumns", report2.DroppedColumns)
			assertEmpty(t, c.name, "2nd AddedIndexes", report2.AddedIndexes)
			assertEmpty(t, c.name, "2nd DroppedIndexes", report2.DroppedIndexes)
			assertEmpty(t, c.name, "2nd RebuiltTables", report2.RebuiltTables)
			assertEmpty(t, c.name, "2nd Pending", report2.Pending)
			assertEmpty(t, c.name, "2nd SkippedDestructive", report2.SkippedDestructive)

			// 全部模型（含跨库新建的表）都已在 orm_meta 留痕。
			var metaCount int
			if err := c.db.QueryRow(`SELECT COUNT(*) FROM orm_meta`).Scan(&metaCount); err != nil {
				t.Fatalf("%s: count orm_meta: %v", c.name, err)
			}
			if metaCount != len(allModelTables()) {
				t.Fatalf("%s: orm_meta 行数 = %d, want %d", c.name, metaCount, len(allModelTables()))
			}
		})
	}
}

// TestExtraIndexDDLCoversInexpressibleIndexes 验证 ExtraIndexDDL 注册表完整覆盖
// 全新 schema 中所有模型 tag 无法表达的索引（复合/部分/复合 UNIQUE），确保这些
// 索引在接管与同步时全部被 ORM 视为“已声明”，不会进入 Pending。
// declaredExtraIndex 是 ExtraIndexDDL 注册表中一条 DDL 的可比较形状
// （表名、列集合、唯一性、是否部分索引）。旧库内联 UNIQUE 约束会被 SQLite
// 暴露为 sqlite_autoindex_* 索引，名字与声明无关，因此按形状匹配。
type declaredExtraIndex struct {
	table   string
	cols    []string
	unique  bool
	partial bool
}

func parseDeclaredExtraIndex(table, ddl string) declaredExtraIndex {
	upper := strings.ToUpper(ddl)
	open := strings.Index(ddl, "(")
	rel := strings.Index(ddl[open:], ")")
	cols := []string{}
	for _, part := range strings.Split(ddl[open+1:open+rel], ",") {
		if col := strings.TrimSpace(part); col != "" {
			cols = append(cols, col)
		}
	}
	return declaredExtraIndex{table: table, cols: cols, unique: strings.Contains(upper, " UNIQUE "), partial: strings.Contains(upper, " WHERE ")}
}

func TestExtraIndexDDLCoversInexpressibleIndexes(t *testing.T) {
	store := openMigratedStore(t)
	defer store.Close()
	registry := models.ExtraIndexDDLFor(models.AllModels())
	var declared []declaredExtraIndex
	for table, ddlList := range registry {
		for _, ddl := range ddlList {
			declared = append(declared, parseDeclaredExtraIndex(table, ddl))
		}
	}
	cases := []struct {
		name   string
		db     *sql.DB
		tables []string
	}{
		{"app", store.AppDB(), appTables},
		{"log", store.LogDB(), logTables},
		{"coord", store.CoordDB(), coordTables},
		{"metrics", store.MetricsDB(), metricsTables},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for _, idx := range discoverInexpressibleIndexes(t, c.db, c.tables) {
				found := false
				for _, d := range declared {
					if d.table == idx.table && d.unique == idx.unique && d.partial == idx.partial &&
						equalStrings(sortedStrings(d.cols), sortedStrings(idx.cols)) {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("%s.%s (%s) 未在 ExtraIndexDDL 注册表中声明", idx.table, idx.name, strings.Join(idx.cols, ","))
				}
			}
		})
	}
}

func allModelTables() []string {
	models := models.AllModels()
	out := make([]string, 0, len(models))
	seen := make(map[string]struct{}, len(models))
	for _, m := range models {
		table := m.(interface{ TableName() string }).TableName()
		if _, ok := seen[table]; ok {
			continue
		}
		seen[table] = struct{}{}
		out = append(out, table)
	}
	return out
}

func subtractStrings(base, remove []string) []string {
	rm := map[string]bool{}
	for _, s := range remove {
		rm[s] = true
	}
	var out []string
	for _, s := range base {
		if !rm[s] {
			out = append(out, s)
		}
	}
	return out
}

// TestModelDDLMatchesLegacyColumns 在全新空库上由模型建出全部存量表，
// 逐列对比（cid/name/type/notnull/dflt/pk）与 Store.Migrate 产生的存量 schema 是否一致，
// 用于校验模型对 DDL 的精确复刻（类型、默认值、非空、主键）。
func TestModelDDLMatchesLegacyColumns(t *testing.T) {
	store := openMigratedStore(t)
	defer store.Close()
	registerAllModels(t)

	dir := t.TempDir()
	modelDB, err := sql.Open("sqlite", "file:"+filepath.ToSlash(filepath.Join(dir, "models.db")))
	if err != nil {
		t.Fatalf("open model db: %v", err)
	}
	defer modelDB.Close()
	if _, err := orm.AutoMigrate(context.Background(), modelDB, orm.WithDestructive(false)); err != nil {
		t.Fatalf("AutoMigrate empty db: %v", err)
	}

	type col struct {
		name, typ, dflt string
		notNull, pk     int
		hasDflt         bool
	}
	tableInfo := func(db *sql.DB, table string) []col {
		t.Helper()
		rows, err := db.Query(`PRAGMA table_info("` + strings.ReplaceAll(table, `"`, `""`) + `")`)
		if err != nil {
			t.Fatalf("table_info(%s): %v", table, err)
		}
		defer rows.Close()
		var out []col
		for rows.Next() {
			var cid, notNull, pk int
			var name, typ string
			var dflt sql.NullString
			if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
				t.Fatalf("scan table_info(%s): %v", table, err)
			}
			out = append(out, col{name: name, typ: typ, notNull: notNull, pk: pk, dflt: dflt.String, hasDflt: dflt.Valid})
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("table_info(%s) rows: %v", table, err)
		}
		return out
	}

	tableDB := map[string]*sql.DB{}
	for _, tb := range appTables {
		tableDB[tb] = store.AppDB()
	}
	for _, tb := range logTables {
		tableDB[tb] = store.LogDB()
	}
	for _, tb := range coordTables {
		tableDB[tb] = store.CoordDB()
	}
	for _, tb := range metricsTables {
		tableDB[tb] = store.MetricsDB()
	}
	for _, table := range sortedStrings(allModelTables()) {
		legacy := tableInfo(tableDB[table], table)
		model := tableInfo(modelDB, table)
		if len(legacy) != len(model) {
			t.Fatalf("%s: 列数不一致 legacy=%d model=%d", table, len(legacy), len(model))
		}
		for i := range legacy {
			l, m := legacy[i], model[i]
			// 单列 TEXT PRIMARY KEY：存量 DDL 不写显式 NOT NULL（SQLite 报告 notnull=0），
			// ORM 建表恒为 NOT NULL PRIMARY KEY（实现细节，语义等价），比较时归一化。
			notNullOK := l.notNull == m.notNull || (l.pk > 0 && m.pk > 0)
			if l.name != m.name || l.typ != m.typ || !notNullOK || l.pk != m.pk || l.hasDflt != m.hasDflt || l.dflt != m.dflt {
				t.Fatalf("%s 第 %d 列不一致: legacy={name:%s type:%s notnull:%d pk:%d dflt:%q} model={name:%s type:%s notnull:%d pk:%d dflt:%q}",
					table, i, l.name, l.typ, l.notNull, l.pk, l.dflt, m.name, m.typ, m.notNull, m.pk, m.dflt)
			}
		}
	}
}
