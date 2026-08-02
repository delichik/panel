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
)

func modelMetaValue(model any) (*modelInfo, reflect.Value, error) {
	rv := reflect.ValueOf(model)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return nil, reflect.Value{}, fmt.Errorf("orm: model must be a non-nil pointer to a struct")
	}
	elem := rv.Elem()
	if elem.Kind() != reflect.Struct {
		return nil, reflect.Value{}, fmt.Errorf("orm: model must point to a struct, got %s", elem.Kind())
	}
	meta, err := metaFor(elem.Type())
	if err != nil {
		return nil, reflect.Value{}, err
	}
	return meta, elem, nil
}

func (q *Query) tableOr(fallback string) string {
	if q.table != "" {
		return q.table
	}
	return fallback
}

// All scans all matching rows into dest (*[]T or *[]*T).
func (q *Query) All(ctx context.Context, dest any) error {
	if q.err != nil {
		return q.err
	}
	if q.table == "" {
		return fmt.Errorf("orm: no table selected")
	}
	rv := reflect.ValueOf(dest)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return fmt.Errorf("orm: All dest must be a non-nil pointer to a slice")
	}
	slice := rv.Elem()
	if slice.Kind() != reflect.Slice {
		return fmt.Errorf("orm: All dest must be *[]T")
	}
	stmt, args := q.buildSelect(true)
	out, err := scanSlice(ctx, q.exec, stmt, args, slice.Type())
	if err != nil {
		return err
	}
	slice.Set(out)
	return nil
}

func scanSlice(ctx context.Context, ex Executor, stmt string, args []any, sliceType reflect.Type) (reflect.Value, error) {
	rows, err := ex.QueryContext(ctx, stmt, args...)
	if err != nil {
		return reflect.Value{}, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return reflect.Value{}, err
	}
	elemType := sliceType.Elem()
	ptrElem := elemType.Kind() == reflect.Pointer
	if ptrElem {
		elemType = elemType.Elem()
	}
	meta, err := metaFor(elemType)
	if err != nil {
		return reflect.Value{}, err
	}
	out := reflect.MakeSlice(sliceType, 0, 0)
	for rows.Next() {
		elem := reflect.New(elemType).Elem()
		targets, plan, err := scanTargets(elem, meta, cols)
		if err != nil {
			return reflect.Value{}, err
		}
		if err := rows.Scan(targets...); err != nil {
			return reflect.Value{}, err
		}
		for _, t := range plan {
			if t.field == nil || t.raw == nil {
				continue
			}
			if err := applyRaw(elem.FieldByIndex(t.field.index), t.field, *t.raw); err != nil {
				return reflect.Value{}, err
			}
		}
		if ptrElem {
			p := reflect.New(elemType)
			p.Elem().Set(elem)
			out = reflect.Append(out, p)
		} else {
			out = reflect.Append(out, elem)
		}
	}
	if err := rows.Err(); err != nil {
		return reflect.Value{}, err
	}
	return out, nil
}

// First scans the first row into dest (*T). Returns sql.ErrNoRows when the
// result set is empty.
func (q *Query) First(ctx context.Context, dest any) error {
	if q.err != nil {
		return q.err
	}
	if q.table == "" {
		return fmt.Errorf("orm: no table selected")
	}
	rv := reflect.ValueOf(dest)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return fmt.Errorf("orm: First dest must be a non-nil pointer to a struct")
	}
	stmt, args := q.buildSelect(false)
	stmt += " LIMIT 1"
	out, err := scanSlice(ctx, q.exec, stmt, args, reflect.SliceOf(rv.Elem().Type()))
	if err != nil {
		return err
	}
	if out.Len() == 0 {
		return sql.ErrNoRows
	}
	rv.Elem().Set(out.Index(0))
	return nil
}

// One scans a single row into dest (*T). Returns sql.ErrNoRows when the
// result set is empty and an error when more than one row matches.
func (q *Query) One(ctx context.Context, dest any) error {
	if q.err != nil {
		return q.err
	}
	if q.table == "" {
		return fmt.Errorf("orm: no table selected")
	}
	rv := reflect.ValueOf(dest)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return fmt.Errorf("orm: One dest must be a non-nil pointer to a struct")
	}
	stmt, args := q.buildSelect(false)
	stmt += " LIMIT 2"
	out, err := scanSlice(ctx, q.exec, stmt, args, reflect.SliceOf(rv.Elem().Type()))
	if err != nil {
		return err
	}
	switch out.Len() {
	case 0:
		return sql.ErrNoRows
	case 1:
		rv.Elem().Set(out.Index(0))
		return nil
	default:
		return fmt.Errorf("orm: expected exactly one row, got %d", out.Len())
	}
}

