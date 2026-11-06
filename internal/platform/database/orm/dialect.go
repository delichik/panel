package orm

import "strings"

// Dialect abstracts database-specific SQL details. The package is
// SQLite-first; only the default dialect is implemented.
type Dialect interface {
	// QuoteIdent quotes a table or column identifier.
	QuoteIdent(name string) string
	// Placeholder returns the argument placeholder for the i-th
	// parameter (0-based).
	Placeholder(i int) string
}

type sqliteDialect struct{}

func (sqliteDialect) QuoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func (sqliteDialect) Placeholder(int) string { return "?" }

var defaultDialect Dialect = sqliteDialect{}

func quoteIdent(name string) string { return defaultDialect.QuoteIdent(name) }

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}
