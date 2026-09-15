package metrics

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"panel/internal/modules/servers"
	"panel/internal/platform/database/models"
	"panel/internal/platform/database/orm"
	panelerr "panel/internal/platform/errors"
	"panel/internal/platform/linux"
)

type Service struct {
	db      *sql.DB
	servers serverProvider
}

type serverProvider interface {
	Get(context.Context, string) (server.Server, error)
}

type reachabilityReporter interface {
	RecordMetricsReachability(context.Context, string, bool, string) error
}

type Series struct {
	Range   string        `json:"range"`
	CPU     []CPUPoint    `json:"cpu"`
	Memory  []MemoryPoint `json:"memory"`
	Disk    []DiskPoint   `json:"disk"`
	Network []NetPoint    `json:"network"`
	Load    []LoadPoint   `json:"load"`
}

type CPUPoint struct {
	Time         time.Time `json:"time"`
	UsagePercent float64   `json:"usagePercent"`
}
type MemoryPoint struct {
	Time       time.Time `json:"time"`
	UsedBytes  int64     `json:"usedBytes"`
	TotalBytes int64     `json:"totalBytes"`
}
type DiskPoint = MemoryPoint
type NetPoint struct {
	Time             time.Time `json:"time"`
	RxBytesPerSecond float64   `json:"rxBytesPerSecond"`
	TxBytesPerSecond float64   `json:"txBytesPerSecond"`
}
type LoadPoint struct {
	Time   time.Time `json:"time"`
	Load1  float64   `json:"load1"`
	Load5  float64   `json:"load5"`
	Load15 float64   `json:"load15"`
}

func NewService(db *sql.DB, servers serverProvider) *Service {
	return &Service{db: db, servers: servers}
}

func (s *Service) Save(ctx context.Context, snap linux.MetricsSnapshot) error {
	if snap.Time.IsZero() {
		snap.Time = time.Now().UTC()
	}
	snap.Time = alignMetricTime(snap.Time)
	return orm.New(s.db).Insert(ctx, &models.MetricsSnapshot{
		ServerID:         snap.ServerID,
		Time:             snap.Time,
		CPUUsagePercent:  snap.CPUUsagePercent,
		MemoryUsedBytes:  snap.MemoryUsedBytes,
		MemoryTotalBytes: snap.MemoryTotalBytes,
		DiskUsedBytes:    snap.DiskUsedBytes,
		DiskTotalBytes:   snap.DiskTotalBytes,
		NetworkRXBps:     snap.NetworkRxBytesRate,
		NetworkTXBps:     snap.NetworkTxBytesRate,
		LoadAverage:      snap.Status.LoadAverage,
		Load1:            snap.Status.Load1,
		Load5:            snap.Status.Load5,
		Load15:           snap.Status.Load15,
		UptimeSeconds:    snap.Status.UptimeSeconds,
		Hostname:         snap.Status.Hostname,
		KernelVersion:    snap.Status.KernelVersion,
		OSVersion:        snap.Status.OSVersion,
	})
}

func (s *Service) SaveReported(ctx context.Context, serverID string, sampleAt time.Time, snap linux.MetricsSnapshot) error {
	snap.ServerID = serverID
	snap.Time = sampleAt
	if err := s.Save(ctx, snap); err != nil {
		return err
	}
	if reporter, ok := s.servers.(reachabilityReporter); ok {
		_ = reporter.RecordMetricsReachability(ctx, serverID, true, "")
	}
	return nil
}

func (s *Service) Query(ctx context.Context, serverID, rng string) (Series, error) {
	return s.querySince(ctx, serverID, rng, false, time.Time{})
}

// QueryAfter returns only snapshots in the range that are strictly newer than
// after. It backs the overview auto-refresh flow, which appends just the points
// collected since the last loaded point instead of reloading the whole range.
func (s *Service) QueryAfter(ctx context.Context, serverID, rng string, after time.Time) (Series, error) {
	return s.querySince(ctx, serverID, rng, true, after)
}

