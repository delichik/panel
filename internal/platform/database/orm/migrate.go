package orm

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"
	"unicode"
)

// DriftReport describes the schema changes performed or pending after an
// AutoMigrate or AutoMigrateModels run.
type DriftReport struct {
	AddedTables        []string
	DroppedTables      []string
	AddedColumns       []string
	DroppedColumns     []string
	AddedIndexes       []string
	DroppedIndexes     []string
	RebuiltTables      []string
	Pending            []string
	SkippedDestructive []string
}

// extraIndexDecl is one composite/partial/composite-unique index that a
// model declares through the ExtraIndexDDLMethod interface. The full DDL
// statement is kept so partial indexes and WHERE clauses survive.
type extraIndexDecl struct {
	name    string
	ddl     string
	cols    []string
	unique  bool
	partial bool
}

// isReservedTable reports whether name is an ORM-internal metadata table.
// Internal tables never appear in drift reports and never participate in
// deletion logic.
func isReservedTable(name string) bool {
	return name == "orm_meta" || name == "orm_migrations"
}

// parseExtraIndexDDL extracts the name, uniqueness, column list and
// partial (WHERE) flag from a CREATE [UNIQUE] INDEX [IF NOT EXISTS] <name>
// ON <table>(<cols>) [WHERE ...] statement. Columns and uniqueness are used
// to recognize legacy inline UNIQUE constraints that SQLite surfaces as
// sqlite_autoindex_* indexes, which cannot be matched by name.
func parseExtraIndexDDL(ddl string) (extraIndexDecl, error) {
	var decl extraIndexDecl
	fields := strings.Fields(ddl)
	if len(fields) < 3 || !strings.EqualFold(fields[0], "CREATE") {
		return decl, fmt.Errorf("invalid extra index DDL %q: must start with CREATE [UNIQUE] INDEX", ddl)
	}
	i := 1
	if strings.EqualFold(fields[i], "UNIQUE") {
		decl.unique = true
		i++
	}
	if i >= len(fields) || !strings.EqualFold(fields[i], "INDEX") {
		return decl, fmt.Errorf("invalid extra index DDL %q: missing INDEX keyword", ddl)
	}
	i++
	if i < len(fields) && strings.EqualFold(fields[i], "IF") {
		if i+2 >= len(fields) || !strings.EqualFold(fields[i+1], "NOT") || !strings.EqualFold(fields[i+2], "EXISTS") {
			return decl, fmt.Errorf("invalid extra index DDL %q: malformed IF NOT EXISTS", ddl)
		}
		i += 3
	}
	if i >= len(fields) {
		return decl, fmt.Errorf("invalid extra index DDL %q: missing index name", ddl)
	}
	name := fields[i]
	for j, r := range name {
		if unicode.IsLetter(r) || r == '_' || (j > 0 && unicode.IsDigit(r)) {
			continue
		}
		return decl, fmt.Errorf("invalid extra index DDL %q: bad index name %q", ddl, name)
	}
	decl.name = name
	decl.ddl = ddl
	open := strings.Index(ddl, "(")
	if open < 0 {
		return decl, fmt.Errorf("invalid extra index DDL %q: missing column list", ddl)
	}
	rel := strings.Index(ddl[open:], ")")
	if rel < 0 {
		return decl, fmt.Errorf("invalid extra index DDL %q: unterminated column list", ddl)
	}
	close := open + rel
	for _, part := range strings.Split(ddl[open+1:close], ",") {
		col := strings.TrimSpace(part)
		if strings.HasPrefix(col, "\"") && strings.HasSuffix(col, "\"") && len(col) >= 2 {
			col = strings.ReplaceAll(col[1:len(col)-1], "\"\"", "\"")
		}
		if col == "" {
			return decl, fmt.Errorf("invalid extra index DDL %q: empty column", ddl)
		}
		decl.cols = append(decl.cols, col)
	}
	if len(decl.cols) == 0 {
		return decl, fmt.Errorf("invalid extra index DDL %q: empty column list", ddl)
	}
	if strings.Contains(strings.ToUpper(ddl[close:]), " WHERE ") {
		decl.partial = true
	}
	return decl, nil
}

// snapshot is the stored schema snapshot for one managed table.
type snapshot struct {
	Columns []snapshotColumn `json:"columns"`
	Indexes []snapshotIndex  `json:"indexes"`
	FKs     []snapshotFK     `json:"fks"`
}

type snapshotColumn struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	NotNull bool   `json:"not_null,omitempty"`
	PK      int    `json:"pk,omitempty"`
	Default string `json:"default,omitempty"`
}

type snapshotIndex struct {
	Name   string   `json:"name"`
	Unique bool     `json:"unique,omitempty"`
	Cols   []string `json:"cols"`
}

type snapshotFK struct {
	From     string `json:"from"`
	Table    string `json:"table"`
	To       string `json:"to"`
	OnDelete string `json:"on_delete,omitempty"`
	OnUpdate string `json:"on_update,omitempty"`
}

func (s *snapshot) hasColumn(name string) bool {
	for _, c := range s.Columns {
		if c.Name == name {
			return true
		}
	}
	return false
}

func (s *snapshot) hasIndex(name string) bool {
	for _, i := range s.Indexes {
		if i.Name == name {
			return true
		}
	}
	return false
}

type actualColumn struct {
	name     string
	typeName string
	notNull  bool
	pk       int
	dflt     string
	hasDflt  bool
}

type actualIndex struct {
	name    string
	unique  bool
	origin  string
	cols    []string
	partial bool
}

type actualFK struct {
	from     string
	table    string
	to       string
	onDelete string
	onUpdate string
}

type tableState struct {
	columns     map[string]*actualColumn
	columnOrder []string
	indexes     []*actualIndex
	fks         []*actualFK
}

func (s *tableState) hasFKFrom(col string) bool {
	for _, fk := range s.fks {
		if fk.from == col {
			return true
		}
	}
	return false
}

