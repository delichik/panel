// Package orm is a lightweight, reflection-based ORM for SQLite built on
// database/sql. It provides model metadata parsing, a chainable query
// builder, CRUD terminal methods, automatic schema migration and
// versioned one-time migration steps.
//
// The package is SQLite-first and depends only on the standard library.
package orm

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// Options configures AutoMigrate.
type Options struct {
	Destructive bool
	Logger      func(format string, args ...any)
}

// Option mutates Options.
type Option func(*Options)

// WithDestructive enables or disables destructive schema changes
// (dropping columns, indexes and tables). The default is false.
func WithDestructive(b bool) Option {
	return func(o *Options) { o.Destructive = b }
}

// WithLogger installs a logger for migration progress messages.
func WithLogger(fn func(format string, args ...any)) Option {
	return func(o *Options) { o.Logger = fn }
}

// WithTx runs fn inside a transaction, committing on success and rolling
// back on error.
func WithTx(ctx context.Context, db *sql.DB, fn func(tx *sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// LikeEscaped escapes \, % and _ and wraps the term with %...% for use
// with LIKE ... ESCAPE '\'.
func LikeEscaped(term string) string {
	escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(term)
	return "%" + escaped + "%"
}

// Insert inserts a single model (convenience wrapper).
func Insert(ctx context.Context, db Executor, model any) error {
	return New(db).Insert(ctx, model)
}

// InsertBatch inserts a slice of models (convenience wrapper).
func InsertBatch(ctx context.Context, db Executor, models any) error {
	return New(db).InsertBatch(ctx, models)
}

// Update updates a model by primary key (convenience wrapper).
func Update(ctx context.Context, db Executor, model any) error {
	return New(db).Update(ctx, model)
}

// Delete deletes a model by primary key (convenience wrapper).
func Delete(ctx context.Context, db Executor, model any) error {
	meta, rv, err := modelMetaValue(model)
	if err != nil {
		return err
	}
	if len(meta.pk) == 0 {
		return fmt.Errorf("orm: %s has no primary key; delete by primary key is not possible", meta.table)
	}
	q := New(db).From(meta.table)
	for _, pk := range meta.pk {
		v, err := fieldValue(pk, rv.FieldByIndex(pk.index))
		if err != nil {
			return err
		}
		if isZeroValue(v) {
			return fmt.Errorf("orm: primary key %s is empty", pk.column)
		}
		q.Where(quoteIdent(pk.column)+" = ?", v)
	}
	return q.Delete(ctx)
}

// Raw runs an arbitrary query and returns the rows (escape hatch).
func Raw(ctx context.Context, db Executor, query string, args ...any) (*sql.Rows, error) {
	return db.QueryContext(ctx, query, args...)
}

// RawExec runs an arbitrary statement and returns its result (escape
// hatch).
func RawExec(ctx context.Context, db Executor, query string, args ...any) (sql.Result, error) {
	return db.ExecContext(ctx, query, args...)
}

// RawRow runs an arbitrary query and returns a single row (escape hatch).
func RawRow(ctx context.Context, db Executor, query string, args ...any) *sql.Row {
	return db.QueryRowContext(ctx, query, args...)
}
