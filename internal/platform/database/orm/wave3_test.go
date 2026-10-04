package orm

import (
	"context"
	"database/sql"
	"strings"
	"testing"
)

// --- Wave 3 models --------------------------------------------------------

type waveBasic struct {
	ID   string `orm:"primary_key"`
	Name string `orm:"not_null"`
}

func (waveBasic) TableName() string { return "wave_basic" }

type waveCheck struct {
	ID   string `orm:"primary_key"`
	Type string `orm:"not_null"`
}

func (waveCheck) TableName() string          { return "wave_check" }
func (waveCheck) TableConstraints() []string { return []string{"CHECK(type IN ('a','b'))"} }

type waveCheckV1 struct {
	ID string `orm:"primary_key"`
}

func (waveCheckV1) TableName() string { return "wave_check_v" }

type waveCheckV2 struct {
	ID   string `orm:"primary_key"`
	Type string `orm:"unique"`
}

func (waveCheckV2) TableName() string          { return "wave_check_v" }
func (waveCheckV2) TableConstraints() []string { return []string{"CHECK(type IN ('a','b'))"} }

type waveAddCheck struct {
	ID          string `orm:"primary_key"`
	Name        string `orm:"not_null"`
	ContentMode string `orm:"not_null;default:'binary'"`
}

func (waveAddCheck) TableName() string { return "wave_add_check" }
func (waveAddCheck) TableConstraints() []string {
	return []string{"CHECK(content_mode IN ('text','binary'))"}
}

type waveExtra struct {
	ID   string `orm:"primary_key"`
	Type string `orm:"not_null"`
	At   string `orm:"not_null"`
}

func (waveExtra) TableName() string { return "wave_extra" }
func (waveExtra) ExtraIndexDDL() map[string][]string {
	return map[string][]string{
		"wave_extra": {`CREATE INDEX IF NOT EXISTS idx_wave_extra_type_at ON wave_extra(type, at)`},
	}
}

// --- AutoMigrateModels ----------------------------------------------------