// Count returns the number of rows the query would return.
func (q *Query) Count(ctx context.Context) (int64, error) {
	if q.err != nil {
		return 0, q.err
	}
	if q.table == "" {
		return 0, fmt.Errorf("orm: no table selected")
	}
	stmt, args := q.countSQL()
	var n int64
	if err := q.exec.QueryRowContext(ctx, stmt, args...).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// Exists reports whether at least one row matches.
func (q *Query) Exists(ctx context.Context) (bool, error) {
	if q.err != nil {
		return false, q.err
	}
	if q.table == "" {
		return false, fmt.Errorf("orm: no table selected")
	}
	stmt, args := q.existsSQL()
	rows, err := q.exec.QueryContext(ctx, stmt, args...)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	return rows.Next(), nil
}

// Pluck scans the first column of every row into dest (*[]string,
// *[]int64, ...).
func (q *Query) Pluck(ctx context.Context, column string, dest any) error {
	if q.err != nil {
		return q.err
	}
	if q.table == "" {
		return fmt.Errorf("orm: no table selected")
	}
	rv := reflect.ValueOf(dest)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return fmt.Errorf("orm: Pluck dest must be a non-nil pointer to a slice")
	}
	slice := rv.Elem()
	if slice.Kind() != reflect.Slice {
		return fmt.Errorf("orm: Pluck dest must be *[]T")
	}
	elemType := slice.Type().Elem()
	kind, err := pluckKind(elemType)
	if err != nil {
		return err
	}
	stmt, args := q.pluckSQL(column)
	rows, err := q.exec.QueryContext(ctx, stmt, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	out := reflect.MakeSlice(slice.Type(), 0, 0)
	for rows.Next() {
		var raw any
		if err := rows.Scan(&raw); err != nil {
			return err
		}
		elem := reflect.New(elemType).Elem()
		if err := applyRaw(elem, &fieldInfo{kind: kind}, raw); err != nil {
			return err
		}
		out = reflect.Append(out, elem)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	slice.Set(out)
	return nil
}

// ScanValue scans the first column of the first row into dest
// (*string, *int64, ...). Returns sql.ErrNoRows when no row matches.
func (q *Query) ScanValue(ctx context.Context, dest any) error {
	if q.err != nil {
		return q.err
	}
	if q.table == "" {
		return fmt.Errorf("orm: no table selected")
	}
	rv := reflect.ValueOf(dest)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return fmt.Errorf("orm: ScanValue dest must be a non-nil pointer")
	}
	kind, err := pluckKind(rv.Elem().Type())
	if err != nil {
		return err
	}
	stmt, args := q.scanValueSQL()
	rows, err := q.exec.QueryContext(ctx, stmt, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return err
		}
		return sql.ErrNoRows
	}
	var raw any
	if err := rows.Scan(&raw); err != nil {
		return err
	}
	return applyRaw(rv.Elem(), &fieldInfo{kind: kind}, raw)
}

func pluckKind(t reflect.Type) (fieldKind, error) {
	base := t
	if base.Kind() == reflect.Pointer {
		base = base.Elem()
	}
	if base == reflect.TypeOf(time.Time{}) {
		return kindTime, nil
	}
	switch base.Kind() {
	case reflect.String:
		return kindString, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return kindInt, nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return kindUint, nil
	case reflect.Float32, reflect.Float64:
		return kindFloat, nil
	case reflect.Bool:
		return kindBool, nil
	case reflect.Slice:
		if base.Elem().Kind() == reflect.Uint8 {
			return kindBytes, nil
		}
	case reflect.Interface:
		return kindAny, nil
	}
	return kindInvalid, fmt.Errorf("orm: unsupported pluck element type %s", t)
}

// Insert inserts a single model, filling auto_create_time /
// auto_update_time and writing back an auto_increment primary key.
func (q *Query) Insert(ctx context.Context, model any) error {
	if q.err != nil {
		return q.err
	}
	meta, rv, err := modelMetaValue(model)
	if err != nil {
		return err
	}
	table := q.tableOr(meta.table)
	cols, vals, autoInc, err := buildInsertValues(meta, rv)
	if err != nil {
		return err
	}
	stmt := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", quoteIdent(table), strings.Join(cols, ", "), placeholders(len(vals)))
	res, err := q.exec.ExecContext(ctx, stmt, vals...)
	if err != nil {
		return err
	}
	if autoInc != nil {
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		fv := rv.FieldByIndex(autoInc.index)
		if fv.Kind() == reflect.Uint {
			fv.SetUint(uint64(id))
		} else {
			fv.SetInt(id)
		}
	}
	return nil
}