func (s *tableState) hasFKTo(col string) bool {
	for _, fk := range s.fks {
		if fk.to == col {
			return true
		}
	}
	return false
}

func (s *tableState) indexesForColumn(col string) []*actualIndex {
	var out []*actualIndex
	for _, idx := range s.indexes {
		for _, c := range idx.cols {
			if c == col {
				out = append(out, idx)
				break
			}
		}
	}
	return out
}

func (s *tableState) hasIndex(cols []string, unique bool) bool {
	for _, idx := range s.indexes {
		if idx.unique == unique && sameCols(idx.cols, cols) {
			return true
		}
	}
	return false
}

type modelIndex struct {
	name   string
	unique bool
	cols   []string
}

func modelIndexes(meta *modelInfo) []modelIndex {
	var out []modelIndex
	for _, f := range meta.fields {
		if f.primaryKey || f.autoIncrement {
			continue
		}
		if f.unique {
			out = append(out, modelIndex{name: "uq_" + meta.table + "_" + f.column, unique: true, cols: []string{f.column}})
		} else if f.indexed {
			out = append(out, modelIndex{name: "idx_" + meta.table + "_" + f.column, unique: false, cols: []string{f.column}})
		}
	}
	return out
}

type modelFK struct {
	from     string
	table    string
	to       string
	onDelete string
	onUpdate string
}

func modelFKs(meta *modelInfo) []modelFK {
	var out []modelFK
	for _, f := range meta.fields {
		if f.refTable == "" {
			continue
		}
		out = append(out, modelFK{from: f.column, table: f.refTable, to: f.refColumn, onDelete: normalizeAction(f.onDelete), onUpdate: normalizeAction(f.onUpdate)})
	}
	return out
}

func normalizeAction(a string) string {
	if a == "" {
		return "NO ACTION"
	}
	return strings.ToUpper(a)
}

func sameCols(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	aa := append([]string{}, a...)
	bb := append([]string{}, b...)
	sort.Strings(aa)
	sort.Strings(bb)
	for i := range aa {
		if aa[i] != bb[i] {
			return false
		}
	}
	return true
}

// migrator runs one AutoMigrate pass on a single dedicated connection so
// that PRAGMA foreign_keys applies to every statement.
type migrator struct {
	conn         *sql.Conn
	destructive  bool
	logf         func(format string, args ...any)
	report       *DriftReport
	allFKs       map[string][]actualFK
	extraIndexes map[string][]extraIndexDecl
}

// execer is satisfied by both *sql.Conn and *sql.Tx so DDL helpers can run
// either directly on the dedicated connection or inside a rebuild transaction.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func (m *migrator) log(format string, args ...any) {
	if m.logf != nil {
		m.logf(format, args...)
	}
}

// AutoMigrate brings the database in line with all registered models. It
// only manages tables it has taken over (recorded in orm_meta); the first
// takeover of an existing table records a snapshot and performs a strictly
// non-destructive column sync without deleting anything. Destructive
// changes are gated by WithDestructive. It delegates to AutoMigrateModels
// with the globally registered models.
func AutoMigrate(ctx context.Context, db *sql.DB, opts ...Option) (*DriftReport, error) {
	return AutoMigrateModels(ctx, db, registeredModelValues(), opts...)
}

// AutoMigrateModels brings the database in line with the given models only.
// It is the per-database variant of AutoMigrate: models that are not in the
// list are never created, synced or dropped, and tables managed by an
// earlier pass that are no longer declared in the list are reported as
// stale (dropped only with WithDestructive). Model metadata is parsed on
// the fly; the global registration registry is not touched.
func AutoMigrateModels(ctx context.Context, db *sql.DB, models []any, opts ...Option) (*DriftReport, error) {
	o := &Options{}
	for _, fn := range opts {
		fn(o)
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	infos := make([]*modelInfo, 0, len(models))
	extras := map[string][]extraIndexDecl{}
	for _, mdl := range models {
		t := reflect.TypeOf(mdl)
		if t == nil {
			return nil, fmt.Errorf("orm: cannot migrate a nil model")
		}
		if t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		info, err := metaFor(t)
		if err != nil {
			return nil, err
		}
		if isReservedTable(info.table) {
			return nil, fmt.Errorf("orm: model %v uses reserved table name %q", info.typ, info.table)
		}
		infos = append(infos, info)
		for table, ddlList := range info.extraIndexDDL {
			for _, ddl := range ddlList {
				decl, err := parseExtraIndexDDL(ddl)
				if err != nil {
					return nil, fmt.Errorf("orm: model %s: %w", info.table, err)
				}
				dup := false
				for _, ex := range extras[table] {
					if ex.name == decl.name {
						if ex.ddl != ddl {
							return nil, fmt.Errorf("orm: conflicting extra index %s for table %s", decl.name, table)
						}
						dup = true
						break
					}
				}
				if !dup {
					extras[table] = append(extras[table], decl)
				}
			}
		}
	}
	m := &migrator{conn: conn, destructive: o.Destructive, logf: o.Logger, report: &DriftReport{}, extraIndexes: extras}
	if err := m.run(ctx, infos); err != nil {
		return m.report, err
	}
	return m.report, nil
}

func (m *migrator) run(ctx context.Context, models []*modelInfo) error {
	if err := m.ensureMetaTable(ctx); err != nil {
		return err
	}
	snaps, err := m.loadSnapshots(ctx)
	if err != nil {
		return err
	}
	modelTables := make(map[string]*modelInfo, len(models))
	for _, meta := range models {
		modelTables[meta.table] = meta
	}
	fkOn, err := m.foreignKeysPragma(ctx)
	if err != nil {
		return err
	}
	if fkOn {
		if _, err := m.conn.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
			return err
		}
		defer func() {
			_, _ = m.conn.ExecContext(context.Background(), `PRAGMA foreign_keys = ON`)
		}()
	}
	for _, meta := range models {
		snap, managed := snaps[meta.table]
		if !managed {
			if err := m.takeover(ctx, meta); err != nil {
				return fmt.Errorf("orm: takeover %s: %w", meta.table, err)
			}
			continue
		}
		if err := m.syncTable(ctx, meta, snap); err != nil {
			return fmt.Errorf("orm: sync %s: %w", meta.table, err)
		}
	}
	if err := m.dropStaleTables(ctx, snaps, modelTables); err != nil {
		return err
	}
	return nil
}

