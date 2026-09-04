package orm

import (
	"database/sql"
	"database/sql/driver"
	"reflect"
	"strings"
	"testing"
	"time"
)

type modelSample struct {
	ID           string            `orm:"primary_key"`
	Name         string            `orm:"not_null"`
	CredentialID string            `orm:"not_null;references:credentials(id);on_delete:RESTRICT"`
	Traits       map[string]string `orm:"json"`
	Reachable    bool              `orm:"not_null;default:0"`
	Score        float64
	Blob         []byte
	Payload      any
	CreatedAt    time.Time `orm:"auto_create_time"`
	UpdatedAt    time.Time `orm:"auto_update_time"`
	Skip         string    `orm:"-"`
	unexported   string
}

func TestParseModelBasic(t *testing.T) {
	meta, err := metaFor(reflect.TypeOf(modelSample{}))
	if err != nil {
		t.Fatal(err)
	}
	if meta.table != "model_sample" {
		t.Fatalf("table = %q", meta.table)
	}
	if meta.byColumn["name"].goName != "Name" || !meta.byColumn["name"].notNull {
		t.Fatalf("name field = %+v", meta.byColumn["name"])
	}
	if meta.byColumn["created_at"].kind != kindTime || !meta.byColumn["created_at"].autoCreate {
		t.Fatalf("created_at field = %+v", meta.byColumn["created_at"])
	}
	if meta.byColumn["traits"].kind != kindJSON {
		t.Fatalf("traits kind = %d", meta.byColumn["traits"].kind)
	}
	if meta.byColumn["payload"].kind != kindAny {
		t.Fatalf("payload kind = %d", meta.byColumn["payload"].kind)
	}
	if meta.byColumn["blob"].kind != kindBytes {
		t.Fatalf("blob kind = %d", meta.byColumn["blob"].kind)
	}
	if meta.byColumn["credential_id"].refTable != "credentials" || meta.byColumn["credential_id"].refColumn != "id" {
		t.Fatalf("credential_id ref = %+v", meta.byColumn["credential_id"])
	}
	if _, ok := meta.byColumn["skip"]; ok {
		t.Fatal("skip field should be ignored")
	}
	if _, ok := meta.byColumn["unexported"]; ok {
		t.Fatal("unexported field should be ignored")
	}
	if len(meta.pk) != 1 || meta.pk[0].column != "id" {
		t.Fatalf("pk = %+v", meta.pk)
	}
}

func TestSnakeCase(t *testing.T) {
	cases := map[string]string{
		"ID":           "id",
		"Name":         "name",
		"CredentialID": "credential_id",
		"OSRelease":    "os_release",
		"SSHUsername":  "ssh_username",
		"HostServerID": "host_server_id",
		"TLS":          "tls",
		"createdAt":    "created_at",
		"created_at":   "created_at",
	}
	for in, want := range cases {
		if got := snakeCase(in); got != want {
			t.Errorf("snakeCase(%q) = %q, want %q", in, got, want)
		}
	}
}

type modelTagTable struct {
	ID   string `orm:"primary_key"`
	Name string `orm:"column:display_name"`
}

func (modelTagTable) TableName() string { return "custom_tab" }

type modelTagTableField struct {
	ID   string   `orm:"primary_key"`
	Meta struct{} `orm:"table:field_table"`
}

type modelEmbedded struct {
	OS      string
	Version string `orm:"column:os_version"`
}

type modelWithEmbed struct {
	ID       string        `orm:"primary_key"`
	Embedded modelEmbedded `orm:"embedded"`
}

type modelEmbedConflict struct {
	ID    string `orm:"primary_key"`
	OS    string
	Inner struct {
		OS string
	} `orm:"embedded"`
}

func TestParseModelTableNames(t *testing.T) {
	meta, err := metaFor(reflect.TypeOf(modelTagTable{}))
	if err != nil {
		t.Fatal(err)
	}
	if meta.table != "custom_tab" {
		t.Fatalf("table = %q", meta.table)
	}
	if meta.byColumn["display_name"].goName != "Name" {
		t.Fatalf("column = %q", meta.byColumn["display_name"].column)
	}
	meta2, err := metaFor(reflect.TypeOf(modelTagTableField{}))
	if err != nil {
		t.Fatal(err)
	}
	if meta2.table != "field_table" {
		t.Fatalf("table = %q", meta2.table)
	}
}

func TestParseModelEmbedded(t *testing.T) {
	meta, err := metaFor(reflect.TypeOf(modelWithEmbed{}))
	if err != nil {
		t.Fatal(err)
	}
	osField := meta.byColumn["os"]
	if osField == nil {
		t.Fatal("embedded os column missing")
	}
	if osField.kind != kindString {
		t.Fatalf("os kind = %d", osField.kind)
	}
	if len(osField.index) != 2 || osField.index[0] != 1 || osField.index[1] != 0 {
		t.Fatalf("os index path = %v", osField.index)
	}
	if meta.byColumn["os_version"] == nil {
		t.Fatal("embedded os_version column missing")
	}
	if _, err := metaFor(reflect.TypeOf(modelEmbedConflict{})); err == nil {
		t.Fatal("expected duplicate column error")
	}
}

