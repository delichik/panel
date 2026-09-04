package orm

import (
	"context"
	"database/sql"
	"reflect"
	"strings"
)

// Executor is satisfied by both *sql.DB and *sql.Tx.
type Executor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type whereClause struct {
	sep   string // "", "AND" or "OR"
	group bool
	sql   string
	args  []any
}

// Query is a chainable SQL builder bound to an Executor.
type Query struct {
	exec       Executor
	err        error
	table      string
	cols       []string
	selectExpr string
	selectArgs []any
	distinct   bool
	wheres     []whereClause
	joins      []string
	groupBy    []string
	having     string
	havingArgs []any
	orderBy    []string
	limit      *int
	offset     *int
}

// New creates a query builder bound to db (a *sql.DB or *sql.Tx).
func New(db Executor) *Query {
	return &Query{exec: db}
}

// From sets the target table.
func (q *Query) From(table string) *Query {
	q.table = table
	return q
}

// Select selects plain column names. Defaults to * when not called.
func (q *Query) Select(cols ...string) *Query {
	q.cols = append(q.cols, cols...)
	return q
}

// SelectExpr selects a raw expression (COUNT(*), SUM(x), ...). Its
// arguments are inserted before all other query arguments.
func (q *Query) SelectExpr(expr string, args ...any) *Query {
	q.selectExpr = expr
	q.selectArgs = args
	return q
}

// Distinct adds SELECT DISTINCT.
func (q *Query) Distinct() *Query {
	q.distinct = true
	return q
}

// Where adds a condition connected with AND to any previous condition.
func (q *Query) Where(cond string, args ...any) *Query {
	sep := "AND"
	if len(q.wheres) == 0 {
		sep = ""
	}
	q.wheres = append(q.wheres, whereClause{sep: sep, sql: cond, args: args})
	return q
}

// And adds a condition connected with AND.
func (q *Query) And(cond string, args ...any) *Query {
	q.wheres = append(q.wheres, whereClause{sep: "AND", sql: cond, args: args})
	return q
}

// Or adds a condition connected with OR.
func (q *Query) Or(cond string, args ...any) *Query {
	q.wheres = append(q.wheres, whereClause{sep: "OR", sql: cond, args: args})
	return q
}

// WhereGroup adds a parenthesized condition group connected with AND.
func (q *Query) WhereGroup(fn func(c *Condition)) *Query {
	inner := &Condition{}
	fn(inner)
	innerSQL, innerArgs := inner.build()
	sep := "AND"
	if len(q.wheres) == 0 {
		sep = ""
	}
	q.wheres = append(q.wheres, whereClause{sep: sep, group: true, sql: "(" + innerSQL + ")", args: innerArgs})
	return q
}

// AndIn adds `col IN (...)` connected with AND. An empty value list
// produces 1=0.
func (q *Query) AndIn(col string, values any) *Query {
	return q.inClause("AND", col, values, false)
}

// OrIn adds `col IN (...)` connected with OR.
func (q *Query) OrIn(col string, values any) *Query {
	return q.inClause("OR", col, values, false)
}

// AndNotIn adds `col NOT IN (...)` connected with AND. An empty value
// list produces 1=1.
func (q *Query) AndNotIn(col string, values any) *Query {
	return q.inClause("AND", col, values, true)
}

func (q *Query) inClause(sep, col string, values any, negate bool) *Query {
	args, empty := toArgs(values)
	var cond string
	switch {
	case empty && negate:
		cond = "1=1"
	case empty:
		cond = "1=0"
	default:
		op := "IN"
		if negate {
			op = "NOT IN"
		}
		cond = quoteQualified(col) + " " + op + " (" + placeholders(len(args)) + ")"
	}
	q.wheres = append(q.wheres, whereClause{sep: sep, sql: cond, args: args})
	return q
}

func toArgs(values any) ([]any, bool) {
	if values == nil {
		return nil, true
	}
	if args, ok := values.([]any); ok {
		return args, len(args) == 0
	}
	rv := reflect.ValueOf(values)
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return nil, true
	}
	args := make([]any, rv.Len())
	for i := 0; i < rv.Len(); i++ {
		args[i] = rv.Index(i).Interface()
	}
	return args, len(args) == 0
}