func (m *migrator) ensureMetaTable(ctx context.Context) error {
	_, err := m.conn.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS orm_meta (
		table_name TEXT PRIMARY KEY,
		schema TEXT NOT NULL,
		synced_at TEXT NOT NULL
	)`)
	return err
}

func (m *migrator) loadSnapshots(ctx context.Context) (map[string]*snapshot, error) {
	rows, err := m.conn.QueryContext(ctx, `SELECT table_name, schema FROM orm_meta`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]*snapshot{}
	for rows.Next() {
		var name, raw string
		if err := rows.Scan(&name, &raw); err != nil {
			return nil, err
		}
		var snap snapshot
		if err := json.Unmarshal([]byte(raw), &snap); err != nil {
			return nil, fmt.Errorf("orm: corrupt orm_meta snapshot for %s: %w", name, err)
		}
		out[name] = &snap
	}
	return out, rows.Err()
}

func (m *migrator) saveSnapshot(ctx context.Context, table string, snap *snapshot) error {
	raw, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	_, err = m.conn.ExecContext(ctx, `INSERT INTO orm_meta (table_name, schema, synced_at) VALUES (?, ?, ?)
		ON CONFLICT(table_name) DO UPDATE SET schema = excluded.schema, synced_at = excluded.synced_at`,
		table, string(raw), time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func (m *migrator) foreignKeysPragma(ctx context.Context) (bool, error) {
	var v int64
	if err := m.conn.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&v); err != nil {
		return false, err
	}
	return v != 0, nil
}

func (m *migrator) tableExists(ctx context.Context, name string) (bool, error) {
	var n int64
	if err := m.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, name).Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}

func (m *migrator) listTables(ctx context.Context) ([]string, error) {
	rows, err := m.conn.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		if name == "orm_meta" || name == "orm_migrations" {
			continue
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

func (m *migrator) introspect(ctx context.Context, table string) (*tableState, error) {
	exists, err := m.tableExists(ctx, table)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil
	}
	st := &tableState{columns: map[string]*actualColumn{}}
	rows, err := m.conn.QueryContext(ctx, "PRAGMA table_info("+quoteIdent(table)+")")
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var cid, notnull, pk int
		var name, typ string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			rows.Close()
			return nil, err
		}
		ac := &actualColumn{name: name, typeName: typ, notNull: notnull != 0, pk: pk}
		if dflt.Valid {
			ac.dflt = dflt.String
			ac.hasDflt = true
		}
		st.columns[name] = ac
		st.columnOrder = append(st.columnOrder, name)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	idxRows, err := m.conn.QueryContext(ctx, "PRAGMA index_list("+quoteIdent(table)+")")
	if err != nil {
		return nil, err
	}
	for idxRows.Next() {
		var seq, unique, partial int
		var name, origin string
		if err := idxRows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
			idxRows.Close()
			return nil, err
		}
		if origin == "pk" {
			continue
		}
		cols, err := m.indexColumns(ctx, name)
		if err != nil {
			idxRows.Close()
			return nil, err
		}
		st.indexes = append(st.indexes, &actualIndex{name: name, unique: unique != 0, origin: origin, cols: cols, partial: partial != 0})
	}
	if err := idxRows.Err(); err != nil {
		idxRows.Close()
		return nil, err
	}
	if err := idxRows.Close(); err != nil {
		return nil, err
	}

	fkRows, err := m.conn.QueryContext(ctx, "PRAGMA foreign_key_list("+quoteIdent(table)+")")
	if err != nil {
		return nil, err
	}
	for fkRows.Next() {
		var id, seq int
		var refTable string
		var from, to, onUpdate, onDelete, match sql.NullString
		if err := fkRows.Scan(&id, &seq, &refTable, &from, &to, &onUpdate, &onDelete, &match); err != nil {
			fkRows.Close()
			return nil, err
		}
		if !from.Valid || !to.Valid {
			continue
		}
		st.fks = append(st.fks, &actualFK{from: from.String, table: refTable, to: to.String, onDelete: onDelete.String, onUpdate: onUpdate.String})
	}
	if err := fkRows.Err(); err != nil {
		fkRows.Close()
		return nil, err
	}
	return st, fkRows.Close()
}

func (m *migrator) indexColumns(ctx context.Context, name string) ([]string, error) {
	rows, err := m.conn.QueryContext(ctx, "PRAGMA index_info("+quoteIdent(name)+")")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cols []string
	for rows.Next() {
		var seqno, cid int
		var col sql.NullString
		if err := rows.Scan(&seqno, &cid, &col); err != nil {
			return nil, err
		}
		if col.Valid {
			cols = append(cols, col.String)
		}
	}
	return cols, rows.Err()
}

func (m *migrator) foreignKeyList(ctx context.Context, table string) ([]actualFK, error) {
	rows, err := m.conn.QueryContext(ctx, "PRAGMA foreign_key_list("+quoteIdent(table)+")")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []actualFK
	for rows.Next() {
		var id, seq int
		var refTable string
		var from, to, onUpdate, onDelete, match sql.NullString
		if err := rows.Scan(&id, &seq, &refTable, &from, &to, &onUpdate, &onDelete, &match); err != nil {
			return nil, err
		}
		if !from.Valid || !to.Valid {
			continue
		}
		out = append(out, actualFK{from: from.String, table: refTable, to: to.String, onDelete: onDelete.String, onUpdate: onUpdate.String})
	}
	return out, rows.Err()
}

func (m *migrator) allForeignKeys(ctx context.Context) (map[string][]actualFK, error) {
	if m.allFKs != nil {
		return m.allFKs, nil
	}
	tables, err := m.listTables(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string][]actualFK{}
	for _, t := range tables {
		fks, err := m.foreignKeyList(ctx, t)
		if err != nil {
			return nil, err
		}
		out[t] = fks
	}
	m.allFKs = out
	return out, nil
}

func (m *migrator) referencedByOtherTable(ctx context.Context, table, col string) (bool, error) {
	fks, err := m.allForeignKeys(ctx)
	if err != nil {
		return false, err
	}
	for t, list := range fks {
		if t == table {
			continue
		}
		for _, fk := range list {
			if fk.table == table && fk.to == col {
				return true, nil
			}
		}
	}
	return false, nil
}

func snapshotFromState(st *tableState) *snapshot {
	s := &snapshot{}
	for _, name := range st.columnOrder {
		col := st.columns[name]
		sc := snapshotColumn{Name: col.name, Type: col.typeName, NotNull: col.notNull, PK: col.pk}
		if col.hasDflt {
			sc.Default = col.dflt
		}
		s.Columns = append(s.Columns, sc)
	}
	for _, idx := range st.indexes {
		s.Indexes = append(s.Indexes, snapshotIndex{Name: idx.name, Unique: idx.unique, Cols: append([]string{}, idx.cols...)})
	}
	for _, fk := range st.fks {
		s.FKs = append(s.FKs, snapshotFK{From: fk.from, Table: fk.table, To: fk.to, OnDelete: fk.onDelete, OnUpdate: fk.onUpdate})
	}
	return s
}

// refreshSnapshot builds the next stored snapshot from the current table
// state. Columns, indexes and FKs that are neither declared by the model
// nor present in the previous snapshot are treated as external and never
// become managed. Extra indexes declared through ExtraIndexDDLMethod are
// considered model-declared.
func refreshSnapshot(prev *snapshot, actual *tableState, meta *modelInfo, extras []extraIndexDecl) *snapshot {
	out := &snapshot{}
	for _, name := range actual.columnOrder {
		col := actual.columns[name]
		if _, inModel := meta.byColumn[name]; !inModel && !prev.hasColumn(name) {
			continue
		}
		sc := snapshotColumn{Name: col.name, Type: col.typeName, NotNull: col.notNull, PK: col.pk}
		if col.hasDflt {
			sc.Default = col.dflt
		}
		out.Columns = append(out.Columns, sc)
	}
	modelIdx := modelIndexes(meta)
	for _, idx := range actual.indexes {
		matched := false
		for _, mi := range modelIdx {
			if mi.unique == idx.unique && sameCols(mi.cols, idx.cols) {
				matched = true
				break
			}
		}
		if !matched {
			for _, ex := range extras {
				if ex.name == idx.name || (ex.unique == idx.unique && ex.partial == idx.partial && sameCols(ex.cols, idx.cols)) {
					matched = true
					break
				}
			}
		}
		if !matched && !prev.hasIndex(idx.name) {
			continue
		}
		out.Indexes = append(out.Indexes, snapshotIndex{Name: idx.name, Unique: idx.unique, Cols: append([]string{}, idx.cols...)})
	}
	modelFks := modelFKs(meta)
	for _, fk := range actual.fks {
		matched := false
		for _, mfk := range modelFks {
			if fk.from == mfk.from && fk.table == mfk.table && fk.to == mfk.to {
				matched = true
				break
			}
		}
		if !matched && !snapshotHasFK(prev, fk) {
			continue
		}
		out.FKs = append(out.FKs, snapshotFK{From: fk.from, Table: fk.table, To: fk.to, OnDelete: fk.onDelete, OnUpdate: fk.onUpdate})
	}
	return out
}

func snapshotHasFK(prev *snapshot, fk *actualFK) bool {
	for _, s := range prev.FKs {
		if s.From == fk.from && s.Table == fk.table && s.To == fk.to {
			return true
		}
	}
	return false
}

// takeover handles the first AutoMigrate pass for a registered model. An
// existing table is snapshotted and then synced in a strictly
// non-destructive way (missing plain columns are added, nothing is
// deleted); a missing table is created.
func (m *migrator) takeover(ctx context.Context, meta *modelInfo) error {
	exists, err := m.tableExists(ctx, meta.table)
	if err != nil {
		return err
	}
	if exists {
		st, err := m.introspect(ctx, meta.table)
		if err != nil {
			return err
		}
		modelIdx := modelIndexes(meta)
		for _, name := range st.columnOrder {
			if _, ok := meta.byColumn[name]; !ok {
				m.report.Pending = append(m.report.Pending, fmt.Sprintf("table %s taken over; column %s exists but is not in the model", meta.table, name))
			}
		}
		for _, idx := range st.indexes {
			matched := false
			for _, mi := range modelIdx {
				if mi.unique == idx.unique && sameCols(mi.cols, idx.cols) {
					matched = true
					break
				}
			}
			if !matched && (m.declaresExtraIndex(meta.table, idx.name) || m.extraMatchCols(meta.table, idx.cols, idx.unique, idx.partial)) {
				matched = true
			}
			if !matched {
				m.report.Pending = append(m.report.Pending, fmt.Sprintf("table %s taken over; index %s exists but is not in the model", meta.table, idx.name))
			}
		}
		modelFks := modelFKs(meta)
		for _, fk := range st.fks {
			if !fkMatches(modelFks, snapshotFK{From: fk.from, Table: fk.table, To: fk.to, OnDelete: fk.onDelete, OnUpdate: fk.onUpdate}) {
				m.report.Pending = append(m.report.Pending, fmt.Sprintf("table %s taken over; foreign key on %s differs from the model", meta.table, fk.from))
			}
		}
		snap := snapshotFromState(st)
		m.log("orm: table %s taken over, snapshot recorded (no destructive changes)", meta.table)
		if err := m.saveSnapshot(ctx, meta.table, snap); err != nil {
			return err
		}
		return m.syncTakeover(ctx, meta, st, snap)
	}
	if err := m.createTable(ctx, meta); err != nil {
		return err
	}
	m.report.AddedTables = append(m.report.AddedTables, meta.table)
	m.log("orm: created table %s", meta.table)
	st, err := m.introspect(ctx, meta.table)
	if err != nil {
		return err
	}
	return m.saveSnapshot(ctx, meta.table, snapshotFromState(st))
}

// syncTakeover brings a freshly taken-over table in line with the model in
// a strictly non-destructive way: missing plain columns are added (with
// their table-level CHECK clauses when they reference the added column),
// and anything that would require a rebuild is reported as Pending for the
// next run. No indexes are created here, so unique indexes never run
// against dirty legacy data before the migration steps have normalized it.
func (m *migrator) syncTakeover(ctx context.Context, meta *modelInfo, actual *tableState, snap *snapshot) error {
	var missing []*fieldInfo
	for _, f := range meta.fields {
		if _, ok := actual.columns[f.column]; !ok {
			missing = append(missing, f)
		}
	}
	rebuildNeeded := false
	for _, f := range missing {
		if fieldNeedsRebuild(f) {
			rebuildNeeded = true
			m.report.Pending = append(m.report.Pending, fmt.Sprintf("table %s taken over; column %s requires a rebuild and will be added on the next run", meta.table, f.column))
		}
	}
	if rebuildNeeded {
		return nil
	}
	for _, f := range missing {
		if _, err := m.conn.ExecContext(ctx, m.alterAddStmt(meta, f)); err != nil {
			return fmt.Errorf("add column %s.%s: %w", meta.table, f.column, err)
		}
		m.report.AddedColumns = append(m.report.AddedColumns, meta.table+"."+f.column)
		if f.notNull && !f.hasDefault {
			m.report.Pending = append(m.report.Pending, fmt.Sprintf("column %s.%s is NOT NULL without a default; added as nullable", meta.table, f.column))
		}
		m.log("orm: added column %s.%s on takeover", meta.table, f.column)
	}
	if len(missing) == 0 {
		return nil
	}
	newActual, err := m.introspect(ctx, meta.table)
	if err != nil {
		return err
	}
	return m.saveSnapshot(ctx, meta.table, refreshSnapshot(snap, newActual, meta, m.extraIndexes[meta.table]))
}

// declaresExtraIndex reports whether the table declares an extra index with
// the given name through ExtraIndexDDLMethod.
func (m *migrator) declaresExtraIndex(table, name string) bool {
	for _, ex := range m.extraIndexes[table] {
		if ex.name == name {
			return true
		}
	}
	return false
}

// extraMatchCols reports whether the table declares an extra index with the
// same columns, uniqueness and partiality as the given actual index. Legacy
// inline UNIQUE constraints are surfaced by SQLite as sqlite_autoindex_*
// indexes whose names cannot match a declared extra index, so they are
// recognized by their shape instead.
func (m *migrator) extraMatchCols(table string, cols []string, unique, partial bool) bool {
	for _, ex := range m.extraIndexes[table] {
		if ex.unique == unique && ex.partial == partial && sameCols(ex.cols, cols) {
			return true
		}
	}
	return false
}

func (m *migrator) syncTable(ctx context.Context, meta *modelInfo, snap *snapshot) error {
	actual, err := m.introspect(ctx, meta.table)
	if err != nil {
		return err
	}
	if actual == nil {
		if err := m.createTable(ctx, meta); err != nil {
			return err
		}
		m.report.AddedTables = append(m.report.AddedTables, meta.table)
		m.log("orm: recreated missing table %s", meta.table)
		newSnap, err := m.introspect(ctx, meta.table)
		if err != nil {
			return err
		}
		return m.saveSnapshot(ctx, meta.table, snapshotFromState(newSnap))
	}

	var missing []*fieldInfo
	for _, f := range meta.fields {
		if _, ok := actual.columns[f.column]; !ok {
			missing = append(missing, f)
		}
	}
	var dropCols []string
	for _, name := range actual.columnOrder {
		if _, inModel := meta.byColumn[name]; inModel {
			continue
		}
		if !snap.hasColumn(name) {
			m.report.Pending = append(m.report.Pending, fmt.Sprintf("column %s.%s is not managed by the ORM; left untouched", meta.table, name))
			continue
		}
		dropCols = append(dropCols, name)
	}

	addTriggered := false
	for _, f := range missing {
		if fieldNeedsRebuild(f) {
			addTriggered = true
			break
		}
	}
	dropTriggered := false
	for _, name := range dropCols {
		col := actual.columns[name]
		if col.pk > 0 || actual.hasFKFrom(name) || actual.hasFKTo(name) {
			dropTriggered = true
			break
		}
		refd, err := m.referencedByOtherTable(ctx, meta.table, name)
		if err != nil {
			return err
		}
		if refd {
			dropTriggered = true
			break
		}
	}

	if addTriggered || (dropTriggered && m.destructive) {
		if err := m.rebuildTable(ctx, meta, actual, snap); err != nil {
			return err
		}
		m.report.RebuiltTables = append(m.report.RebuiltTables, meta.table)
		for _, f := range missing {
			m.report.AddedColumns = append(m.report.AddedColumns, meta.table+"."+f.column)
		}
		for _, name := range dropCols {
			if m.destructive {
				m.report.DroppedColumns = append(m.report.DroppedColumns, meta.table+"."+name)
			} else {
				m.report.SkippedDestructive = append(m.report.SkippedDestructive, "column "+meta.table+"."+name)
			}
		}
		newSnap, err := m.introspect(ctx, meta.table)
		if err != nil {
			return err
		}
		return m.saveSnapshot(ctx, meta.table, refreshSnapshot(snap, newSnap, meta, m.extraIndexes[meta.table]))
	}

	// Add columns via ALTER TABLE ADD COLUMN.
	for _, f := range missing {
		stmt := m.alterAddStmt(meta, f)
		if _, err := m.conn.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("add column %s.%s: %w", meta.table, f.column, err)
		}
		m.report.AddedColumns = append(m.report.AddedColumns, meta.table+"."+f.column)
		if f.notNull && !f.hasDefault {
			m.report.Pending = append(m.report.Pending, fmt.Sprintf("column %s.%s is NOT NULL without a default; added as nullable", meta.table, f.column))
		}
	}
	// Drop managed columns, dropping their indexes first.
	droppedCols := map[string]bool{}
	for _, name := range dropCols {
		if !m.destructive {
			m.report.SkippedDestructive = append(m.report.SkippedDestructive, "column "+meta.table+"."+name)
			continue
		}
		droppedCols[name] = true
		for _, idx := range actual.indexesForColumn(name) {
			if idx.origin == "pk" {
				continue
			}
			if _, err := m.conn.ExecContext(ctx, `DROP INDEX IF EXISTS `+quoteIdent(idx.name)); err != nil {
				return fmt.Errorf("drop index %s: %w", idx.name, err)
			}
			m.report.DroppedIndexes = append(m.report.DroppedIndexes, meta.table+"."+idx.name)
		}
		if _, err := m.conn.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s", quoteIdent(meta.table), quoteIdent(name))); err != nil {
			return fmt.Errorf("drop column %s.%s: %w", meta.table, name, err)
		}
		m.report.DroppedColumns = append(m.report.DroppedColumns, meta.table+"."+name)
		m.log("orm: dropped column %s.%s", meta.table, name)
	}
	if err := m.syncIndexes(ctx, meta, actual, snap, droppedCols); err != nil {
		return err
	}
	m.syncFK(meta, actual)
	newSnap, err := m.introspect(ctx, meta.table)
	if err != nil {
		return err
	}
	return m.saveSnapshot(ctx, meta.table, refreshSnapshot(snap, newSnap, meta, m.extraIndexes[meta.table]))
}

func (m *migrator) alterAddStmt(meta *modelInfo, f *fieldInfo) string {
	def := m.columnDef(f, false)
	if f.notNull && !f.hasDefault {
		// SQLite refuses ADD COLUMN with NOT NULL unless a default is
		// present; add the column as nullable and record the drift.
		nf := *f
		nf.notNull = false
		def = m.columnDef(&nf, false)
	}
	// Table-level constraints (e.g. CHECK) that reference the added column
	// are appended inline so legacy tables upgraded via ADD COLUMN get the
	// same enforcement as freshly created ones.
	for _, c := range meta.tableConstraints {
		if constraintReferencesColumn(c, f.column) {
			def += " " + c
		}
	}
	return fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s", quoteIdent(meta.table), def)
}

// constraintReferencesColumn reports whether a raw constraint clause refers
// to the given column (identifier token match, so e.g. "type" does not
// match "content_type").
func constraintReferencesColumn(clause, col string) bool {
	for _, tok := range strings.FieldsFunc(clause, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_'
	}) {
		if tok == col {
			return true
		}
	}
	return false
}

func fieldNeedsRebuild(f *fieldInfo) bool {
	return f.primaryKey || f.autoIncrement || f.unique || f.refTable != ""
}

// rebuildTable recreates a table from the model, copying surviving data.
// Columns that exist in the database but are not in the model are
// preserved unless they are managed by the ORM and destructive mode is on.
func (m *migrator) rebuildTable(ctx context.Context, meta *modelInfo, actual *tableState, snap *snapshot) error {
	// Rebuild inside an explicit transaction so a crash mid-rebuild cannot
	// leave the original table dropped with only a temporary table behind.
	tx, err := m.conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("rebuild %s: begin transaction: %w", meta.table, err)
	}
	defer tx.Rollback()
	tmp := "__orm_rebuild_" + meta.table
	// A previous crashed rebuild may have left a stale temp table; drop it so
	// the CREATE TABLE below never fails on an existing name.
	if _, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS `+quoteIdent(tmp)); err != nil {
		return fmt.Errorf("rebuild %s: drop stale temp table: %w", meta.table, err)
	}
	var preserve []*actualColumn
	for _, name := range actual.columnOrder {
		if _, inModel := meta.byColumn[name]; !inModel {
			if snap.hasColumn(name) && m.destructive {
				continue
			}
			preserve = append(preserve, actual.columns[name])
		}
	}
	ddl := m.createTableDDL(tmp, meta, preserve)
	if _, err := tx.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("rebuild %s: create %s: %w", meta.table, tmp, err)
	}
	copyCols := m.copyColumns(meta, actual, preserve)
	quoted := make([]string, len(copyCols))
	for i, c := range copyCols {
		quoted[i] = quoteIdent(c)
	}
	stmt := fmt.Sprintf("INSERT INTO %s (%s) SELECT %s FROM %s", quoteIdent(tmp), strings.Join(quoted, ", "), strings.Join(quoted, ", "), quoteIdent(meta.table))
	if _, err := tx.ExecContext(ctx, stmt); err != nil {
		return fmt.Errorf("rebuild %s: copy data: %w", meta.table, err)
	}
	if _, err := tx.ExecContext(ctx, `DROP TABLE `+quoteIdent(meta.table)); err != nil {
		return fmt.Errorf("rebuild %s: drop old table: %w", meta.table, err)
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s RENAME TO %s", quoteIdent(tmp), quoteIdent(meta.table))); err != nil {
		return fmt.Errorf("rebuild %s: rename: %w", meta.table, err)
	}
	if err := m.createModelIndexes(ctx, tx, meta); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("rebuild %s: commit: %w", meta.table, err)
	}
	m.log("orm: rebuilt table %s", meta.table)
	return nil
}

