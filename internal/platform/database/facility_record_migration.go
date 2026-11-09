package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// migrateFacilityIndependentConfigRecordsOn backfills per-route version and
// scopes static assets to the facility domain records that reference them.
func migrateFacilityIndependentConfigRecordsOn(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `UPDATE reverse_proxy_routes SET version=1 WHERE version IS NULL OR version < 1`); err != nil {
		// Column may already be constrained; ignore missing-column only if AutoMigrate has not run.
		if !strings.Contains(strings.ToLower(err.Error()), "no such column") {
			return fmt.Errorf("backfill reverse_proxy_routes.version: %w", err)
		}
	}
	// Drop legacy global unique name index if present (name varies by SQLite/orm).
	for _, ddl := range []string{
		`DROP INDEX IF EXISTS idx_facility_static_assets_name`,
		`DROP INDEX IF EXISTS sqlite_autoindex_facility_static_assets_2`,
	} {
		if _, err := tx.ExecContext(ctx, ddl); err != nil {
			return fmt.Errorf("drop legacy facility asset name index: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `CREATE UNIQUE INDEX IF NOT EXISTS idx_facility_static_assets_record_name ON facility_static_assets(record_id, name)`); err != nil {
		return fmt.Errorf("create facility asset record_name index: %w", err)
	}

	rows, err := tx.QueryContext(ctx, `SELECT domain, paths_json FROM reverse_proxy_routes WHERE app_id IN ('facility-reverse-proxy','facility_reverse_proxy')`)
	if err != nil {
		return err
	}
	defer rows.Close()
	assetToDomains := map[string]map[string]struct{}{}
	for rows.Next() {
		var domain, pathsRaw string
		if err := rows.Scan(&domain, &pathsRaw); err != nil {
			return err
		}
		domain = strings.ToLower(strings.TrimSpace(domain))
		var paths []map[string]any
		_ = json.Unmarshal([]byte(pathsRaw), &paths)
		for _, path := range paths {
			name := strings.TrimSpace(fmt.Sprint(path["assetName"]))
			if name == "" || name == "<nil>" {
				name = strings.TrimSpace(fmt.Sprint(path["assetId"]))
			}
			if name == "" || name == "<nil>" {
				continue
			}
			if assetToDomains[name] == nil {
				assetToDomains[name] = map[string]struct{}{}
			}
			assetToDomains[name][domain] = struct{}{}
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	assetRows, err := tx.QueryContext(ctx, `SELECT id, name, kind, content_mode, filename, size, sha256, created_at, updated_at, COALESCE(record_id,'') FROM facility_static_assets`)
	if err != nil {
		return err
	}
	defer assetRows.Close()
	type assetRow struct {
		ID, Name, Kind, Mode, Filename, SHA, Created, Updated, RecordID string
		Size                                                             int
	}
	var assets []assetRow
	for assetRows.Next() {
		var row assetRow
		if err := assetRows.Scan(&row.ID, &row.Name, &row.Kind, &row.Mode, &row.Filename, &row.Size, &row.SHA, &row.Created, &row.Updated, &row.RecordID); err != nil {
			return err
		}
		assets = append(assets, row)
	}
	if err := assetRows.Err(); err != nil {
		return err
	}

	for _, asset := range assets {
		if strings.TrimSpace(asset.RecordID) != "" {
			continue
		}
		domains := assetToDomains[asset.Name]
		if len(domains) == 0 {
			continue
		}
		first := true
		for domain := range domains {
			if first {
				if _, err := tx.ExecContext(ctx, `UPDATE facility_static_assets SET record_id=? WHERE id=?`, domain, asset.ID); err != nil {
					return err
				}
				first = false
				continue
			}
			// Duplicate metadata row for additional referencing domains (blob path still keyed by id;
			// callers that need a private copy rename on next edit).
			newID := asset.ID + "__" + strings.ReplaceAll(domain, ".", "_")
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO facility_static_assets(id,record_id,name,kind,content_mode,filename,size,sha256,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
				newID, domain, asset.Name, asset.Kind, asset.Mode, asset.Filename, asset.Size, asset.SHA, asset.Created, asset.Updated); err != nil {
				return err
			}
		}
	}
	return nil
}