func (s *Service) querySince(ctx context.Context, serverID, rng string, hasAfter bool, after time.Time) (Series, error) {
	duration := map[string]time.Duration{"1h": time.Hour, "6h": 6 * time.Hour, "1d": 24 * time.Hour, "24h": 24 * time.Hour, "7d": 7 * 24 * time.Hour}[rng]
	if duration == 0 {
		return Series{}, panelerr.Validation("range_invalid", "Range must be 1h, 6h, 1d, 24h, or 7d")
	}
	query := orm.New(s.db).From("metrics_snapshots").Where("server_id = ?", serverID).And("time >= ?", time.Now().UTC().Add(-duration).Format(time.RFC3339Nano))
	if hasAfter {
		query = query.And("time > ?", after.UTC().Truncate(time.Second).Format(time.RFC3339Nano))
	}
	var rows []models.MetricsSnapshot
	if err := query.OrderBy("time").All(ctx, &rows); err != nil {
		return Series{}, err
	}
	series := Series{Range: rng, CPU: []CPUPoint{}, Memory: []MemoryPoint{}, Disk: []DiskPoint{}, Network: []NetPoint{}, Load: []LoadPoint{}}
	for _, row := range rows {
		t := row.Time
		series.CPU = append(series.CPU, CPUPoint{Time: t, UsagePercent: row.CPUUsagePercent})
		series.Memory = append(series.Memory, MemoryPoint{Time: t, UsedBytes: row.MemoryUsedBytes, TotalBytes: row.MemoryTotalBytes})
		series.Disk = append(series.Disk, DiskPoint{Time: t, UsedBytes: row.DiskUsedBytes, TotalBytes: row.DiskTotalBytes})
		series.Network = append(series.Network, NetPoint{Time: t, RxBytesPerSecond: row.NetworkRXBps, TxBytesPerSecond: row.NetworkTXBps})
		series.Load = append(series.Load, LoadPoint{Time: t, Load1: row.Load1, Load5: row.Load5, Load15: row.Load15})
	}
	return series, nil
}

