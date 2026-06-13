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
	metricsDB *sql.DB
}

func Open(cfg config.Config) (*Store, error) {
	for _, p := range []string{cfg.AppDatabase, cfg.MetricsDatabase, filepath.Join(cfg.DataRoot, "tmp")} {
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
	metricsDB, err := sql.Open("sqlite", sqliteDSN(cfg.MetricsDatabase))
	if err != nil {
		_ = appDB.Close()
		return nil, err
	}
	configureDB(appDB)
	configureDB(metricsDB)
	s := &Store{appDB: appDB, metricsDB: metricsDB}
	if err := s.Migrate(context.Background()); err != nil {
		_ = s.Close()
		return nil, err
	}
	return s, nil
}

func sqliteDSN(path string) string {
	normalized := filepath.ToSlash(path)
	if strings.HasPrefix(normalized, "file:") {
		return normalized
	}
	return "file:" + normalized + "?" + url.Values{
		"_pragma": []string{
			"busy_timeout(5000)",
			"journal_mode(WAL)",
			"foreign_keys(ON)",
		},
	}.Encode()
}

func configureDB(db *sql.DB) {
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(4)
	db.SetConnMaxLifetime(30 * time.Minute)
}

func (s *Store) AppDB() *sql.DB     { return s.appDB }
func (s *Store) MetricsDB() *sql.DB { return s.metricsDB }

func (s *Store) Close() error {
	if s == nil {
		return nil
	}
	var err error
	if s.appDB != nil {
		err = s.appDB.Close()
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
