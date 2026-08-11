package orm

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
)

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func containsSubstr(list []string, sub string) bool {
	for _, x := range list {
		if strings.Contains(x, sub) {
			return true
		}
	}
	return false
}

// --- scenario model types -------------------------------------------------

type migBasic struct {
	ID   string `orm:"primary_key"`
	Name string `orm:"not_null"`
}

func (migBasic) TableName() string { return "mig_basic" }

type migTakeover struct {
	ID   string `orm:"primary_key"`
	Name string `orm:"not_null"`
}

func (migTakeover) TableName() string { return "mig_takeover" }

type migAddV1 struct {
	ID   string `orm:"primary_key"`
	Name string `orm:"not_null"`
}

func (migAddV1) TableName() string { return "mig_add" }

type migAddV2 struct {
	ID   string `orm:"primary_key"`
	Name string `orm:"not_null"`
	Age  int    `orm:"default:0"`
	Note string
}

func (migAddV2) TableName() string { return "mig_add" }

type migNNV1 struct {
	ID string `orm:"primary_key"`
}

func (migNNV1) TableName() string { return "mig_nn" }

type migNNV2 struct {
	ID   string `orm:"primary_key"`
	Code string `orm:"not_null"`
}

func (migNNV2) TableName() string { return "mig_nn" }

type migRebuildV1 struct {
	ID   string `orm:"primary_key"`
	Name string `orm:"not_null"`
}

func (migRebuildV1) TableName() string { return "mig_rebuild" }

type migRebuildV2 struct {
	ID   string `orm:"primary_key"`
	Name string `orm:"not_null"`
	Code string `orm:"unique"`
}

func (migRebuildV2) TableName() string { return "mig_rebuild" }

type migRebuildTxV1 struct {
	ID   string `orm:"primary_key"`
	Name string `orm:"not_null"`
}

func (migRebuildTxV1) TableName() string { return "mig_rebuild_tx" }

type migRebuildTxV2 struct {
	ID   string `orm:"primary_key"`
	Name string `orm:"not_null"`
	Code string `orm:"unique"`
}

func (migRebuildTxV2) TableName() string { return "mig_rebuild_tx" }

type migDropV1 struct {
	ID   string `orm:"primary_key"`
	Name string `orm:"not_null"`
	Age  int
}

func (migDropV1) TableName() string { return "mig_drop" }

type migDropV2 struct {
	ID   string `orm:"primary_key"`
	Name string `orm:"not_null"`
}

func (migDropV2) TableName() string { return "mig_drop" }

type migDropIdxV1 struct {
	ID  string `orm:"primary_key"`
	Age int    `orm:"index"`
}

func (migDropIdxV1) TableName() string { return "mig_drop_idx" }

type migDropIdxV2 struct {
	ID string `orm:"primary_key"`
}

func (migDropIdxV2) TableName() string { return "mig_drop_idx" }

type migDropPkV1 struct {
	ID   string `orm:"primary_key"`
	Name string `orm:"not_null"`
	Code string `orm:"primary_key"`
}

func (migDropPkV1) TableName() string { return "mig_drop_pk" }

type migDropPkV2 struct {
	ID   string `orm:"primary_key"`
	Name string `orm:"not_null"`
}

func (migDropPkV2) TableName() string { return "mig_drop_pk" }

type migIdxAddV1 struct {
	ID   string `orm:"primary_key"`
	Name string
}

func (migIdxAddV1) TableName() string { return "mig_idx_add" }

type migIdxAddV2 struct {
	ID   string `orm:"primary_key"`
	Name string `orm:"index"`
}

func (migIdxAddV2) TableName() string { return "mig_idx_add" }

type migIdxDropV1 struct {
	ID   string `orm:"primary_key"`
	Name string `orm:"index"`
}

func (migIdxDropV1) TableName() string { return "mig_idx_drop" }

type migIdxDropV2 struct {
	ID   string `orm:"primary_key"`
	Name string
}

