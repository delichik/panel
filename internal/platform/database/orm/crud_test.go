package orm

import (
	"context"
	"database/sql"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	return openTestDBPath(t, "test.db", false)
}

func openTestDBFK(t *testing.T) *sql.DB {
	t.Helper()
	return openTestDBPath(t, "test.db", true)
}

func openTestDBPath(t *testing.T, name string, fk bool) *sql.DB {
	t.Helper()
	dsn := "file:" + filepath.ToSlash(filepath.Join(t.TempDir(), name))
	if fk {
		dsn += "?_pragma=foreign_keys(1)"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(4)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

type crudUser struct {
	ID        int64  `orm:"primary_key;auto_increment"`
	Name      string `orm:"not_null"`
	Email     string
	Active    bool `orm:"not_null;default:1"`
	Score     float64
	Blob      []byte
	Tags      []string       `orm:"json"`
	Profile   map[string]any `orm:"json"`
	CreatedAt time.Time      `orm:"auto_create_time"`
	UpdatedAt time.Time      `orm:"auto_update_time"`
}

func (crudUser) TableName() string { return "crud_users" }

type crudNote struct {
	ID   string `orm:"primary_key"`
	Body *string
}

func (crudNote) TableName() string { return "crud_notes" }

type crudEmbed struct {
	ID    string `orm:"primary_key"`
	Label string
	Inner struct {
		OS      string
		Version string `orm:"column:os_version"`
	} `orm:"embedded"`
}

func (crudEmbed) TableName() string { return "crud_embeds" }

type crudUnix struct {
	ID string    `orm:"primary_key"`
	At time.Time `orm:"time_format:unix"`
}

func (crudUnix) TableName() string { return "crud_unix" }

func TestCRUDRoundTrip(t *testing.T) {
	db := openTestDB(t)
	if err := Register(&crudUser{}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE crud_users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		email TEXT,
		active INTEGER NOT NULL DEFAULT 1,
		score REAL,
		blob BLOB,
		tags TEXT,
		profile TEXT,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`); err != nil {
		t.Fatal(err)
	}

	u := &crudUser{Name: "alice", Email: "a@example.com", Active: true, Score: 9.5, Blob: []byte{1, 2, 3}, Tags: []string{"x", "y"}, Profile: map[string]any{"role": "admin"}}
	if err := Insert(context.Background(), db, u); err != nil {
		t.Fatal(err)
	}
	if u.ID == 0 {
		t.Fatal("auto_increment id not backfilled")
	}
	if u.CreatedAt.IsZero() || u.UpdatedAt.IsZero() {
		t.Fatal("auto times not filled")
	}

	var got crudUser
	if err := New(db).From("crud_users").First(context.Background(), &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != u.ID || got.Name != "alice" || !got.Active || got.Score != 9.5 {
		t.Fatalf("round trip = %+v", got)
	}
	if !reflect.DeepEqual(got.Blob, []byte{1, 2, 3}) {
		t.Fatalf("blob = %v", got.Blob)
	}
	if !reflect.DeepEqual(got.Tags, []string{"x", "y"}) || got.Profile["role"] != "admin" {
		t.Fatalf("json = %v %v", got.Tags, got.Profile)
	}
	if !got.CreatedAt.Equal(u.CreatedAt) {
		t.Fatalf("created_at = %v", got.CreatedAt)
	}

	// raw storage format is RFC3339Nano UTC
	var raw string
	if err := db.QueryRow(`SELECT created_at FROM crud_users WHERE id = ?`, u.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(raw, "Z") {
		t.Fatalf("created_at raw = %q", raw)
	}
	if _, err := time.Parse(time.RFC3339Nano, raw); err != nil {
		t.Fatalf("created_at raw not parseable: %q", raw)
	}

	// update by pk
	u.Name = "bob"
	u.Email = ""
	before := u.UpdatedAt
	if err := Update(context.Background(), db, u); err != nil {
		t.Fatal(err)
	}
	if !u.UpdatedAt.After(before) {
		t.Fatal("auto_update_time not stamped")
	}
	var name string
	if err := db.QueryRow(`SELECT name FROM crud_users WHERE id = ?`, u.ID).Scan(&name); err != nil {
		t.Fatal(err)
	}
	if name != "bob" {
		t.Fatalf("updated name = %q", name)
	}

	// delete by pk
	if err := Delete(context.Background(), db, u); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM crud_users`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("after delete count = %d, err = %v", n, err)
	}
}

func TestCRUDTerminalMethods(t *testing.T) {
	db := openTestDB(t)
	if err := Register(&crudUser{}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE crud_users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		email TEXT,
		active INTEGER NOT NULL DEFAULT 1,
		score REAL,
		blob BLOB,
		tags TEXT,
		profile TEXT,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		u := &crudUser{Name: "u" + string(rune('a'+i)), Score: float64(i)}
		if err := Insert(ctx, db, u); err != nil {
			t.Fatal(err)
		}
	}

	var all []crudUser
	if err := New(db).From("crud_users").OrderBy("id").All(ctx, &all); err != nil {
		t.Fatal(err)
	}
	if len(all) != 5 || all[0].Name != "ua" || all[4].Name != "ue" {
		t.Fatalf("all = %+v", all)
	}

	var first crudUser
	if err := New(db).From("crud_users").OrderBy("id DESC").First(ctx, &first); err != nil {
		t.Fatal(err)
	}
	if first.Name != "ue" {
		t.Fatalf("first = %+v", first)
	}

	var one crudUser
	if err := New(db).From("crud_users").Where("name = ?", "ub").One(ctx, &one); err != nil {
		t.Fatal(err)
	}
	if one.Name != "ub" {
		t.Fatalf("one = %+v", one)
	}
	if err := New(db).From("crud_users").Where("name = ?", "missing").One(ctx, &one); err != sql.ErrNoRows {
		t.Fatalf("one missing err = %v", err)
	}
	if err := New(db).From("crud_users").Where("1=1").One(ctx, &one); err == nil {
		t.Fatal("expected multiple rows error")
	}

	n, err := New(db).From("crud_users").Count(ctx)
	if err != nil || n != 5 {
		t.Fatalf("count = %d, err = %v", n, err)
	}
	n2, err := New(db).From("crud_users").Where("score >= ?", 2).Count(ctx)
	if err != nil || n2 != 3 {
		t.Fatalf("count filtered = %d, err = %v", n2, err)
	}

	ex, err := New(db).From("crud_users").Where("name = ?", "ua").Exists(ctx)
	if err != nil || !ex {
		t.Fatalf("exists = %v, err = %v", ex, err)
	}
	ex2, err := New(db).From("crud_users").Where("name = ?", "zz").Exists(ctx)
	if err != nil || ex2 {
		t.Fatalf("exists2 = %v, err = %v", ex2, err)
	}

	var names []string
	if err := New(db).From("crud_users").OrderBy("id").Pluck(ctx, "name", &names); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(names, []string{"ua", "ub", "uc", "ud", "ue"}) {
		t.Fatalf("pluck = %v", names)
	}

	var maxScore float64
	if err := New(db).From("crud_users").SelectExpr("MAX(score)").ScanValue(ctx, &maxScore); err != nil {
		t.Fatal(err)
	}
	if maxScore != 4 {
		t.Fatalf("max = %v", maxScore)
	}

	var cnt int64
	if err := New(db).From("crud_users").SelectExpr("COUNT(*)").ScanValue(ctx, &cnt); err != nil {
		t.Fatal(err)
	}
	if cnt != 5 {
		t.Fatalf("scan count = %d", cnt)
	}

	// pagination
	var page []crudUser
	if err := New(db).From("crud_users").OrderBy("id").Limit(2).Offset(2).All(ctx, &page); err != nil {
		t.Fatal(err)
	}
	if len(page) != 2 || page[0].Name != "uc" {
		t.Fatalf("page = %+v", page)
	}

	// UpdateColumns with safety checks
	if err := New(db).From("crud_users").UpdateColumns(ctx, map[string]any{"name": "zz"}); err == nil {
		t.Fatal("UpdateColumns without WHERE should fail")
	}
	if err := New(db).From("crud_users").Where("name = ?", "ua").UpdateColumns(ctx, map[string]any{"name": "renamed"}); err != nil {
		t.Fatal(err)
	}
	var renamed string
	if err := db.QueryRow(`SELECT name FROM crud_users WHERE name = ?`, "renamed").Scan(&renamed); err != nil {
		t.Fatalf("updated column not found: %v", err)
	}
	if err := New(db).From("crud_users").Where("name = ?", "ua").UpdateColumns(ctx, map[string]any{"nope": 1}); err == nil {
		t.Fatal("UpdateColumns with unknown column should fail")
	}

	// Delete safety
	if err := New(db).From("crud_users").Delete(ctx); err == nil {
		t.Fatal("Delete without WHERE should fail")
	}
	if err := New(db).From("crud_users").Where("name = ?", "renamed").Delete(ctx); err != nil {
		t.Fatal(err)
	}
	if n, _ := New(db).From("crud_users").Count(ctx); n != 4 {
		t.Fatalf("after delete count = %d", n)
	}
}

func TestCRUDLikeEscaped(t *testing.T) {
	db := openTestDB(t)
	if err := Register(&crudUser{}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE crud_users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		email TEXT,
		active INTEGER NOT NULL DEFAULT 1,
		score REAL,
		blob BLOB,
		tags TEXT,
		profile TEXT,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, name := range []string{"100%_x", "100ax", "plain"} {
		if err := Insert(ctx, db, &crudUser{Name: name}); err != nil {
			t.Fatal(err)
		}
	}
	var names []string
	if err := New(db).From("crud_users").Where("name LIKE ? ESCAPE '\\'", LikeEscaped("100%_x")).Pluck(ctx, "name", &names); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(names, []string{"100%_x"}) {
		t.Fatalf("like escaped matches = %v", names)
	}
}

func TestCRUDInsertBatch(t *testing.T) {
	db := openTestDB(t)
	if err := Register(&crudUser{}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE crud_users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		email TEXT,
		active INTEGER NOT NULL DEFAULT 1,
		score REAL,
		blob BLOB,
		tags TEXT,
		profile TEXT,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	models := make([]crudUser, 0, 1001)
	for i := 0; i < 1001; i++ {
		models = append(models, crudUser{Name: "n" + string(rune('0'+i%10))})
	}
	if err := InsertBatch(ctx, db, models); err != nil {
		t.Fatal(err)
	}
	n, err := New(db).From("crud_users").Count(ctx)
	if err != nil || n != 1001 {
		t.Fatalf("batch count = %d, err = %v", n, err)
	}
	ptrs := make([]*crudUser, 3)
	for i := range ptrs {
		ptrs[i] = &crudUser{Name: "ptr"}
	}
	if err := New(db).From("crud_users").InsertBatch(ctx, ptrs); err != nil {
		t.Fatal(err)
	}
	if n, _ := New(db).From("crud_users").Count(ctx); n != 1004 {
		t.Fatalf("ptr batch count = %d", n)
	}
}

func TestCRUDNullAndScanner(t *testing.T) {
	db := openTestDB(t)
	if err := Register(&crudNote{}, &modelScanner{}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE crud_notes (
		id TEXT PRIMARY KEY,
		body TEXT
	)`); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	nilBody := &crudNote{ID: "n1"}
	if err := Insert(ctx, db, nilBody); err != nil {
		t.Fatal(err)
	}
	text := "hello"
	withBody := &crudNote{ID: "n2", Body: &text}
	if err := Insert(ctx, db, withBody); err != nil {
		t.Fatal(err)
	}
	var got crudNote
	if err := New(db).From("crud_notes").Where("id = ?", "n1").First(ctx, &got); err != nil {
		t.Fatal(err)
	}
	if got.Body != nil {
		t.Fatalf("expected nil body, got %v", *got.Body)
	}
	if err := New(db).From("crud_notes").Where("id = ?", "n2").First(ctx, &got); err != nil {
		t.Fatal(err)
	}
	if got.Body == nil || *got.Body != "hello" {
		t.Fatalf("body = %v", got.Body)
	}

	if _, err := db.Exec(`CREATE TABLE model_scanner (
		id TEXT PRIMARY KEY,
		tags TEXT
	)`); err != nil {
		t.Fatal(err)
	}
	sc := &modelScanner{ID: "s1", Tags: csvTags{"a", "b", "c"}}
	if err := Insert(ctx, db, sc); err != nil {
		t.Fatal(err)
	}
	var sc2 modelScanner
	if err := New(db).From("model_scanner").First(ctx, &sc2); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(sc2.Tags, csvTags{"a", "b", "c"}) {
		t.Fatalf("scanner tags = %v", sc2.Tags)
	}
}

func TestCRUDEmbedded(t *testing.T) {
	db := openTestDB(t)
	if err := Register(&crudEmbed{}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE crud_embeds (
		id TEXT PRIMARY KEY,
		label TEXT,
		os TEXT,
		os_version TEXT
	)`); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	m := &crudEmbed{ID: "e1", Label: "l", Inner: struct {
		OS      string
		Version string `orm:"column:os_version"`
	}{OS: "linux", Version: "v1"}}
	if err := Insert(ctx, db, m); err != nil {
		t.Fatal(err)
	}
	var got crudEmbed
	if err := New(db).From("crud_embeds").First(ctx, &got); err != nil {
		t.Fatal(err)
	}
	if got.Inner.OS != "linux" || got.Inner.Version != "v1" {
		t.Fatalf("embedded = %+v", got.Inner)
	}
}

func TestCRUDUnixTime(t *testing.T) {
	db := openTestDB(t)
	if err := Register(&crudUnix{}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE crud_unix (
		id TEXT PRIMARY KEY,
		at INTEGER
	)`); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	now := time.Unix(1700000000, 0)
	m := &crudUnix{ID: "t1", At: now}
	if err := Insert(ctx, db, m); err != nil {
		t.Fatal(err)
	}
	var raw int64
	if err := db.QueryRow(`SELECT at FROM crud_unix WHERE id = ?`, "t1").Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if raw != now.Unix() {
		t.Fatalf("unix stored = %d", raw)
	}
	var got crudUnix
	if err := New(db).From("crud_unix").First(ctx, &got); err != nil {
		t.Fatal(err)
	}
	if !got.At.Equal(now) {
		t.Fatalf("unix read = %v", got.At)
	}
}

func TestCRUDWithTx(t *testing.T) {
	db := openTestDB(t)
	if err := Register(&crudUser{}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE crud_users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		email TEXT,
		active INTEGER NOT NULL DEFAULT 1,
		score REAL,
		blob BLOB,
		tags TEXT,
		profile TEXT,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	err := WithTx(ctx, db, func(tx *sql.Tx) error {
		if err := Insert(ctx, tx, &crudUser{Name: "tx1"}); err != nil {
			return err
		}
		if err := Insert(ctx, tx, &crudUser{Name: "tx2"}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	n, _ := New(db).From("crud_users").Count(ctx)
	if n != 2 {
		t.Fatalf("committed count = %d", n)
	}

	rollbackErr := WithTx(ctx, db, func(tx *sql.Tx) error {
		if err := Insert(ctx, tx, &crudUser{Name: "tx3"}); err != nil {
			return err
		}
		return context.DeadlineExceeded
	})
	if rollbackErr == nil {
		t.Fatal("expected rollback error")
	}
	n, _ = New(db).From("crud_users").Count(ctx)
	if n != 2 {
		t.Fatalf("after rollback count = %d", n)
	}

	// query builder works on a transaction
	err = WithTx(ctx, db, func(tx *sql.Tx) error {
		var names []string
		if err := New(tx).From("crud_users").OrderBy("id").Pluck(ctx, "name", &names); err != nil {
			return err
		}
		if !reflect.DeepEqual(names, []string{"tx1", "tx2"}) {
			t.Fatalf("tx pluck = %v", names)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCRUDRawEscapeHatch(t *testing.T) {
	db := openTestDB(t)
	if _, err := db.Exec(`CREATE TABLE raw_t (id TEXT PRIMARY KEY, v TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO raw_t (id, v) VALUES (?, ?)`, "a", "x"); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	rows, err := Raw(ctx, db, `SELECT id FROM raw_t WHERE v = ?`, "x")
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if !reflect.DeepEqual(ids, []string{"a"}) {
		t.Fatalf("raw ids = %v", ids)
	}
	res, err := RawExec(ctx, db, `UPDATE raw_t SET v = ? WHERE id = ?`, "y", "a")
	if err != nil {
		t.Fatal(err)
	}
	if ra, _ := res.RowsAffected(); ra != 1 {
		t.Fatalf("raw exec rows = %d", ra)
	}
	var v string
	if err := RawRow(ctx, db, `SELECT v FROM raw_t WHERE id = ?`, "a").Scan(&v); err != nil {
		t.Fatal(err)
	}
	if v != "y" {
		t.Fatalf("raw row = %q", v)
	}
}