// AndLike adds `col LIKE ? ESCAPE '\'` connected with AND.
func (q *Query) AndLike(col, pattern string) *Query {
	q.wheres = append(q.wheres, whereClause{sep: "AND", sql: quoteQualified(col) + " LIKE ? ESCAPE '\\'", args: []any{pattern}})
	return q
}

// OrLike adds `col LIKE ? ESCAPE '\'` connected with OR.
func (q *Query) OrLike(col, pattern string) *Query {
	q.wheres = append(q.wheres, whereClause{sep: "OR", sql: quoteQualified(col) + " LIKE ? ESCAPE '\\'", args: []any{pattern}})
	return q
}

// AndNull adds `col IS NULL` connected with AND.
func (q *Query) AndNull(col string) *Query {
	q.wheres = append(q.wheres, whereClause{sep: "AND", sql: quoteQualified(col) + " IS NULL"})
	return q
}

// AndNotNull adds `col IS NOT NULL` connected with AND.
func (q *Query) AndNotNull(col string) *Query {
	q.wheres = append(q.wheres, whereClause{sep: "AND", sql: quoteQualified(col) + " IS NOT NULL"})
	return q
}

// AndBetween adds `col BETWEEN ? AND ?` connected with AND.
func (q *Query) AndBetween(col string, lo, hi any) *Query {
	q.wheres = append(q.wheres, whereClause{sep: "AND", sql: quoteQualified(col) + " BETWEEN ? AND ?", args: []any{lo, hi}})
	return q
}

// Join adds an INNER JOIN clause. table and on are used verbatim.
func (q *Query) Join(table, on string) *Query {
	q.joins = append(q.joins, "JOIN "+table+" ON "+on)
	return q
}

// LeftJoin adds a LEFT JOIN clause.
func (q *Query) LeftJoin(table, on string) *Query {
	q.joins = append(q.joins, "LEFT JOIN "+table+" ON "+on)
	return q
}

// RightJoin adds a RIGHT JOIN clause.
func (q *Query) RightJoin(table, on string) *Query {
	q.joins = append(q.joins, "RIGHT JOIN "+table+" ON "+on)
	return q
}

// GroupBy groups by plain column names.
func (q *Query) GroupBy(cols ...string) *Query {
	q.groupBy = append(q.groupBy, cols...)
	return q
}

// Having adds a HAVING condition.
func (q *Query) Having(cond string, args ...any) *Query {
	q.having = cond
	q.havingArgs = args
	return q
}

// OrderBy appends ORDER BY terms. Terms are used verbatim so expressions
// such as "created_at DESC" are allowed.
func (q *Query) OrderBy(cols ...string) *Query {
	q.orderBy = append(q.orderBy, cols...)
	return q
}

// Limit sets LIMIT n.
func (q *Query) Limit(n int) *Query {
	q.limit = &n
	return q
}

// Offset sets OFFSET n.
func (q *Query) Offset(n int) *Query {
	q.offset = &n
	return q
}

// ToSQL renders the query with ? placeholders (debug/testing helper).
func (q *Query) ToSQL() (string, []any) {
	return q.buildSelect(true)
}

// Condition builds a parenthesized where group.
type Condition struct {
	parts []whereClause
}

func (c *Condition) Where(cond string, args ...any) *Condition {
	c.parts = append(c.parts, whereClause{sep: "", sql: cond, args: args})
	return c
}

func (c *Condition) And(cond string, args ...any) *Condition {
	c.parts = append(c.parts, whereClause{sep: "AND", sql: cond, args: args})
	return c
}

func (c *Condition) Or(cond string, args ...any) *Condition {
	c.parts = append(c.parts, whereClause{sep: "OR", sql: cond, args: args})
	return c
}

func (c *Condition) WhereGroup(fn func(c *Condition)) *Condition {
	inner := &Condition{}
	fn(inner)
	innerSQL, innerArgs := inner.build()
	sep := "AND"
	if len(c.parts) == 0 {
		sep = ""
	}
	c.parts = append(c.parts, whereClause{sep: sep, group: true, sql: "(" + innerSQL + ")", args: innerArgs})
	return c
}