type modelNullable struct {
	ID   string  `orm:"primary_key"`
	Name *string `orm:"column:name"`
	Num  *int
}

func TestParseModelNullable(t *testing.T) {
	meta, err := metaFor(reflect.TypeOf(modelNullable{}))
	if err != nil {
		t.Fatal(err)
	}
	if !meta.byColumn["name"].nullable {
		t.Fatal("name should be nullable")
	}
	if meta.byColumn["name"].kind != kindString {
		t.Fatal("name kind should be string")
	}
	if !meta.byColumn["num"].nullable {
		t.Fatal("num should be nullable")
	}
}

type modelBadTags struct {
	ID string `orm:"primary_key;bogus_flag"`
}

type modelBadAutoInc struct {
	ID   string `orm:"primary_key;auto_increment"`
	Name string
}

type modelBadRef struct {
	ID  string `orm:"primary_key"`
	FID string `orm:"references:broken"`
}

func TestParseModelErrors(t *testing.T) {
	if _, err := metaFor(reflect.TypeOf(modelBadTags{})); err == nil || !strings.Contains(err.Error(), "bogus_flag") {
		t.Fatalf("expected unknown tag error, got %v", err)
	}
	if _, err := metaFor(reflect.TypeOf(modelBadAutoInc{})); err == nil {
		t.Fatal("expected auto_increment type error")
	}
	if _, err := metaFor(reflect.TypeOf(modelBadRef{})); err == nil {
		t.Fatal("expected references parse error")
	}
	if err := Register(42); err == nil {
		t.Fatal("expected non-struct register error")
	}
}

type modelTimeUnix struct {
	ID        string    `orm:"primary_key"`
	CreatedAt time.Time `orm:"time_format:unix"`
}

type modelBadTime struct {
	ID string `orm:"primary_key"`
	N  int    `orm:"time_format:unix"`
}

func TestParseModelTimeFormats(t *testing.T) {
	meta, err := metaFor(reflect.TypeOf(modelTimeUnix{}))
	if err != nil {
		t.Fatal(err)
	}
	if !meta.byColumn["created_at"].timeUnix {
		t.Fatal("created_at should be unix format")
	}
	if _, err := metaFor(reflect.TypeOf(modelBadTime{})); err == nil {
		t.Fatal("expected time_format type error")
	}
}

type csvTags []string

func (c *csvTags) Scan(v any) error {
	if v == nil {
		*c = nil
		return nil
	}
	s, ok := v.(string)
	if !ok {
		if b, ok2 := v.([]byte); ok2 {
			s = string(b)
		} else {
			return sql.ErrNoRows
		}
	}
	*c = strings.Split(s, ",")
	return nil
}

func (c csvTags) Value() (driver.Value, error) {
	return strings.Join(c, ","), nil
}

type modelScanner struct {
	ID   string `orm:"primary_key"`
	Tags csvTags
}

func TestParseModelScanner(t *testing.T) {
	meta, err := metaFor(reflect.TypeOf(modelScanner{}))
	if err != nil {
		t.Fatal(err)
	}
	if meta.byColumn["tags"].kind != kindScanner {
		t.Fatalf("tags kind = %d", meta.byColumn["tags"].kind)
	}
}

type modelDefaultSize struct {
	ID   string `orm:"primary_key;type:TEXT;size:64"`
	Note string `orm:"type:VARCHAR;size:255"`
}

func TestParseModelTypeOverride(t *testing.T) {
	meta, err := metaFor(reflect.TypeOf(modelDefaultSize{}))
	if err != nil {
		t.Fatal(err)
	}
	if meta.byColumn["id"].typeOverride != "TEXT" || meta.byColumn["id"].size != 64 {
		t.Fatalf("id = %+v", meta.byColumn["id"])
	}
	if meta.byColumn["note"].typeOverride != "VARCHAR" {
		t.Fatalf("note = %+v", meta.byColumn["note"])
	}
}

type modelAutoInc struct {
	ID   int64 `orm:"primary_key;auto_increment"`
	Name string
}

func TestParseModelAutoIncrement(t *testing.T) {
	meta, err := metaFor(reflect.TypeOf(modelAutoInc{}))
	if err != nil {
		t.Fatal(err)
	}
	if !meta.byColumn["id"].autoIncrement {
		t.Fatal("id should be auto increment")
	}
}

func TestRegisterIdempotent(t *testing.T) {
	if err := Register(&modelAutoInc{}); err != nil {
		t.Fatal(err)
	}
	if err := Register(modelAutoInc{}); err != nil {
		t.Fatal(err)
	}
	got := 0
	for _, m := range registeredModels() {
		if m.table == "model_auto_inc" {
			got++
		}
	}
	if got != 1 {
		t.Fatalf("model_auto_inc registered %d times", got)
	}
}

func TestLikeEscaped(t *testing.T) {
	got := LikeEscaped(`100%_x\y`)
	want := `%100\%\_x\\y%`
	if got != want {
		t.Fatalf("LikeEscaped = %q, want %q", got, want)
	}
}
