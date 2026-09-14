package applications

import (
	"context"
	"sort"
	"strings"
	"time"

	"panel/internal/platform/database/orm"
)

type persistentLocationRow struct {
	ServerID string `orm:"column:server_id"`
}

func (s *Service) persistentLocationServerIDs(ctx context.Context, appID string) ([]string, error) {
	var rows []persistentLocationRow
	if err := orm.New(s.db).From("application_persistent_locations").Select("server_id").Where("application_id=?", appID).OrderBy("server_id ASC").All(ctx, &rows); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		if serverID := strings.TrimSpace(row.ServerID); serverID != "" {
			out = append(out, serverID)
		}
	}
	sort.Strings(out)
	return uniqueStringItems(out), nil
}

func ensurePersistentLocations(ctx context.Context, exec orm.Executor, app Application) error {
	if strings.TrimSpace(app.PersistentPath) == "" {
		return nil
	}
	now := formatTime(time.Now().UTC())
	for _, serverID := range uniqueStringItems(app.DeploymentServers) {
		if _, err := orm.RawExec(ctx, exec, `INSERT OR IGNORE INTO application_persistent_locations(application_id,server_id,created_at) VALUES(?,?,?)`, app.ID, serverID, now); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) ensurePersistentLocation(ctx context.Context, appID, serverID string) error {
	serverID = strings.TrimSpace(serverID)
	if serverID == "" {
		return nil
	}
	_, err := orm.RawExec(ctx, s.db, `INSERT OR IGNORE INTO application_persistent_locations(application_id,server_id,created_at) VALUES(?,?,?)`, appID, serverID, formatTime(time.Now().UTC()))
	return err
}
