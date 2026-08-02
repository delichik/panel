package orm

import (
	"database/sql"
	"database/sql/driver"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

type fieldKind int

const (
	kindInvalid fieldKind = iota
	kindString
	kindInt
	kindUint
	kindFloat
	kindBool
	kindTime
	kindBytes
	kindJSON
	kindScanner
	kindAny
)

// fieldInfo is the parsed metadata of one mapped struct field.
type fieldInfo struct {
	goName        string
	column        string
	index         []int
	kind          fieldKind
	nullable      bool
	primaryKey    bool
	autoIncrement bool
	notNull       bool
	unique        bool
	indexed       bool
	hasDefault    bool
	defaultValue  string
	typeOverride  string
	size          int
	json          bool
	refTable      string
	refColumn     string
	onDelete      string
	onUpdate      string
	autoCreate    bool
	autoUpdate    bool
	timeUnix      bool
}

type modelInfo struct {
	typ              reflect.Type
	table            string
	fields           []*fieldInfo
	byColumn         map[string]*fieldInfo
	pk               []*fieldInfo
	tableConstraints []string
	extraIndexDDL    map[string][]string
}

var (
	metaMu      sync.RWMutex
	metaCache   = map[reflect.Type]*modelInfo{}
	registered  []reflect.Type
	tableToMeta = map[string]*modelInfo{}
)

// Register parses and caches model metadata. It is idempotent and returns
// an error for invalid tags or unsupported field types. Only models passed
// to Register are considered by AutoMigrate.
func Register(models ...any) error {
	parsed := make([]*modelInfo, 0, len(models))
	for _, m := range models {
		t := reflect.TypeOf(m)
		if t == nil {
			return fmt.Errorf("orm: cannot register a nil model")
		}
		if t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		info, err := metaFor(t)
		if err != nil {
			return err
		}
		parsed = append(parsed, info)
	}
	metaMu.Lock()
	defer metaMu.Unlock()
	for _, info := range parsed {
		t := info.typ
		found := false
		for _, rt := range registered {
			if rt == t {
				found = true
				break
			}
		}
		if found {
			continue
		}
		metaCache[t] = info
		registered = append(registered, t)
		tableToMeta[info.table] = info
	}
	return nil
}

// metaFor returns the cached metadata for t, parsing and caching it on
// first use. It is safe for concurrent use.
func metaFor(t reflect.Type) (*modelInfo, error) {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	metaMu.RLock()
	if info, ok := metaCache[t]; ok {
		metaMu.RUnlock()
		return info, nil
	}
	metaMu.RUnlock()
	metaMu.Lock()
	defer metaMu.Unlock()
	if info, ok := metaCache[t]; ok {
		return info, nil
	}
	info, err := parseModel(t)
	if err != nil {
		return nil, err
	}
	metaCache[t] = info
	return info, nil
}

// registeredModels returns one model per table (the last registered model
// wins when several models share a table name).
func registeredModels() []*modelInfo {
	metaMu.RLock()
	defer metaMu.RUnlock()
	byTable := map[string]*modelInfo{}
	var order []string
	for _, t := range registered {
		info := metaCache[t]
		if _, ok := byTable[info.table]; !ok {
			order = append(order, info.table)
		}
		byTable[info.table] = info
	}
	out := make([]*modelInfo, 0, len(order))
	for _, table := range order {
		out = append(out, byTable[table])
	}
	return out
}

func registeredModelByTable(table string) *modelInfo {
	metaMu.RLock()
	defer metaMu.RUnlock()
	return tableToMeta[table]
}

// registeredModelValues returns fresh instances of every registered model
// (one per table, the last registration wins), mirroring registeredModels().
// It is used by AutoMigrate to delegate to AutoMigrateModels.
func registeredModelValues() []any {
	infos := registeredModels()
	out := make([]any, 0, len(infos))
	for _, info := range infos {
		out = append(out, reflect.New(info.typ).Interface())
	}
	return out
}

// snakeCase converts a Go identifier to snake_case, e.g. CredentialID ->
// credential_id, OSRelease -> os_release.
func snakeCase(s string) string {
	if s == "" {
		return ""
	}
	runes := []rune(s)
	var b strings.Builder
	for i, r := range runes {
		if unicode.IsUpper(r) {
			prevLower := i > 0 && (unicode.IsLower(runes[i-1]) || unicode.IsDigit(runes[i-1]))
			nextLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
			if i > 0 && (prevLower || nextLower) {
				b.WriteByte('_')
			}
			b.WriteRune(unicode.ToLower(r))
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func parseModel(t reflect.Type) (*modelInfo, error) {
	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("orm: model %v is not a struct", t)
	}
	m := &modelInfo{typ: t, byColumn: map[string]*fieldInfo{}}
	if name, ok := tableNameOf(t); ok {
		m.table = name
	} else {
		m.table = snakeCase(t.Name())
	}
	if m.table == "" {
		return nil, fmt.Errorf("orm: model %v has an empty table name", t)
	}
	m.tableConstraints = tableConstraintsOf(t)
	m.extraIndexDDL = extraIndexDDLOf(t)
	if err := m.parseFields(t, nil); err != nil {
		return nil, err
	}
	if len(m.fields) == 0 {
		return nil, fmt.Errorf("orm: model %v has no mapped fields", t)
	}
	autoIncCount := 0
	for _, f := range m.fields {
		if f.autoIncrement {
			autoIncCount++
			if f.kind != kindInt && f.kind != kindUint {
				return nil, fmt.Errorf("orm: %s.%s: auto_increment requires an integer type", m.table, f.column)
			}
			if !f.primaryKey {
				return nil, fmt.Errorf("orm: %s.%s: auto_increment requires primary_key", m.table, f.column)
			}
		}
		if (f.autoCreate || f.autoUpdate) && f.kind != kindTime {
			return nil, fmt.Errorf("orm: %s.%s: auto_create_time/auto_update_time require time.Time", m.table, f.column)
		}
		if f.timeUnix && f.kind != kindTime {
			return nil, fmt.Errorf("orm: %s.%s: time_format requires time.Time", m.table, f.column)
		}
	}
	if autoIncCount > 1 {
		return nil, fmt.Errorf("orm: %s: at most one auto_increment column is allowed", m.table)
	}
	if autoIncCount > 0 && len(m.pk) > 1 {
		return nil, fmt.Errorf("orm: %s: auto_increment cannot be combined with a composite primary key", m.table)
	}
	return m, nil
}

// TableConstraintsMethod is implemented by models that declare raw
// table-level constraint clauses (e.g. CHECK(...)) to be appended to the
// CREATE TABLE statement and preserved on table rebuilds.
type TableConstraintsMethod interface {
	TableConstraints() []string
}

// ExtraIndexDDLMethod is implemented by models that declare composite,
// partial or composite-unique index DDL that cannot be expressed with orm
// tags. The map is keyed by table name; each value is a complete
// CREATE [UNIQUE] INDEX [IF NOT EXISTS] statement.
type ExtraIndexDDLMethod interface {
	ExtraIndexDDL() map[string][]string
}

func tableConstraintsOf(t reflect.Type) []string {
	tcType := reflect.TypeOf((*TableConstraintsMethod)(nil)).Elem()
	if reflect.PointerTo(t).Implements(tcType) {
		return reflect.New(t).Interface().(TableConstraintsMethod).TableConstraints()
	}
	if t.Implements(tcType) {
		return reflect.Zero(t).Interface().(TableConstraintsMethod).TableConstraints()
	}
	return nil
}

func extraIndexDDLOf(t reflect.Type) map[string][]string {
	exType := reflect.TypeOf((*ExtraIndexDDLMethod)(nil)).Elem()
	if reflect.PointerTo(t).Implements(exType) {
		return reflect.New(t).Interface().(ExtraIndexDDLMethod).ExtraIndexDDL()
	}
	if t.Implements(exType) {
		return reflect.Zero(t).Interface().(ExtraIndexDDLMethod).ExtraIndexDDL()
	}
	return nil
}

// tableNameOf resolves the table name from a TableName() string method
// (preferred) or a lone `table:xxx` tag on a field. Go has no type-level
// tags, so the contract's `table:xxx` is additionally supported on a field.
func tableNameOf(t reflect.Type) (string, bool) {
	tnType := reflect.TypeOf((*interface{ TableName() string })(nil)).Elem()
	if reflect.PointerTo(t).Implements(tnType) {
		name := strings.TrimSpace(reflect.New(t).Interface().(interface{ TableName() string }).TableName())
		if name != "" {
			return name, true
		}
	}
	if t.Implements(tnType) {
		name := strings.TrimSpace(reflect.Zero(t).Interface().(interface{ TableName() string }).TableName())
		if name != "" {
			return name, true
		}
	}
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		if sf.PkgPath != "" {
			continue
		}
		tag := strings.TrimSpace(sf.Tag.Get("orm"))
		if tag == "" {
			continue
		}
		parts := strings.Split(tag, ";")
		if len(parts) == 1 {
			key, val, has := strings.Cut(strings.TrimSpace(parts[0]), ":")
			if has && key == "table" && strings.TrimSpace(val) != "" {
				return strings.TrimSpace(val), true
			}
		}
	}
	return "", false
}

func (m *modelInfo) parseFields(t reflect.Type, prefix []int) error {
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		if sf.PkgPath != "" {
			continue
		}
		tag := strings.TrimSpace(sf.Tag.Get("orm"))
		if tag == "-" {
			continue
		}
		path := append(append([]int{}, prefix...), i)
		if isLoneTableTag(tag) {
			continue
		}
		if tag == "embedded" {
			ft := sf.Type
			if ft.Kind() != reflect.Struct {
				return fmt.Errorf("orm: embedded field %s must be a struct", sf.Name)
			}
			if err := m.parseFields(ft, path); err != nil {
				return err
			}
			continue
		}
		f, err := parseField(sf, path)
		if err != nil {
			return err
		}
		if prev, dup := m.byColumn[f.column]; dup {
			return fmt.Errorf("orm: duplicate column %q (conflicts with %s)", f.column, prev.goName)
		}
		m.byColumn[f.column] = f
		m.fields = append(m.fields, f)
		if f.primaryKey {
			m.pk = append(m.pk, f)
		}
	}
	return nil
}

func isLoneTableTag(tag string) bool {
	parts := strings.Split(strings.TrimSpace(tag), ";")
	if len(parts) != 1 {
		return false
	}
	key, _, has := strings.Cut(strings.TrimSpace(parts[0]), ":")
	return has && key == "table"
}
func parseField(sf reflect.StructField, path []int) (*fieldInfo, error) {
	f := &fieldInfo{goName: sf.Name, index: path}
	tag := strings.TrimSpace(sf.Tag.Get("orm"))
	if err := applyTag(tag, f); err != nil {
		return nil, fmt.Errorf("orm: field %s: %w", sf.Name, err)
	}
	if f.column == "" {
		f.column = snakeCase(sf.Name)
	}
	base := sf.Type
	if base.Kind() == reflect.Pointer {
		f.nullable = true
		base = base.Elem()
	}
	if base.Kind() == reflect.Interface {
		f.kind = kindAny
		return f, nil
	}
	if f.json {
		f.kind = kindJSON
		return f, nil
	}
	if implementsScanner(base) {
		f.kind = kindScanner
		return f, nil
	}
	if base == reflect.TypeOf(time.Time{}) {
		f.kind = kindTime
		return f, nil
	}
	switch base.Kind() {
	case reflect.String:
		f.kind = kindString
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		f.kind = kindInt
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		f.kind = kindUint
	case reflect.Float32, reflect.Float64:
		f.kind = kindFloat
	case reflect.Bool:
		f.kind = kindBool
	case reflect.Slice:
		if base.Elem().Kind() == reflect.Uint8 {
			f.kind = kindBytes
		} else {
			f.kind = kindJSON
		}
	case reflect.Map:
		f.kind = kindJSON
	case reflect.Struct:
		return nil, fmt.Errorf("unsupported struct type %v; use orm:\"json\" or orm:\"embedded\"", sf.Type)
	default:
		return nil, fmt.Errorf("unsupported type %v", sf.Type)
	}
	return f, nil
}

func implementsScanner(t reflect.Type) bool {
	scannerType := reflect.TypeOf((*sql.Scanner)(nil)).Elem()
	valuerType := reflect.TypeOf((*driver.Valuer)(nil)).Elem()
	if t.Implements(scannerType) && t.Implements(valuerType) {
		return true
	}
	pt := reflect.PointerTo(t)
	return pt.Implements(scannerType) && pt.Implements(valuerType)
}

func applyTag(tag string, f *fieldInfo) error {
	if tag == "" {
		return nil
	}
	for _, part := range strings.Split(tag, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		key, val, hasVal := strings.Cut(part, ":")
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		switch key {
		case "column":
			if !hasVal || val == "" {
				return fmt.Errorf("column tag requires a value")
			}
			f.column = val
		case "primary_key":
			f.primaryKey = true
		case "auto_increment":
			f.autoIncrement = true
		case "not_null":
			f.notNull = true
		case "unique":
			f.unique = true
		case "index":
			f.indexed = true
		case "json":
			f.json = true
		case "auto_create_time":
			f.autoCreate = true
		case "auto_update_time":
			f.autoUpdate = true
		case "embedded":
			return fmt.Errorf("embedded must be the only tag on the field")
		case "default":
			if !hasVal {
				return fmt.Errorf("default tag requires a value")
			}
			f.hasDefault = true
			f.defaultValue = val
		case "type":
			if !hasVal || val == "" {
				return fmt.Errorf("type tag requires a value")
			}
			f.typeOverride = val
		case "size":
			n, err := strconv.Atoi(val)
			if err != nil || n <= 0 {
				return fmt.Errorf("size tag requires a positive integer")
			}
			f.size = n
		case "references":
			table, col, err := parseReference(val)
			if err != nil {
				return err
			}
			f.refTable, f.refColumn = table, col
		case "on_delete":
			if !hasVal || !validAction(val) {
				return fmt.Errorf("on_delete has invalid action %q", val)
			}
			f.onDelete = val
		case "on_update":
			if !hasVal || !validAction(val) {
				return fmt.Errorf("on_update has invalid action %q", val)
			}
			f.onUpdate = val
		case "time_format":
			if val != "unix" {
				return fmt.Errorf("unsupported time_format %q", val)
			}
			f.timeUnix = true
		case "table":
			return fmt.Errorf("table tag is only valid as the sole tag on a field")
		default:
			return fmt.Errorf("unknown tag %q", key)
		}
	}
	return nil
}

func parseReference(val string) (string, string, error) {
	table, rest, ok := strings.Cut(val, "(")
	if !ok {
		return "", "", fmt.Errorf("references tag must be table(column)")
	}
	col := strings.TrimSuffix(rest, ")")
	table = strings.TrimSpace(table)
	col = strings.TrimSpace(col)
	if table == "" || col == "" || strings.Contains(col, ")") {
		return "", "", fmt.Errorf("references tag must be table(column)")
	}
	return table, col, nil
}

func validAction(a string) bool {
	switch strings.ToUpper(a) {
	case "RESTRICT", "CASCADE", "SET NULL", "NO ACTION", "SET DEFAULT":
		return true
	}
	return false
}