func (migIdxDropV2) TableName() string { return "mig_idx_drop" }

type migFkParent struct {
	ID string `orm:"primary_key"`
}

func (migFkParent) TableName() string { return "mig_fk_parent" }

type migFkChild struct {
	ID       string `orm:"primary_key"`
	ParentID string `orm:"references:mig_fk_parent(id)"`
}

func (migFkChild) TableName() string { return "mig_fk_child" }

type migExtV1 struct {
	ID   string `orm:"primary_key"`
	Name string `orm:"not_null"`
}

func (migExtV1) TableName() string { return "mig_ext" }

type migExtV2 struct {
	ID   string `orm:"primary_key"`
	Name string `orm:"not_null"`
	Code string `orm:"unique"`
}

func (migExtV2) TableName() string { return "mig_ext" }

// --- helpers ---------------------------------------------------------------

func runMigrate(t *testing.T, db *sql.DB, destructive bool) *DriftReport {
	t.Helper()
	opts := []Option{WithLogger(func(format string, args ...any) { t.Logf(format, args...) })}
	if destructive {
		opts = append(opts, WithDestructive(true))
	}
	report, err := AutoMigrate(context.Background(), db, opts...)
	if err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	return report
}

func seedManaged(t *testing.T, db *sql.DB, table, ddl string, cols []snapshotColumn, idx []snapshotIndex, fks []snapshotFK) {
	t.Helper()
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS orm_meta (table_name TEXT PRIMARY KEY, schema TEXT NOT NULL, synced_at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ddl); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(snapshot{Columns: cols, Indexes: idx, FKs: fks})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO orm_meta (table_name, schema, synced_at) VALUES (?, ?, ?)`, table, string(raw), "2026-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
}

func simpleCols(name, typ string, pk bool, notNull bool) []snapshotColumn {
	cols := []snapshotColumn{{Name: name, Type: typ, NotNull: notNull}}
	if pk {
		cols[0].PK = 1
	}
	return cols
}

func tableColumnNames(t *testing.T, db *sql.DB, table string) []string {
	t.Helper()
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var cid, notnull, pk int
		var name, typ string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	return names
}

func tableExists(t *testing.T, db *sql.DB, table string) bool {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n > 0
}

func indexNames(t *testing.T, db *sql.DB, table string) []string {
	t.Helper()
	rows, err := db.Query(`PRAGMA index_list(` + table + `)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var seq, unique, partial int
		var name, origin string
		if err := rows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
			t.Fatal(err)
		}
		out = append(out, name)
	}
	return out
}

// --- tests -----------------------------------------------------------------

func TestMigrateCreateAndIdempotent(t *testing.T) {
	db := openTestDB(t)
	if err := Register(&migBasic{}); err != nil {
		t.Fatal(err)
	}
	r := runMigrate(t, db, true)
	if !contains(r.AddedTables, "mig_basic") {
		t.Fatalf("AddedTables = %v", r.AddedTables)
	}
	if !tableExists(t, db, "mig_basic") {
		t.Fatal("table not created")
	}
	cols := tableColumnNames(t, db, "mig_basic")
	if len(cols) != 2 || cols[0] != "id" || cols[1] != "name" {
		t.Fatalf("columns = %v", cols)
	}
	r2 := runMigrate(t, db, true)
	if len(r2.AddedTables) != 0 || len(r2.AddedColumns) != 0 || len(r2.DroppedColumns) != 0 || len(r2.DroppedTables) != 0 || len(r2.RebuiltTables) != 0 {
		t.Fatalf("second run not idempotent: %+v", r2)
	}
}