func (c *Condition) build() (string, []any) {
	var b strings.Builder
	var args []any
	for i, p := range c.parts {
		if i > 0 {
			b.WriteString(" " + p.sep + " ")
		}
		b.WriteString(p.sql)
		args = append(args, p.args...)
	}
	return b.String(), args
}

func (q *Query) whereArgs() []any {
	var args []any
	for _, w := range q.wheres {
		args = append(args, w.args...)
	}
	return args
}

// buildSelect renders the full SELECT statement, optionally including
// LIMIT/OFFSET.
func (q *Query) buildSelect(withLimit bool) (string, []any) {
	var b strings.Builder
	var args []any
	b.WriteString("SELECT ")
	if q.distinct {
		b.WriteString("DISTINCT ")
	}
	switch {
	case q.selectExpr != "":
		b.WriteString(q.selectExpr)
		args = append(args, q.selectArgs...)
	case len(q.cols) > 0:
		quoted := make([]string, len(q.cols))
		for i, c := range q.cols {
			quoted[i] = quoteQualified(c)
		}
		b.WriteString(strings.Join(quoted, ", "))
	default:
		b.WriteString("*")
	}
	q.writeCore(&b, &args, withLimit)
	return b.String(), args
}

// writeCore appends FROM ... WHERE ... GROUP BY ... HAVING ... ORDER BY
// ... LIMIT/OFFSET and appends the corresponding arguments in order.
func quoteQualified(name string) string {
	if strings.Contains(name, ".") {
		parts := strings.Split(name, ".")
		for i, p := range parts {
			parts[i] = quoteIdent(p)
		}
		return strings.Join(parts, ".")
	}
	return quoteIdent(name)
}
func (q *Query) writeCore(b *strings.Builder, args *[]any, withLimit bool) {
	b.WriteString(" FROM " + quoteIdent(q.table))
	for _, j := range q.joins {
		b.WriteString(" " + j)
	}
	if len(q.wheres) > 0 {
		b.WriteString(" WHERE ")
		for i, w := range q.wheres {
			if i > 0 {
				b.WriteString(" " + w.sep + " ")
			}
			b.WriteString(w.sql)
			*args = append(*args, w.args...)
		}
	}
	if len(q.groupBy) > 0 {
		quoted := make([]string, len(q.groupBy))
		for i, c := range q.groupBy {
			quoted[i] = quoteQualified(c)
		}
		b.WriteString(" GROUP BY " + strings.Join(quoted, ", "))
	}
	if q.having != "" {
		b.WriteString(" HAVING " + q.having)
		*args = append(*args, q.havingArgs...)
	}
	if len(q.orderBy) > 0 {
		b.WriteString(" ORDER BY " + strings.Join(q.orderBy, ", "))
	}
	if withLimit && q.limit != nil {
		b.WriteString(" LIMIT ?")
		*args = append(*args, *q.limit)
	}
	if withLimit && q.offset != nil {
		b.WriteString(" OFFSET ?")
		*args = append(*args, *q.offset)
	}
}

func (q *Query) countSQL() (string, []any) {
	inner, args := q.buildSelect(false)
	return "SELECT COUNT(*) FROM (" + inner + ")", args
}

func (q *Query) existsSQL() (string, []any) {
	var b strings.Builder
	var args []any
	b.WriteString("SELECT 1")
	q.writeCore(&b, &args, false)
	b.WriteString(" LIMIT 1")
	return b.String(), args
}

func (q *Query) pluckSQL(column string) (string, []any) {
	var b strings.Builder
	var args []any
	b.WriteString("SELECT " + quoteQualified(column))
	q.writeCore(&b, &args, true)
	return b.String(), args
}

func (q *Query) scanValueSQL() (string, []any) {
	var b strings.Builder
	var args []any
	b.WriteString("SELECT ")
	switch {
	case q.selectExpr != "":
		b.WriteString(q.selectExpr)
		args = append(args, q.selectArgs...)
	case len(q.cols) > 0:
		quoted := make([]string, len(q.cols))
		for i, c := range q.cols {
			quoted[i] = quoteIdent(c)
		}
		b.WriteString(strings.Join(quoted, ", "))
	default:
		b.WriteString("*")
	}
	q.writeCore(&b, &args, false)
	b.WriteString(" LIMIT 1")
	return b.String(), args
}