// InsertBatch inserts a slice of models ([]T or []*T) using multi-row
// VALUES statements in chunks of 500 rows.
func (q *Query) InsertBatch(ctx context.Context, models any) error {
	if q.err != nil {
		return q.err
	}
	rv := reflect.ValueOf(models)
	if rv.Kind() != reflect.Slice {
		return fmt.Errorf("orm: InsertBatch requires a slice, got %s", rv.Kind())
	}
	if rv.Len() == 0 {
		return nil
	}
	meta, err := metaForModelValue(rv.Index(0))
	if err != nil {
		return err
	}
	table := q.tableOr(meta.table)
	firstElem := modelElemValue(rv.Index(0))
	if !firstElem.IsValid() {
		return fmt.Errorf("orm: InsertBatch element 0 is a nil pointer")
	}
	cols, _, _, err := buildInsertValues(meta, firstElem)
	if err != nil {
		return err
	}
	const chunkSize = 500
	for start := 0; start < rv.Len(); start += chunkSize {
		end := start + chunkSize
		if end > rv.Len() {
			end = rv.Len()
		}
		var b strings.Builder
		var args []any
		b.WriteString(fmt.Sprintf("INSERT INTO %s (%s) VALUES ", quoteIdent(table), strings.Join(cols, ", ")))
		for i := start; i < end; i++ {
			if i > start {
				b.WriteString(", ")
			}
			elem := modelElemValue(rv.Index(i))
			if !elem.IsValid() {
				return fmt.Errorf("orm: InsertBatch element %d is a nil pointer", i)
			}
			_, vals, _, err := buildInsertValues(meta, elem)
			if err != nil {
				return err
			}
			b.WriteString("(" + placeholders(len(vals)) + ")")
			args = append(args, vals...)
		}
		if _, err := q.exec.ExecContext(ctx, b.String(), args...); err != nil {
			return err
		}
	}
	return nil
}

func metaForModelValue(v reflect.Value) (*modelInfo, error) {
	t := v.Type()
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("orm: model elements must be structs, got %s", t)
	}
	return metaFor(t)
}

func modelElemValue(v reflect.Value) reflect.Value {
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return reflect.Value{}
		}
		return v.Elem()
	}
	return v
}

// Update updates all mapped columns of a model (except primary key and
// auto_create_time) by primary key, stamping auto_update_time.
func (q *Query) Update(ctx context.Context, model any) error {
	if q.err != nil {
		return q.err
	}
	meta, rv, err := modelMetaValue(model)
	if err != nil {
		return err
	}
	if len(meta.pk) == 0 {
		return fmt.Errorf("orm: %s has no primary key; use UpdateColumns", meta.table)
	}
	table := q.tableOr(meta.table)
	now := time.Now()
	var sets []string
	var args []any
	for _, f := range meta.fields {
		if f.primaryKey || f.autoCreate {
			continue
		}
		fv := rv.FieldByIndex(f.index)
		if f.autoUpdate {
			if err := setTimeField(fv, now, f); err != nil {
				return err
			}
		}
		v, err := fieldValue(f, fv)
		if err != nil {
			return err
		}
		sets = append(sets, quoteIdent(f.column)+" = ?")
		args = append(args, v)
	}
	var wheres []string
	for _, pk := range meta.pk {
		v, err := fieldValue(pk, rv.FieldByIndex(pk.index))
		if err != nil {
			return err
		}
		if isZeroValue(v) {
			return fmt.Errorf("orm: primary key %s is empty", pk.column)
		}
		wheres = append(wheres, quoteIdent(pk.column)+" = ?")
		args = append(args, v)
	}
	stmt := fmt.Sprintf("UPDATE %s SET %s WHERE %s", quoteIdent(table), strings.Join(sets, ", "), strings.Join(wheres, " AND "))
	_, err = q.exec.ExecContext(ctx, stmt, args...)
	return err
}

// UpdateColumns sets arbitrary columns; a WHERE clause is required for
// safety.
func (q *Query) UpdateColumns(ctx context.Context, values map[string]any) error {
	if q.err != nil {
		return q.err
	}
	if q.table == "" {
		return fmt.Errorf("orm: no table selected")
	}
	if len(q.wheres) == 0 {
		return fmt.Errorf("orm: UpdateColumns requires a WHERE clause")
	}
	if len(values) == 0 {
		return fmt.Errorf("orm: UpdateColumns requires at least one column")
	}
	cols := make([]string, 0, len(values))
	for c := range values {
		cols = append(cols, c)
	}
	sort.Strings(cols)
	meta := registeredModelByTable(q.table)
	var sets []string
	args := make([]any, 0, len(cols)+8)
	for _, c := range cols {
		if meta != nil {
			if _, ok := meta.byColumn[c]; !ok {
				return fmt.Errorf("orm: unknown column %q", c)
			}
		}
		sets = append(sets, quoteIdent(c)+" = ?")
		args = append(args, values[c])
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("UPDATE %s SET %s", quoteIdent(q.table), strings.Join(sets, ", ")))
	b.WriteString(q.whereClauseSQL())
	args = append(args, q.whereArgs()...)
	_, err := q.exec.ExecContext(ctx, b.String(), args...)
	return err
}