func TestMigrateFirstTakeoverNoDelete(t *testing.T) {
	db := openTestDB(t)
	if _, err := db.Exec(`CREATE TABLE mig_takeover (id TEXT PRIMARY KEY, name TEXT NOT NULL, retired_col TEXT)`); err != nil {
		t.Fatal(err)
	}
	if err := Register(&migTakeover{}); err != nil {
		t.Fatal(err)
	}
	r := runMigrate(t, db, true)
	if len(r.DroppedColumns) != 0 || len(r.DroppedTables) != 0 {
		t.Fatalf("first takeover should not delete: %+v", r)
	}
	if !containsSubstr(r.Pending, "retired_col") {
		t.Fatalf("Pending should mention retired_col: %v", r.Pending)
	}
	// second run (snapshot exists) drops the retired column
	r2 := runMigrate(t, db, true)
	if !contains(r2.DroppedColumns, "mig_takeover.retired_col") {
		t.Fatalf("DroppedColumns = %v", r2.DroppedColumns)
	}
	if contains(tableColumnNames(t, db, "mig_takeover"), "retired_col") {
		t.Fatal("retired_col still present")
	}
}

func TestMigrateAddColumns(t *testing.T) {
	db := openTestDB(t)
	if err := Register(&migAddV1{}); err != nil {
		t.Fatal(err)
	}
	runMigrate(t, db, true)
	if _, err := db.Exec(`INSERT INTO mig_add (id, name) VALUES (?, ?)`, "r1", "keep"); err != nil {
		t.Fatal(err)
	}
	if err := Register(&migAddV2{}); err != nil {
		t.Fatal(err)
	}
	r := runMigrate(t, db, true)
	if !contains(r.AddedColumns, "mig_add.age") || !contains(r.AddedColumns, "mig_add.note") {
		t.Fatalf("AddedColumns = %v", r.AddedColumns)
	}
	cols := tableColumnNames(t, db, "mig_add")
	if !contains(cols, "age") || !contains(cols, "note") {
		t.Fatalf("columns = %v", cols)
	}
	var name string
	if err := db.QueryRow(`SELECT name FROM mig_add WHERE id = ?`, "r1").Scan(&name); err != nil {
		t.Fatal(err)
	}
	if name != "keep" {
		t.Fatalf("data lost after add: %q", name)
	}
}

func TestMigrateNotNullWithoutDefaultBecomesNullable(t *testing.T) {
	db := openTestDB(t)
	if err := Register(&migNNV1{}); err != nil {
		t.Fatal(err)
	}
	runMigrate(t, db, true)
	if err := Register(&migNNV2{}); err != nil {
		t.Fatal(err)
	}
	r := runMigrate(t, db, true)
	if !contains(r.AddedColumns, "mig_nn.code") {
		t.Fatalf("AddedColumns = %v", r.AddedColumns)
	}
	if !containsSubstr(r.Pending, "NOT NULL") {
		t.Fatalf("Pending = %v", r.Pending)
	}
	var notnull int
	if err := db.QueryRow(`SELECT "notnull" FROM pragma_table_info('mig_nn') WHERE name = 'code'`).Scan(&notnull); err != nil {
		t.Fatal(err)
	}
	if notnull != 0 {
		t.Fatalf("code column should be nullable, notnull = %d", notnull)
	}
}

func TestMigrateRebuildForUniqueColumn(t *testing.T) {
	db := openTestDB(t)
	if err := Register(&migRebuildV1{}); err != nil {
		t.Fatal(err)
	}
	runMigrate(t, db, true)
	if _, err := db.Exec(`INSERT INTO mig_rebuild (id, name) VALUES (?, ?)`, "r1", "keep"); err != nil {
		t.Fatal(err)
	}
	if err := Register(&migRebuildV2{}); err != nil {
		t.Fatal(err)
	}
	r := runMigrate(t, db, true)
	if !contains(r.RebuiltTables, "mig_rebuild") {
		t.Fatalf("RebuiltTables = %v", r.RebuiltTables)
	}
	if !contains(r.AddedColumns, "mig_rebuild.code") {
		t.Fatalf("AddedColumns = %v", r.AddedColumns)
	}
	var name string
	if err := db.QueryRow(`SELECT name FROM mig_rebuild WHERE id = ?`, "r1").Scan(&name); err != nil {
		t.Fatal(err)
	}
	if name != "keep" {
		t.Fatalf("data lost after rebuild: %q", name)
	}
	// unique constraint present via autoindex
	found := false
	for _, idx := range indexNames(t, db, "mig_rebuild") {
		if strings.HasPrefix(idx, "sqlite_autoindex_mig_rebuild") {
			found = true
		}
	}
	if !found {
		t.Fatal("unique autoindex missing after rebuild")
	}
}