func (s *Service) Cleanup(ctx context.Context, retentionDays int) (int64, error) {
	if retentionDays < 1 {
		return 0, panelerr.Validation("invalid_metrics_retention", "Metrics retention must be at least 1 day")
	}
	cutoff := time.Now().UTC().Add(-time.Duration(retentionDays) * 24 * time.Hour).Format(time.RFC3339Nano)
	res, err := orm.RawExec(ctx, s.db, `DELETE FROM metrics_snapshots WHERE time < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// LatestAtMany 批量返回多个服务器的最新指标时间，避免概览页逐服务器 N+1 查询。
func (s *Service) LatestAtMany(ctx context.Context, serverIDs []string) (map[string]*time.Time, error) {
	out := map[string]*time.Time{}
	ids := cleanStringList(serverIDs)
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := orm.Raw(ctx, s.db, `SELECT server_id, MAX(time) FROM metrics_snapshots WHERE server_id IN (`+inPlaceholders(len(ids))+`) GROUP BY server_id`, stringArgs(ids)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var serverID string
		var ts sql.NullString
		if err := rows.Scan(&serverID, &ts); err != nil {
			return nil, err
		}
		if ts.Valid && ts.String != "" {
			if v, err := time.Parse(time.RFC3339Nano, ts.String); err == nil {
				out[serverID] = &v
			}
		}
	}
	return out, rows.Err()
}

// LatestLoadMany 批量返回每个服务器最新快照的 load_average，避免 N+1 查询。
func (s *Service) LatestLoadMany(ctx context.Context, serverIDs []string) (map[string]string, error) {
	out := map[string]string{}
	ids := cleanStringList(serverIDs)
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := orm.Raw(ctx, s.db, `SELECT s.server_id, s.load_average FROM metrics_snapshots s WHERE s.server_id IN (`+inPlaceholders(len(ids))+`) AND s.time = (SELECT MAX(time) FROM metrics_snapshots WHERE server_id = s.server_id)`, stringArgs(ids)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var serverID, load string
		if err := rows.Scan(&serverID, &load); err != nil {
			return nil, err
		}
		out[serverID] = load
	}
	return out, rows.Err()
}

// QueryMany 批量返回多个服务器的指标序列，避免概览卡片逐服务器 N+1 查询。
// after 非 nil 时只返回严格晚于该时间的点（与 QueryAfter 语义一致）。
func (s *Service) QueryMany(ctx context.Context, serverIDs []string, rng string, after *time.Time) (map[string]Series, error) {
	duration := map[string]time.Duration{"1h": time.Hour, "6h": 6 * time.Hour, "1d": 24 * time.Hour, "24h": 24 * time.Hour, "7d": 7 * 24 * time.Hour}[rng]
	if duration == 0 {
		return nil, panelerr.Validation("range_invalid", "Range must be 1h, 6h, 1d, 24h, or 7d")
	}
	out := map[string]Series{}
	ids := cleanStringList(serverIDs)
	if len(ids) == 0 {
		return out, nil
	}
	// 与旧逐服务器查询保持一致：每个请求的服务器都返回条目（无数据时为空序列）。
	for _, serverID := range ids {
		out[serverID] = Series{Range: rng, CPU: []CPUPoint{}, Memory: []MemoryPoint{}, Disk: []DiskPoint{}, Network: []NetPoint{}, Load: []LoadPoint{}}
	}
	since := time.Now().UTC().Add(-duration).Format(time.RFC3339Nano)
	for _, chunk := range chunkStrings(ids, 200) {
		query := orm.New(s.db).From("metrics_snapshots").
			Where("server_id IN ("+inPlaceholders(len(chunk))+")", stringArgs(chunk)...).
			And("time >= ?", since)
		if after != nil {
			query = query.And("time > ?", after.UTC().Truncate(time.Second).Format(time.RFC3339Nano))
		}
		var rows []models.MetricsSnapshot
		if err := query.OrderBy("server_id", "time").All(ctx, &rows); err != nil {
			return nil, err
		}
		for _, row := range rows {
			series := out[row.ServerID]
			t := row.Time
			series.CPU = append(series.CPU, CPUPoint{Time: t, UsagePercent: row.CPUUsagePercent})
			series.Memory = append(series.Memory, MemoryPoint{Time: t, UsedBytes: row.MemoryUsedBytes, TotalBytes: row.MemoryTotalBytes})
			series.Disk = append(series.Disk, DiskPoint{Time: t, UsedBytes: row.DiskUsedBytes, TotalBytes: row.DiskTotalBytes})
			series.Network = append(series.Network, NetPoint{Time: t, RxBytesPerSecond: row.NetworkRXBps, TxBytesPerSecond: row.NetworkTXBps})
			series.Load = append(series.Load, LoadPoint{Time: t, Load1: row.Load1, Load5: row.Load5, Load15: row.Load15})
			out[row.ServerID] = series
		}
	}
	return out, nil
}

func cleanStringList(values []string) []string {
	out := []string{}
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func stringArgs(values []string) []any {
	out := make([]any, 0, len(values))
	for _, value := range values {
		out = append(out, value)
	}
	return out
}

func chunkStrings(values []string, size int) [][]string {
	if size <= 0 {
		size = 200
	}
	chunks := [][]string{}
	for len(values) > 0 {
		if len(values) > size {
			chunks = append(chunks, values[:size])
			values = values[size:]
		} else {
			chunks = append(chunks, values)
			break
		}
	}
	return chunks
}

func inPlaceholders(count int) string {
	items := make([]string, count)
	for i := range items {
		items[i] = "?"
	}
	return strings.Join(items, ",")
}

func alignMetricTime(t time.Time) time.Time {
	return t.UTC().Truncate(time.Second)
}
