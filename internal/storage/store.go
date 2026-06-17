package storage

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"panel/internal/config"
)

type Store struct {
	appDB     *sql.DB
	taskDB    *sql.DB
	metricsDB *sql.DB
}

func Open(cfg config.Config) (*Store, error) {
	for _, p := range []string{cfg.AppDatabase, cfg.TaskDatabase, cfg.MetricsDatabase, filepath.Join(cfg.DataRoot, "tmp")} {
		dir := p
		if filepath.Ext(p) != "" {
			dir = filepath.Dir(p)
		}
		if err := os.MkdirAll(dir, 0700); err != nil {
			return nil, err
		}
	}
	appDB, err := sql.Open("sqlite", sqliteDSN(cfg.AppDatabase))
	if err != nil {
		return nil, err
	}
	taskDB, err := sql.Open("sqlite", sqliteDSN(cfg.TaskDatabase))
	if err != nil {
		_ = appDB.Close()
		return nil, err
	}
	metricsDB, err := sql.Open("sqlite", sqliteDSN(cfg.MetricsDatabase))
	if err != nil {
		_ = taskDB.Close()
		_ = appDB.Close()
		return nil, err
	}
	configureDB(appDB)
	configureDB(taskDB)
	configureDB(metricsDB)
	s := &Store{appDB: appDB, taskDB: taskDB, metricsDB: metricsDB}
	if err := s.Migrate(context.Background()); err != nil {
		_ = s.Close()
		return nil, err
	}
	return s, nil
}

func sqliteDSN(path string) string {
	normalized := filepath.ToSlash(path)
	if strings.HasPrefix(normalized, "file:") {
		return appendSQLitePragmas(normalized)
	}
	return appendSQLitePragmas("file:" + normalized)
}

func appendSQLitePragmas(dsn string) string {
	base, rawQuery, hasQuery := strings.Cut(dsn, "?")
	values, _ := url.ParseQuery(rawQuery)
	ensureSQLitePragma(values, "busy_timeout", "busy_timeout(5000)")
	ensureSQLitePragma(values, "journal_mode", "journal_mode(WAL)")
	ensureSQLitePragma(values, "foreign_keys", "foreign_keys(ON)")
	encoded := values.Encode()
	if encoded == "" {
		if hasQuery {
			return base + "?"
		}
		return base
	}
	return base + "?" + encoded
}

func ensureSQLitePragma(values url.Values, name, value string) {
	name = strings.ToLower(name)
	for _, existing := range values["_pragma"] {
		existing = strings.ToLower(strings.TrimSpace(existing))
		if strings.HasPrefix(existing, name+"(") || strings.HasPrefix(existing, name+"=") {
			return
		}
	}
	values.Add("_pragma", value)
}

func configureDB(db *sql.DB) {
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(30 * time.Minute)
}

func (s *Store) AppDB() *sql.DB     { return s.appDB }
func (s *Store) TaskDB() *sql.DB    { return s.taskDB }
func (s *Store) MetricsDB() *sql.DB { return s.metricsDB }

func (s *Store) Close() error {
	if s == nil {
		return nil
	}
	var err error
	if s.appDB != nil {
		err = s.appDB.Close()
	}
	if s.taskDB != nil {
		if e := s.taskDB.Close(); err == nil {
			err = e
		}
	}
	if s.metricsDB != nil {
		if e := s.metricsDB.Close(); err == nil {
			err = e
		}
	}
	return err
}

func (s *Store) WithAppTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.appDB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