// Delete deletes rows matched by the WHERE clause; a WHERE clause is
// required for safety.
func (q *Query) Delete(ctx context.Context) error {
	if q.err != nil {
		return q.err
	}
	if q.table == "" {
		return fmt.Errorf("orm: no table selected")
	}
	if len(q.wheres) == 0 {
		return fmt.Errorf("orm: Delete requires a WHERE clause")
	}
	var b strings.Builder
	b.WriteString("DELETE FROM " + quoteIdent(q.table))
	b.WriteString(q.whereClauseSQL())
	_, err := q.exec.ExecContext(ctx, b.String(), q.whereArgs()...)
	return err
}

func (q *Query) whereClauseSQL() string {
	if len(q.wheres) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(" WHERE ")
	for i, w := range q.wheres {
		if i > 0 {
			b.WriteString(" " + w.sep + " ")
		}
		b.WriteString(w.sql)
	}
	return b.String()
}

func buildInsertValues(meta *modelInfo, rv reflect.Value) (cols []string, vals []any, autoInc *fieldInfo, err error) {
	now := time.Now()
	for _, f := range meta.fields {
		if f.autoIncrement {
			autoInc = f
			continue
		}
		fv := rv.FieldByIndex(f.index)
		if f.autoCreate || f.autoUpdate {
			if f.kind == kindTime && timeIsZero(fv) {
				if err := setTimeField(fv, now, f); err != nil {
					return nil, nil, nil, err
				}
			}
		}
		v, err := fieldValue(f, fv)
		if err != nil {
			return nil, nil, nil, err
		}
		cols = append(cols, quoteIdent(f.column))
		vals = append(vals, v)
	}
	return cols, vals, autoInc, nil
}

func fieldValue(f *fieldInfo, fv reflect.Value) (any, error) {
	if fv.Kind() == reflect.Pointer || fv.Kind() == reflect.Interface {
		if fv.IsNil() {
			return nil, nil
		}
		fv = fv.Elem()
	}
	switch f.kind {
	case kindString:
		return fv.String(), nil
	case kindInt:
		return fv.Int(), nil
	case kindUint:
		return fv.Uint(), nil
	case kindFloat:
		return fv.Float(), nil
	case kindBool:
		if fv.Bool() {
			return int64(1), nil
		}
		return int64(0), nil
	case kindTime:
		t, ok := fv.Interface().(time.Time)
		if !ok {
			return nil, fmt.Errorf("orm: %s is not time.Time", f.column)
		}
		if f.timeUnix {
			return t.Unix(), nil
		}
		return t.UTC().Format(time.RFC3339Nano), nil
	case kindBytes:
		b := fv.Bytes()
		if b == nil {
			return nil, nil
		}
		cp := make([]byte, len(b))
		copy(cp, b)
		return cp, nil
	case kindJSON:
		b, err := json.Marshal(fv.Interface())
		if err != nil {
			return nil, fmt.Errorf("orm: marshal json column %s: %w", f.column, err)
		}
		return b, nil
	case kindAny:
		return fv.Interface(), nil
	case kindScanner:
		return fv.Interface(), nil
	default:
		return nil, fmt.Errorf("orm: unsupported field kind for %s", f.column)
	}
}

func timeIsZero(fv reflect.Value) bool {
	if fv.Kind() == reflect.Pointer {
		if fv.IsNil() {
			return true
		}
		fv = fv.Elem()
	}
	t, ok := fv.Interface().(time.Time)
	return !ok || t.IsZero()
}

func setTimeField(fv reflect.Value, t time.Time, f *fieldInfo) error {
	if fv.Kind() == reflect.Pointer {
		p := reflect.New(fv.Type().Elem())
		p.Elem().Set(reflect.ValueOf(t).Convert(fv.Type().Elem()))
		fv.Set(p)
		return nil
	}
	fv.Set(reflect.ValueOf(t).Convert(fv.Type()))
	return nil
}

func isZeroValue(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return x == ""
	case int64:
		return x == 0
	case int:
		return x == 0
	case uint64:
		return x == 0
	case float64:
		return x == 0
	case bool:
		return !x
	}
	return false
}
