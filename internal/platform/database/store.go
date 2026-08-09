package database

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"panel/internal/platform/config"
)

type Store struct {
	appDB     *sql.DB
	logDB     *sql.DB
	coordDB   *sql.DB
	metricsDB *sql.DB
}

func Open(cfg config.Config) (*Store, error) {
	if err := migrateLegacyLogDatabasePath(cfg.LogDatabase); err != nil {
		return nil, err
	}
	for _, p := range []string{cfg.AppDatabase, cfg.LogDatabase, cfg.CoordinationDatabase, cfg.MetricsDatabase, filepath.Join(cfg.DataRoot, "tmp")} {
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
	logDB, err := sql.Open("sqlite", sqliteDSN(cfg.LogDatabase))
	if err != nil {
		_ = appDB.Close()
		return nil, err
	}
	coordDB, err := sql.Open("sqlite", sqliteDSN(cfg.CoordinationDatabase))
	if err != nil {
		_ = logDB.Close()
		_ = appDB.Close()
		return nil, err
	}
	metricsDB, err := sql.Open("sqlite", sqliteDSN(cfg.MetricsDatabase))
	if err != nil {
		_ = coordDB.Close()
		_ = logDB.Close()
		_ = appDB.Close()
		return nil, err
	}
	configureDB(appDB)
	configureDB(logDB)
	configureDB(coordDB)
	configureDB(metricsDB)
	s := &Store{appDB: appDB, logDB: logDB, coordDB: coordDB, metricsDB: metricsDB}
	if err := s.Migrate(context.Background()); err != nil {
		_ = s.Close()
		return nil, err
	}
	return s, nil
}

func migrateLegacyLogDatabasePath(logPath string) error {
	if strings.TrimSpace(logPath) == "" || strings.HasPrefix(filepath.ToSlash(logPath), "file:") {
		return nil
	}
	if filepath.Base(logPath) != "log.db" {
		return nil
	}
	legacyPath := filepath.Join(filepath.Dir(logPath), "tasks.db")
	if _, err := os.Stat(logPath); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if _, err := os.Stat(legacyPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := os.Rename(legacyPath, logPath); err != nil {
		return err
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		oldSidecar := legacyPath + suffix
		newSidecar := logPath + suffix
		if _, err := os.Stat(oldSidecar); err == nil {
			if _, statErr := os.Stat(newSidecar); os.IsNotExist(statErr) {
				if renameErr := os.Rename(oldSidecar, newSidecar); renameErr != nil {
					return renameErr
				}
			}
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	return nil
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
func (s *Store) LogDB() *sql.DB     { return s.logDB }
func (s *Store) CoordDB() *sql.DB   { return s.coordDB }
func (s *Store) MetricsDB() *sql.DB { return s.metricsDB }

func (s *Store) Close() error {
	if s == nil {
		return nil
	}
	var err error
	if s.appDB != nil {
		err = s.appDB.Close()
	}
	if s.logDB != nil {
		if e := s.logDB.Close(); err == nil {
			err = e
		}
	}
	if s.coordDB != nil {
		if e := s.coordDB.Close(); err == nil {
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
