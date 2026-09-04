package orm

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"strings"
)

// blankStringValue reports whether v is a string (or byte slice) that is
// empty after trimming. It is used to treat "" in nullable time columns as
// "not set" on both the read and the write path.
func blankStringValue(v any) bool {
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x) == ""
	case []byte:
		return strings.TrimSpace(string(x)) == ""
	}
	return false
}

// NormalizeBlankTimeColumns runs an idempotent cleanup over every nullable
// time column of the given models: empty strings are converted to NULL.
// It must run after AutoMigrateModels on every startup so legacy rows written
// by older code (and any table that may regress later) can never reach scans
// as "orm: cannot parse time \"\"". Only columns that are physically nullable
// in the database are touched, so this never fails on a schema that
// AutoMigrate has not converged yet. Tables without such columns are no-ops.
func NormalizeBlankTimeColumns(ctx context.Context, ex Executor, models []any) error {
	for _, m := range models {
		info, err := metaFor(reflect.TypeOf(m))
		if err != nil {
			return err
		}
		var cols []string
		for _, f := range info.fields {
			if f.kind == kindTime && f.nullable {
				cols = append(cols, f.column)
			}
		}
		if len(cols) == 0 {
			continue
		}
		nullable, err := nullableTimeColumns(ctx, ex, info.table)
		if err != nil {
			return err
		}
		var target []string
		for _, c := range cols {
			if nullable[c] {
				target = append(target, c)
			}
		}
		if len(target) == 0 {
			continue
		}
		sets := make([]string, len(target))
		conds := make([]string, len(target))
		for i, c := range target {
			sets[i] = quoteIdent(c) + " = NULL"
			conds[i] = quoteIdent(c) + " = ''"
		}
		stmt := "UPDATE " + quoteIdent(info.table) + " SET " + strings.Join(sets, ", ") +
			" WHERE " + strings.Join(conds, " OR ")
		if _, err := ex.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("orm: normalize blank time column %s: %w", info.table, err)
		}
	}
	return nil
}

// nullableTimeColumns reports which columns of a table are nullable (PRAGMA
// table_info notnull = 0). A missing table yields an empty map, which is
// treated as "nothing to normalize".
func nullableTimeColumns(ctx context.Context, ex Executor, table string) (map[string]bool, error) {
	rows, err := ex.QueryContext(ctx, `PRAGMA table_info(`+quoteIdent(table)+`)`)
	if err != nil {
		return nil, fmt.Errorf("orm: inspect %s: %w", table, err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull int
		var dflt sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			return nil, fmt.Errorf("orm: inspect %s: %w", table, err)
		}
		out[name] = notnull == 0
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("orm: inspect %s: %w", table, err)
	}
	return out, nil
}