func (m *migrator) copyColumns(meta *modelInfo, actual *tableState, preserve []*actualColumn) []string {
	var out []string
	for _, f := range meta.fields {
		if _, ok := actual.columns[f.column]; ok {
			out = append(out, f.column)
		}
	}
	for _, c := range preserve {
		out = append(out, c.name)
	}
	return out
}

func (m *migrator) createTable(ctx context.Context, meta *modelInfo) error {
	if _, err := m.conn.ExecContext(ctx, m.createTableDDL(meta.table, meta, nil)); err != nil {
		return fmt.Errorf("create table %s: %w", meta.table, err)
	}
	return m.createModelIndexes(ctx, m.conn, meta)
}

func (m *migrator) createTableDDL(name string, meta *modelInfo, preserve []*actualColumn) string {
	defs := make([]string, 0, len(meta.fields)+len(preserve)+1)
	for _, f := range meta.fields {
		inlinePK := f.primaryKey && len(meta.pk) == 1
		defs = append(defs, m.columnDef(f, inlinePK))
	}
	for _, c := range preserve {
		defs = append(defs, m.preservedColumnDef(c))
	}
	if len(meta.pk) > 1 {
		cols := make([]string, len(meta.pk))
		for i, p := range meta.pk {
			cols[i] = quoteIdent(p.column)
		}
		defs = append(defs, "PRIMARY KEY ("+strings.Join(cols, ", ")+")")
	}
	defs = append(defs, meta.tableConstraints...)
	return "CREATE TABLE " + quoteIdent(name) + " (" + strings.Join(defs, ", ") + ")"
}