func TestAutoMigrateModelsScopesToGivenModels(t *testing.T) {
	db := openTestDB(t)
	if _, err := db.Exec(`CREATE TABLE wave_untouched (id TEXT PRIMARY KEY, name TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	report, err := AutoMigrateModels(context.Background(), db, []any{&waveBasic{}}, WithDestructive(true))
	if err != nil {
		t.Fatalf("AutoMigrateModels: %v", err)
	}
	if !contains(report.AddedTables, "wave_basic") {
		t.Fatalf("AddedTables = %v", report.AddedTables)
	}
	if len(report.DroppedTables) != 0 {
		t.Fatalf("DroppedTables = %v", report.DroppedTables)
	}
	if len(report.Pending) != 0 {
		t.Fatalf("Pending = %v", report.Pending)
	}
	if !tableExists(t, db, "wave_untouched") {
		t.Fatal("non-listed table must stay untouched")
	}
	// second destructive pass still leaves the non-listed table alone.
	report2, err := AutoMigrateModels(context.Background(), db, []any{&waveBasic{}}, WithDestructive(true))
	if err != nil {
		t.Fatalf("AutoMigrateModels(2nd): %v", err)
	}
	if len(report2.DroppedTables) != 0 || len(report2.Pending) != 0 {
		t.Fatalf("2nd pass drift: %+v", report2)
	}
	if !tableExists(t, db, "wave_untouched") {
		t.Fatal("non-listed table must survive a destructive pass")
	}
}

func TestAutoMigrateModelsIgnoresGlobalRegistry(t *testing.T) {
	db := openTestDB(t)
	if err := Register(&waveCheck{}); err != nil {
		t.Fatal(err)
	}
	report, err := AutoMigrateModels(context.Background(), db, []any{&waveBasic{}})
	if err != nil {
		t.Fatalf("AutoMigrateModels: %v", err)
	}
	if !contains(report.AddedTables, "wave_basic") {
		t.Fatalf("AddedTables = %v", report.AddedTables)
	}
	if tableExists(t, db, "wave_check") {
		t.Fatal("globally registered model must not be migrated by AutoMigrateModels")
	}
}

type waveReserved struct {
	ID string `orm:"primary_key"`
}

func (waveReserved) TableName() string { return "orm_meta" }

func TestAutoMigrateModelsRejectsReservedTableName(t *testing.T) {
	db := openTestDB(t)
	_, err := AutoMigrateModels(context.Background(), db, []any{&waveReserved{}})
	if err == nil || !strings.Contains(err.Error(), "reserved table name") {
		t.Fatalf("expected reserved table name error, got %v", err)
	}
}

// --- RunSteps -------------------------------------------------------------

func TestRunStepsPerDatabase(t *testing.T) {
	ctx := context.Background()
	db1 := openTestDB(t)
	db2 := openTestDB(t)
	marker := Step{
		ID: "wave_step_shared",
		Run: func(ctx context.Context, tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `INSERT INTO wave_step_marker (v) VALUES (1)`)
			return err
		},
	}
	for _, db := range []*sql.DB{db1, db2} {
		if _, err := db.Exec(`CREATE TABLE wave_step_marker (v INTEGER)`); err != nil {
			t.Fatal(err)
		}
	}
	if err := RunSteps(ctx, db1, []Step{marker}); err != nil {
		t.Fatalf("RunSteps(db1): %v", err)
	}
	if err := RunSteps(ctx, db2, []Step{marker}); err != nil {
		t.Fatalf("RunSteps(db2): %v", err)
	}
	for _, db := range []*sql.DB{db1, db2} {
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM wave_step_marker`).Scan(&n); err != nil || n != 1 {
			t.Fatalf("marker rows = %d, err = %v", n, err)
		}
	}
	// already applied steps are skipped on re-run.
	if err := RunSteps(ctx, db1, []Step{marker}); err != nil {
		t.Fatalf("RunSteps(db1 2nd): %v", err)
	}
	var n int
	if err := db1.QueryRow(`SELECT COUNT(*) FROM wave_step_marker`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("marker rows after re-run = %d, err = %v", n, err)
	}
	// invalid steps are rejected before any work happens.
	if err := RunSteps(ctx, db1, []Step{{Run: func(ctx context.Context, tx *sql.Tx) error { return nil }}}); err == nil {
		t.Fatal("empty step id must be rejected")
	}
	if err := RunSteps(ctx, db1, []Step{{ID: "wave_step_nil"}}); err == nil {
		t.Fatal("nil Run must be rejected")
	}
}

// --- TableConstraints -----------------------------------------------------