func TestMigrateRebuildDropsStaleTempTableAndCommits(t *testing.T) {
	db := openTestDB(t)
	if err := Register(&migRebuildTxV1{}); err != nil {
		t.Fatal(err)
	}
	runMigrate(t, db, true)
	if _, err := db.Exec(`INSERT INTO mig_rebuild_tx (id, name) VALUES (?, ?)`, "r1", "keep"); err != nil {
		t.Fatal(err)
	}
	// Simulate a crashed previous rebuild that left its temp table behind.
	if _, err := db.Exec(`CREATE TABLE "__orm_rebuild_mig_rebuild_tx" (id TEXT PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	if err := Register(&migRebuildTxV2{}); err != nil {
		t.Fatal(err)
	}
	r := runMigrate(t, db, true)
	if !contains(r.RebuiltTables, "mig_rebuild_tx") {
		t.Fatalf("RebuiltTables = %v", r.RebuiltTables)
	}
	if tableExists(t, db, "__orm_rebuild_mig_rebuild_tx") {
		t.Fatal("stale rebuild temp table should be dropped")
	}
	var name string
	if err := db.QueryRow(`SELECT name FROM mig_rebuild_tx WHERE id = ?`, "r1").Scan(&name); err != nil {
		t.Fatal(err)
	}
	if name != "keep" {
		t.Fatalf("data lost after rebuild: %q", name)
	}
}
func TestMigrateDropColumn(t *testing.T) {
	db := openTestDB(t)
	if err := Register(&migDropV1{}); err != nil {
		t.Fatal(err)
	}
	runMigrate(t, db, true)
	if _, err := db.Exec(`INSERT INTO mig_drop (id, name, age) VALUES (?, ?, ?)`, "r1", "keep", 7); err != nil {
		t.Fatal(err)
	}
	if err := Register(&migDropV2{}); err != nil {
		t.Fatal(err)
	}
	// non-destructive first
	r := runMigrate(t, db, false)
	if !contains(r.SkippedDestructive, "column mig_drop.age") {
		t.Fatalf("SkippedDestructive = %v", r.SkippedDestructive)
	}
	if contains(tableColumnNames(t, db, "mig_drop"), "age") == false {
		t.Fatal("age should still exist in non-destructive mode")
	}
	var age int
	if err := db.QueryRow(`SELECT age FROM mig_drop WHERE id = ?`, "r1").Scan(&age); err != nil || age != 7 {
		t.Fatalf("age lost in non-destructive mode: %d, %v", age, err)
	}
	// destructive now
	r2 := runMigrate(t, db, true)
	if !contains(r2.DroppedColumns, "mig_drop.age") {
		t.Fatalf("DroppedColumns = %v", r2.DroppedColumns)
	}
	if contains(tableColumnNames(t, db, "mig_drop"), "age") {
		t.Fatal("age still present after destructive drop")
	}
	var name string
	if err := db.QueryRow(`SELECT name FROM mig_drop WHERE id = ?`, "r1").Scan(&name); err != nil || name != "keep" {
		t.Fatalf("data lost: %q, %v", name, err)
	}
}

func TestMigrateDropIndexedColumn(t *testing.T) {
	db := openTestDB(t)
	if err := Register(&migDropIdxV1{}); err != nil {
		t.Fatal(err)
	}
	runMigrate(t, db, true)
	if !contains(indexNames(t, db, "mig_drop_idx"), "idx_mig_drop_idx_age") {
		t.Fatal("index idx_mig_drop_idx_age not created")
	}
	if err := Register(&migDropIdxV2{}); err != nil {
		t.Fatal(err)
	}
	r := runMigrate(t, db, true)
	if !contains(r.DroppedIndexes, "mig_drop_idx.idx_mig_drop_idx_age") {
		t.Fatalf("DroppedIndexes = %v", r.DroppedIndexes)
	}
	if !contains(r.DroppedColumns, "mig_drop_idx.age") {
		t.Fatalf("DroppedColumns = %v", r.DroppedColumns)
	}
	if contains(indexNames(t, db, "mig_drop_idx"), "idx_mig_drop_idx_age") {
		t.Fatal("index still present")
	}
}

func TestMigrateDropPKColumnRebuilds(t *testing.T) {
	db := openTestDB(t)
	if err := Register(&migDropPkV1{}); err != nil {
		t.Fatal(err)
	}
	runMigrate(t, db, true)
	if _, err := db.Exec(`INSERT INTO mig_drop_pk (id, name, code) VALUES (?, ?, ?)`, "r1", "keep", "c1"); err != nil {
		t.Fatal(err)
	}
	if err := Register(&migDropPkV2{}); err != nil {
		t.Fatal(err)
	}
	r := runMigrate(t, db, true)
	if !contains(r.RebuiltTables, "mig_drop_pk") {
		t.Fatalf("RebuiltTables = %v", r.RebuiltTables)
	}
	if !contains(r.DroppedColumns, "mig_drop_pk.code") {
		t.Fatalf("DroppedColumns = %v", r.DroppedColumns)
	}
	if contains(tableColumnNames(t, db, "mig_drop_pk"), "code") {
		t.Fatal("code still present")
	}
	var name string
	if err := db.QueryRow(`SELECT name FROM mig_drop_pk WHERE id = ?`, "r1").Scan(&name); err != nil || name != "keep" {
		t.Fatalf("data lost: %q, %v", name, err)
	}
}

func TestMigrateIndexAddAndDrop(t *testing.T) {
	db := openTestDB(t)
	if err := Register(&migIdxAddV1{}); err != nil {
		t.Fatal(err)
	}
	runMigrate(t, db, true)
	if contains(indexNames(t, db, "mig_idx_add"), "idx_mig_idx_add_name") {
		t.Fatal("index should not exist yet")
	}
	if err := Register(&migIdxAddV2{}); err != nil {
		t.Fatal(err)
	}
	r := runMigrate(t, db, true)
	if !contains(r.AddedIndexes, "mig_idx_add.idx_mig_idx_add_name") {
		t.Fatalf("AddedIndexes = %v", r.AddedIndexes)
	}
	if !contains(indexNames(t, db, "mig_idx_add"), "idx_mig_idx_add_name") {
		t.Fatal("index missing after sync")
	}

	if err := Register(&migIdxDropV1{}); err != nil {
		t.Fatal(err)
	}
	runMigrate(t, db, true)
	if !contains(indexNames(t, db, "mig_idx_drop"), "idx_mig_idx_drop_name") {
		t.Fatal("index idx_mig_idx_drop_name not created")
	}
	if err := Register(&migIdxDropV2{}); err != nil {
		t.Fatal(err)
	}
	r2 := runMigrate(t, db, true)
	if !contains(r2.DroppedIndexes, "mig_idx_drop.idx_mig_idx_drop_name") {
		t.Fatalf("DroppedIndexes = %v", r2.DroppedIndexes)
	}
	if contains(indexNames(t, db, "mig_idx_drop"), "idx_mig_idx_drop_name") {
		t.Fatal("index still present")
	}
}

func TestMigrateFKDeclaredAtCreationAndMismatchPending(t *testing.T) {
	db := openTestDBFK(t)
	if err := Register(&migFkParent{}); err != nil {
		t.Fatal(err)
	}
	runMigrate(t, db, true)
	if _, err := db.Exec(`CREATE TABLE mig_fk_child (
		id TEXT PRIMARY KEY,
		parent_id TEXT REFERENCES mig_fk_parent(id) ON DELETE CASCADE
	)`); err != nil {
		t.Fatal(err)
	}
	if err := Register(&migFkChild{}); err != nil {
		t.Fatal(err)
	}
	r := runMigrate(t, db, true)
	if !containsSubstr(r.Pending, "foreign key") {
		t.Fatalf("Pending should mention FK mismatch: %v", r.Pending)
	}
	r2 := runMigrate(t, db, true)
	if !containsSubstr(r2.Pending, "foreign key") {
		t.Fatalf("second run should keep FK mismatch pending: %v", r2.Pending)
	}
	if len(r2.RebuiltTables) != 0 {
		t.Fatalf("FK mismatch must not auto-rebuild: %v", r2.RebuiltTables)
	}
}

func TestMigrateRebuildPreservesExternalColumn(t *testing.T) {
	db := openTestDB(t)
	if err := Register(&migExtV1{}); err != nil {
		t.Fatal(err)
	}
	runMigrate(t, db, true)
	if _, err := db.Exec(`INSERT INTO mig_ext (id, name) VALUES (?, ?)`, "r1", "keep"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`ALTER TABLE mig_ext ADD COLUMN ext_col TEXT`); err != nil {
		t.Fatal(err)
	}
	if err := Register(&migExtV2{}); err != nil {
		t.Fatal(err)
	}
	r := runMigrate(t, db, true)
	if !contains(r.RebuiltTables, "mig_ext") {
		t.Fatalf("RebuiltTables = %v", r.RebuiltTables)
	}
	cols := tableColumnNames(t, db, "mig_ext")
	if !contains(cols, "ext_col") {
		t.Fatalf("external column lost during rebuild: %v", cols)
	}
	var name string
	if err := db.QueryRow(`SELECT name FROM mig_ext WHERE id = ?`, "r1").Scan(&name); err != nil || name != "keep" {
		t.Fatalf("data lost: %q, %v", name, err)
	}
	// external column stays external: never dropped by later destructive runs
	r2 := runMigrate(t, db, true)
	if !containsSubstr(r2.Pending, "ext_col") {
		t.Fatalf("external column should stay pending: %v", r2.Pending)
	}
	if len(r2.DroppedColumns) != 0 {
		t.Fatalf("external column must not be dropped: %v", r2.DroppedColumns)
	}
	if contains(tableColumnNames(t, db, "mig_ext"), "ext_col") == false {
		t.Fatal("ext_col disappeared")
	}
}

func TestMigrateDropStaleTableNonDestructive(t *testing.T) {
	db := openTestDB(t)
	seedManaged(t, db, "mig_stale", `CREATE TABLE mig_stale (id TEXT PRIMARY KEY, name TEXT)`,
		simpleCols("id", "TEXT", true, true), nil, nil)
	r := runMigrate(t, db, false)
	if !contains(r.SkippedDestructive, "table mig_stale") {
		t.Fatalf("SkippedDestructive = %v", r.SkippedDestructive)
	}
	if !tableExists(t, db, "mig_stale") {
		t.Fatal("table dropped in non-destructive mode")
	}
	r2 := runMigrate(t, db, true)
	if !contains(r2.DroppedTables, "mig_stale") {
		t.Fatalf("DroppedTables = %v", r2.DroppedTables)
	}
	if tableExists(t, db, "mig_stale") {
		t.Fatal("stale table still exists")
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM orm_meta WHERE table_name = ?`, "mig_stale").Scan(&n); err != nil || n != 0 {
		t.Fatalf("orm_meta row not removed: %d, %v", n, err)
	}
}