func (m *migrator) columnDef(f *fieldInfo, inlinePK bool) string {
	typ := f.typeOverride
	if typ == "" {
		typ = mappedType(f)
		if f.size > 0 {
			typ = fmt.Sprintf("%s(%d)", typ, f.size)
		}
	}
	var b strings.Builder
	b.WriteString(quoteIdent(f.column) + " " + typ)
	if f.notNull || inlinePK {
		b.WriteString(" NOT NULL")
	}
	if f.hasDefault {
		b.WriteString(" DEFAULT " + f.defaultValue)
	}
	if inlinePK {
		b.WriteString(" PRIMARY KEY")
		if f.autoIncrement {
			b.WriteString(" AUTOINCREMENT")
		}
	}
	if f.unique {
		b.WriteString(" UNIQUE")
	}
	if f.refTable != "" {
		b.WriteString(" REFERENCES " + quoteIdent(f.refTable) + "(" + quoteIdent(f.refColumn) + ")")
		if f.onDelete != "" {
			b.WriteString(" ON DELETE " + f.onDelete)
		}
		if f.onUpdate != "" {
			b.WriteString(" ON UPDATE " + f.onUpdate)
		}
	}
	return b.String()
}

func mappedType(f *fieldInfo) string {
	switch f.kind {
	case kindString, kindJSON, kindAny, kindScanner:
		return "TEXT"
	case kindInt, kindUint, kindBool:
		return "INTEGER"
	case kindFloat:
		return "REAL"
	case kindTime:
		if f.timeUnix {
			return "INTEGER"
		}
		return "TEXT"
	case kindBytes:
		return "BLOB"
	default:
		return "TEXT"
	}
}