func TestMigrateTableConstraintsOnCreate(t *testing.T) {
	db := openTestDB(t)
	if _, err := AutoMigrateModels(context.Background(), db, []any{&waveCheck{}}, WithDestructive(true)); err != nil {
		t.Fatalf("AutoMigrateModels: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO wave_check (id, type) VALUES ('1', 'x')`); err == nil {
		t.Fatal("expected CHECK violation for invalid type")
	}
	if _, err := db.Exec(`INSERT INTO wave_check (id, type) VALUES ('1', 'a')`); err != nil {
		t.Fatalf("valid insert failed: %v", err)
	}
}

func TestMigrateTableConstraintsPreservedOnRebuild(t *testing.T) {
	db := openTestDB(t)
	if _, err := AutoMigrateModels(context.Background(), db, []any{&waveCheckV1{}}, WithDestructive(true)); err != nil {
		t.Fatalf("AutoMigrateModels(v1): %v", err)
	}
	if _, err := db.Exec(`INSERT INTO wave_check_v (id) VALUES ('keep')`); err != nil {
		t.Fatal(err)
	}
	if _, err := AutoMigrateModels(context.Background(), db, []any{&waveCheckV2{}}, WithDestructive(true)); err != nil {
		t.Fatalf("AutoMigrateModels(v2): %v", err)
	}
	var name string
	if err := db.QueryRow(`SELECT id FROM wave_check_v WHERE id = 'keep'`).Scan(&name); err != nil || name != "keep" {
		t.Fatalf("data lost after rebuild: %q, %v", name, err)
	}
	if _, err := db.Exec(`UPDATE wave_check_v SET type = 'x' WHERE id = 'keep'`); err == nil {
		t.Fatal("expected CHECK violation after rebuild")
	}
	if _, err := db.Exec(`UPDATE wave_check_v SET type = 'a' WHERE id = 'keep'`); err != nil {
		t.Fatalf("valid update failed after rebuild: %v", err)
	}
}

func TestMigrateTableConstraintsAddedWithColumn(t *testing.T) {
	db := openTestDB(t)
	if _, err := db.Exec(`CREATE TABLE wave_add_check (id TEXT PRIMARY KEY, name TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO wave_add_check (id, name) VALUES ('1', 'keep')`); err != nil {
		t.Fatal(err)
	}
	if _, err := AutoMigrateModels(context.Background(), db, []any{&waveAddCheck{}}, WithDestructive(true)); err != nil {
		t.Fatalf("AutoMigrateModels: %v", err)
	}
	if _, err := db.Exec(`UPDATE wave_add_check SET content_mode = 'invalid'`); err == nil {
		t.Fatal("expected CHECK violation on column added via ADD COLUMN")
	}
	if _, err := db.Exec(`UPDATE wave_add_check SET content_mode = 'text'`); err != nil {
		t.Fatalf("valid update failed: %v", err)
	}
}

// --- ExtraIndexDDL --------------------------------------------------------

func TestMigrateExtraIndexCreatedAndMatched(t *testing.T) {
	db := openTestDB(t)
	report, err := AutoMigrateModels(context.Background(), db, []any{&waveExtra{}}, WithDestructive(true))
	if err != nil {
		t.Fatalf("AutoMigrateModels: %v", err)
	}
	if !contains(report.AddedTables, "wave_extra") {
		t.Fatalf("AddedTables = %v", report.AddedTables)
	}
	found := false
	for _, idx := range indexNames(t, db, "wave_extra") {
		if idx == "idx_wave_extra_type_at" {
			found = true
		}
	}
	if !found {
		t.Fatal("extra index missing after creation")
	}
	report2, err := AutoMigrateModels(context.Background(), db, []any{&waveExtra{}}, WithDestructive(true))
	if err != nil {
		t.Fatalf("AutoMigrateModels(2nd): %v", err)
	}
	if len(report2.AddedIndexes) != 0 || len(report2.Pending) != 0 || len(report2.DroppedIndexes) != 0 {
		t.Fatalf("2nd pass drift: %+v", report2)
	}
	// takeover of a manually created table with the index must not flag it.
	db2 := openTestDB(t)
	if _, err := db2.Exec(`CREATE TABLE wave_extra (id TEXT PRIMARY KEY, type TEXT NOT NULL, at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db2.Exec(`CREATE INDEX idx_wave_extra_type_at ON wave_extra(type, at)`); err != nil {
		t.Fatal(err)
	}
	report3, err := AutoMigrateModels(context.Background(), db2, []any{&waveExtra{}}, WithDestructive(true))
	if err != nil {
		t.Fatalf("AutoMigrateModels(takeover): %v", err)
	}
	for _, p := range report3.Pending {
		if strings.Contains(p, "idx_wave_extra_type_at") {
			t.Fatalf("declared extra index reported as pending: %v", report3.Pending)
		}
	}
	if len(report3.DroppedIndexes) != 0 {
		t.Fatalf("DroppedIndexes = %v", report3.DroppedIndexes)
	}
}

// --- internal metadata tables ---------------------------------------------

func TestMigrateInternalTablesNeverManaged(t *testing.T) {
	db := openTestDB(t)
	if _, err := db.Exec(`CREATE TABLE orm_meta (table_name TEXT PRIMARY KEY, schema TEXT NOT NULL, synced_at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE orm_migrations (id TEXT PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	raw := `{"columns":[{"name":"id","type":"TEXT","pk":1}],"indexes":[],"fks":[]}`
	for _, table := range []string{"orm_meta", "orm_migrations"} {
		if _, err := db.Exec(`INSERT INTO orm_meta (table_name, schema, synced_at) VALUES (?, ?, ?)`, table, raw, "2026-01-01T00:00:00Z"); err != nil {
			t.Fatal(err)
		}
	}
	if err := Register(&waveBasic{}); err != nil {
		t.Fatal(err)
	}
	r := runMigrate(t, db, true)
	for _, x := range r.DroppedTables {
		if x == "orm_meta" || x == "orm_migrations" {
			t.Fatalf("internal table %s in DroppedTables: %v", x, r.DroppedTables)
		}
	}
	for _, p := range r.Pending {
		if strings.Contains(p, "orm_meta") || strings.Contains(p, "orm_migrations") {
			t.Fatalf("internal table mentioned in Pending: %v", r.Pending)
		}
	}
	if !tableExists(t, db, "orm_meta") || !tableExists(t, db, "orm_migrations") {
		t.Fatal("internal metadata tables must never be dropped")
	}
}

type waveLegacyUnique struct {
	ID   string `orm:"primary_key"`
	App  string `orm:"not_null"`
	Name string `orm:"not_null"`
}

func (waveLegacyUnique) TableName() string { return "wave_legacy_unique" }

func (waveLegacyUnique) ExtraIndexDDL() map[string][]string {
	return map[string][]string{
		"wave_legacy_unique": {
			"CREATE UNIQUE INDEX IF NOT EXISTS uq_wave_legacy_unique_app_name ON wave_legacy_unique(app, name)",
		},
	}
}

// TestMigrateTakeoverMatchesLegacyAutoIndex verifies that a legacy table
// with an inline table-level UNIQUE constraint (surfaced by SQLite as a
// sqlite_autoindex_* index) is recognized as the declared extra index by
// columns+uniqueness, so takeover and the following sync pass report no
// drift and no redundant named index is created.
func TestMigrateTakeoverMatchesLegacyAutoIndex(t *testing.T) {
	db := openTestDB(t)
	if _, err := db.Exec(`CREATE TABLE wave_legacy_unique (id TEXT PRIMARY KEY, app TEXT NOT NULL, name TEXT NOT NULL, UNIQUE(app, name))`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO wave_legacy_unique (id, app, name) VALUES ('1', 'a', 'n')`); err != nil {
		t.Fatal(err)
	}
	report, err := AutoMigrateModels(context.Background(), db, []any{&waveLegacyUnique{}}, WithDestructive(true))
	if err != nil {
		t.Fatalf("AutoMigrateModels: %v", err)
	}
	if len(report.Pending) != 0 {
		t.Fatalf("Pending = %v", report.Pending)
	}
	if len(report.AddedIndexes) != 0 {
		t.Fatalf("takeover must not create indexes, AddedIndexes = %v", report.AddedIndexes)
	}
	report2, err := AutoMigrateModels(context.Background(), db, []any{&waveLegacyUnique{}}, WithDestructive(true))
	if err != nil {
		t.Fatalf("AutoMigrateModels(2nd): %v", err)
	}
	if len(report2.AddedIndexes) != 0 || len(report2.Pending) != 0 || len(report2.DroppedIndexes) != 0 {
		t.Fatalf("2nd pass drift: %+v", report2)
	}
	if _, err := db.Exec(`INSERT INTO wave_legacy_unique (id, app, name) VALUES ('2', 'a', 'n')`); err == nil {
		t.Fatal("expected unique violation enforced by the legacy autoindex")
	}
}