func TestMigrateDropTablesChildFirst(t *testing.T) {
	db := openTestDB(t)
	seedManaged(t, db, "mig_p", `CREATE TABLE mig_p (id TEXT PRIMARY KEY)`, simpleCols("id", "TEXT", true, true), nil, nil)
	seedManaged(t, db, "mig_c", `CREATE TABLE mig_c (id TEXT PRIMARY KEY, pid TEXT REFERENCES mig_p(id))`,
		[]snapshotColumn{{Name: "id", Type: "TEXT", PK: 1}, {Name: "pid", Type: "TEXT"}},
		nil, []snapshotFK{{From: "pid", Table: "mig_p", To: "id"}})
	r := runMigrate(t, db, true)
	if !contains(r.DroppedTables, "mig_p") || !contains(r.DroppedTables, "mig_c") {
		t.Fatalf("DroppedTables = %v", r.DroppedTables)
	}
	ci, pi := -1, -1
	for i, x := range r.DroppedTables {
		if x == "mig_c" {
			ci = i
		}
		if x == "mig_p" {
			pi = i
		}
	}
	if ci < 0 || pi < 0 || ci > pi {
		t.Fatalf("child must be dropped before parent: %v", r.DroppedTables)
	}
}

func TestMigrateDropTableCyclePending(t *testing.T) {
	db := openTestDB(t)
	seedManaged(t, db, "mig_cyc_a", `CREATE TABLE mig_cyc_a (id TEXT PRIMARY KEY, b_id TEXT REFERENCES mig_cyc_b(id))`,
		[]snapshotColumn{{Name: "id", Type: "TEXT", PK: 1}, {Name: "b_id", Type: "TEXT"}},
		nil, []snapshotFK{{From: "b_id", Table: "mig_cyc_b", To: "id"}})
	seedManaged(t, db, "mig_cyc_b", `CREATE TABLE mig_cyc_b (id TEXT PRIMARY KEY, a_id TEXT REFERENCES mig_cyc_a(id))`,
		[]snapshotColumn{{Name: "id", Type: "TEXT", PK: 1}, {Name: "a_id", Type: "TEXT"}},
		nil, []snapshotFK{{From: "a_id", Table: "mig_cyc_a", To: "id"}})
	r := runMigrate(t, db, true)
	if !containsSubstr(r.Pending, "mig_cyc_a") || !containsSubstr(r.Pending, "mig_cyc_b") {
		t.Fatalf("Pending = %v", r.Pending)
	}
	if !tableExists(t, db, "mig_cyc_a") || !tableExists(t, db, "mig_cyc_b") {
		t.Fatal("cyclic tables must not be dropped")
	}
}

func TestMigrateDropTableBlockedByUnmanagedChild(t *testing.T) {
	db := openTestDB(t)
	seedManaged(t, db, "mig_unm_p", `CREATE TABLE mig_unm_p (id TEXT PRIMARY KEY)`, simpleCols("id", "TEXT", true, true), nil, nil)
	if _, err := db.Exec(`CREATE TABLE mig_unm_child (id TEXT PRIMARY KEY, pid TEXT REFERENCES mig_unm_p(id))`); err != nil {
		t.Fatal(err)
	}
	r := runMigrate(t, db, true)
	if !containsSubstr(r.Pending, "mig_unm_p") {
		t.Fatalf("Pending = %v", r.Pending)
	}
	if !tableExists(t, db, "mig_unm_p") {
		t.Fatal("parent dropped despite unmanaged child reference")
	}
}

func TestMigrateDriftReportFields(t *testing.T) {
	db := openTestDB(t)
	if err := Register(&migBasic{}); err != nil {
		t.Fatal(err)
	}
	r := runMigrate(t, db, false)
	if !contains(r.AddedTables, "mig_basic") {
		t.Fatalf("AddedTables = %v", r.AddedTables)
	}

}