func (m *migrator) preservedColumnDef(c *actualColumn) string {
	typ := c.typeName
	if typ == "" {
		typ = "TEXT"
	}
	var b strings.Builder
	b.WriteString(quoteIdent(c.name) + " " + typ)
	if c.notNull {
		b.WriteString(" NOT NULL")
	}
	if c.hasDflt {
		b.WriteString(" DEFAULT " + c.dflt)
	}
	if c.pk > 0 {
		b.WriteString(" PRIMARY KEY")
	}
	return b.String()
}

func (m *migrator) createModelIndexes(ctx context.Context, db execer, meta *modelInfo) error {
	for _, mi := range modelIndexes(meta) {
		uniq := ""
		if mi.unique {
			uniq = "UNIQUE "
		}
		cols := make([]string, len(mi.cols))
		for i, c := range mi.cols {
			cols[i] = quoteIdent(c)
		}
		stmt := fmt.Sprintf("CREATE %sINDEX IF NOT EXISTS %s ON %s (%s)", uniq, quoteIdent(mi.name), quoteIdent(meta.table), strings.Join(cols, ", "))
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("create index %s: %w", mi.name, err)
		}
	}
	for _, ex := range m.extraIndexes[meta.table] {
		if _, err := db.ExecContext(ctx, ex.ddl); err != nil {
			return fmt.Errorf("create index %s: %w", ex.name, err)
		}
	}
	return nil
}

func (m *migrator) syncIndexes(ctx context.Context, meta *modelInfo, actual *tableState, snap *snapshot, droppedCols map[string]bool) error {
	modelIdx := modelIndexes(meta)
	for _, mi := range modelIdx {
		if actual.hasIndex(mi.cols, mi.unique) {
			continue
		}
		uniq := ""
		if mi.unique {
			uniq = "UNIQUE "
		}
		cols := make([]string, len(mi.cols))
		for i, c := range mi.cols {
			cols[i] = quoteIdent(c)
		}
		stmt := fmt.Sprintf("CREATE %sINDEX IF NOT EXISTS %s ON %s (%s)", uniq, quoteIdent(mi.name), quoteIdent(meta.table), strings.Join(cols, ", "))
		if _, err := m.conn.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("create index %s: %w", mi.name, err)
		}
		m.report.AddedIndexes = append(m.report.AddedIndexes, meta.table+"."+mi.name)
	}
	for _, ex := range m.extraIndexes[meta.table] {
		if actualHasIndexName(actual, ex.name) || m.extraMatchCols(meta.table, ex.cols, ex.unique, ex.partial) {
			continue
		}
		if _, err := m.conn.ExecContext(ctx, ex.ddl); err != nil {
			return fmt.Errorf("create index %s: %w", ex.name, err)
		}
		m.report.AddedIndexes = append(m.report.AddedIndexes, meta.table+"."+ex.name)
	}
	for _, idx := range actual.indexes {
		if idx.origin == "pk" {
			continue
		}
		if indexTouchesColumns(idx, droppedCols) {
			continue
		}
		matched := false
		for _, mi := range modelIdx {
			if mi.unique == idx.unique && sameCols(mi.cols, idx.cols) {
				matched = true
				break
			}
		}
		if !matched && (m.declaresExtraIndex(meta.table, idx.name) || m.extraMatchCols(meta.table, idx.cols, idx.unique, idx.partial)) {
			matched = true
		}
		if matched {
			continue
		}
		if !snap.hasIndex(idx.name) {
			m.report.Pending = append(m.report.Pending, fmt.Sprintf("index %s.%s is not managed by the ORM; left untouched", meta.table, idx.name))
			continue
		}
		if idx.origin == "u" {
			m.report.Pending = append(m.report.Pending, fmt.Sprintf("index %s.%s backs a UNIQUE constraint and cannot be dropped without a rebuild", meta.table, idx.name))
			continue
		}
		if !m.destructive {
			m.report.SkippedDestructive = append(m.report.SkippedDestructive, "index "+meta.table+"."+idx.name)
			continue
		}
		if _, err := m.conn.ExecContext(ctx, `DROP INDEX IF EXISTS `+quoteIdent(idx.name)); err != nil {
			return fmt.Errorf("drop index %s: %w", idx.name, err)
		}
		m.report.DroppedIndexes = append(m.report.DroppedIndexes, meta.table+"."+idx.name)
	}
	return nil
}

func indexTouchesColumns(idx *actualIndex, cols map[string]bool) bool {
	for _, c := range idx.cols {
		if cols[c] {
			return true
		}
	}
	return false
}

func actualHasIndexName(st *tableState, name string) bool {
	for _, idx := range st.indexes {
		if idx.name == name {
			return true
		}
	}
	return false
}

func fkMatches(modelFks []modelFK, fk snapshotFK) bool {
	for _, mfk := range modelFks {
		if mfk.from == fk.From && mfk.table == fk.Table && mfk.to == fk.To &&
			strings.EqualFold(mfk.onDelete, fk.OnDelete) && strings.EqualFold(mfk.onUpdate, fk.OnUpdate) {
			return true
		}
	}
	return false
}

func (m *migrator) syncFK(meta *modelInfo, actual *tableState) {
	modelFks := modelFKs(meta)
	for _, mfk := range modelFks {
		matched := false
		for _, afk := range actual.fks {
			if afk.from == mfk.from && afk.table == mfk.table && afk.to == mfk.to &&
				strings.EqualFold(afk.onDelete, mfk.onDelete) && strings.EqualFold(afk.onUpdate, mfk.onUpdate) {
				matched = true
				break
			}
		}
		if !matched {
			m.report.Pending = append(m.report.Pending, fmt.Sprintf("foreign key %s.%s -> %s(%s) is missing or differs from the model (requires rebuild)", meta.table, mfk.from, mfk.table, mfk.to))
		}
	}
	for _, afk := range actual.fks {
		matched := false
		for _, mfk := range modelFks {
			if afk.from == mfk.from && afk.table == mfk.table && afk.to == mfk.to {
				matched = true
				break
			}
		}
		if !matched {
			m.report.Pending = append(m.report.Pending, fmt.Sprintf("foreign key %s.%s -> %s(%s) is not declared by the model", meta.table, afk.from, afk.table, afk.to))
		}
	}
}

// dropStaleTables drops managed tables that no registered model declares.
// Tables are dropped children-first based on PRAGMA foreign_key_list;
// cycles and references from unmanaged tables go to Pending.
func (m *migrator) dropStaleTables(ctx context.Context, snaps map[string]*snapshot, models map[string]*modelInfo) error {
	var candidates []string
	for t := range snaps {
		if isReservedTable(t) {
			continue
		}
		if _, ok := models[t]; !ok {
			candidates = append(candidates, t)
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	cand := make(map[string]bool, len(candidates))
	for _, t := range candidates {
		cand[t] = true
	}
	tables, err := m.listTables(ctx)
	if err != nil {
		return err
	}
	refs := map[string][]string{}
	for _, t := range tables {
		fks, err := m.foreignKeyList(ctx, t)
		if err != nil {
			return err
		}
		for _, fk := range fks {
			if cand[fk.table] {
				refs[fk.table] = append(refs[fk.table], t)
			}
		}
	}
	remaining := map[string]bool{}
	for _, t := range candidates {
		remaining[t] = true
	}
	for len(remaining) > 0 {
		progress := false
		for t := range remaining {
			blocked := false
			for _, ref := range refs[t] {
				if remaining[ref] {
					blocked = true
					break
				}
				exists, err := m.tableExists(ctx, ref)
				if err != nil {
					return err
				}
				if exists {
					// Referenced by a table that is not being dropped
					// (unmanaged table or registered model).
					blocked = true
					break
				}
			}
			if blocked {
				continue
			}
			if !m.destructive {
				m.report.SkippedDestructive = append(m.report.SkippedDestructive, "table "+t)
				delete(remaining, t)
				progress = true
				continue
			}
			if _, err := m.conn.ExecContext(ctx, `DROP TABLE IF EXISTS `+quoteIdent(t)); err != nil {
				return fmt.Errorf("drop table %s: %w", t, err)
			}
			if _, err := m.conn.ExecContext(ctx, `DELETE FROM orm_meta WHERE table_name = ?`, t); err != nil {
				return err
			}
			m.report.DroppedTables = append(m.report.DroppedTables, t)
			m.log("orm: dropped table %s", t)
			delete(remaining, t)
			progress = true
		}
		if !progress {
			for t := range remaining {
				m.report.Pending = append(m.report.Pending, fmt.Sprintf("table %s cannot be dropped: FK cycle or references from tables outside the ORM", t))
			}
			break
		}
	}
	return nil
}
