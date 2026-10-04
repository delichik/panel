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

// SeriesFields 选择批量查询需要物化的指标序列，避免概览卡片为不使用的序列付出
// 聚合与序列化成本。
type SeriesFields struct {
	CPU     bool
	Memory  bool
	Disk    bool
	Network bool
	Load    bool
}

// QueryManyOptions 控制批量指标查询。BucketSeconds 必须大于零：查询按 Unix 纪元
// 对齐的固定时间桶返回平均值，保证响应点数只与时间范围和服务器数量有关，与原始
// 采样频率无关。After 非 nil 时先向下对齐到桶起点，使正在累积的桶被完整重算，
// 供前端按后缀替换。
type QueryManyOptions struct {
	After         *time.Time
	BucketSeconds int
	Fields        SeriesFields
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
	duration, ok := RangeDuration(rng)
	if !ok {
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

// RangeDuration 返回指标查询支持的时间范围时长，供服务端规划降采样桶。
func RangeDuration(rng string) (time.Duration, bool) {
	duration, ok := map[string]time.Duration{"1h": time.Hour, "6h": 6 * time.Hour, "1d": 24 * time.Hour, "24h": 24 * time.Hour, "7d": 7 * 24 * time.Hour}[rng]
	return duration, ok
}

// QueryMany 批量返回多个服务器的降采样指标序列，避免概览卡片逐服务器 N+1 查询
// 或整段原始点下发。按 serverID 分块查询；无数据服务器保留空序列。
func (s *Service) QueryMany(ctx context.Context, serverIDs []string, rng string, opts QueryManyOptions) (map[string]Series, error) {
	duration, ok := RangeDuration(rng)
	if !ok {
		return nil, panelerr.Validation("range_invalid", "Range must be 1h, 6h, 1d, 24h, or 7d")
	}
	if opts.BucketSeconds <= 0 {
		return nil, panelerr.Validation("bucket_seconds_invalid", "Bucket seconds must be positive")
	}
	out := map[string]Series{}
	ids := cleanStringList(serverIDs)
	if len(ids) == 0 {
		return out, nil
	}
	for _, serverID := range ids {
		out[serverID] = emptySeries(rng)
	}
	selects := []string{
		"server_id",
		"(CAST(strftime('%s', time) AS INTEGER) / ?) * ? AS bucket",
	}
	selectArgs := []any{opts.BucketSeconds, opts.BucketSeconds}
	if opts.Fields.CPU {
		selects = append(selects, "AVG(cpu_usage_percent) AS cpu_usage_percent")
	}
	if opts.Fields.Memory {
		selects = append(selects, "CAST(ROUND(AVG(memory_used_bytes)) AS INTEGER) AS memory_used_bytes", "CAST(ROUND(AVG(memory_total_bytes)) AS INTEGER) AS memory_total_bytes")
	}
	if opts.Fields.Disk {
		selects = append(selects, "CAST(ROUND(AVG(disk_used_bytes)) AS INTEGER) AS disk_used_bytes", "CAST(ROUND(AVG(disk_total_bytes)) AS INTEGER) AS disk_total_bytes")
	}
	if opts.Fields.Network {
		selects = append(selects, "AVG(network_rx_bps) AS network_rx_bps", "AVG(network_tx_bps) AS network_tx_bps")
	}
	if opts.Fields.Load {
		selects = append(selects, "AVG(load_1) AS load_1", "AVG(load_5) AS load_5", "AVG(load_15) AS load_15")
	}
	since := time.Now().UTC().Add(-duration).Format(time.RFC3339Nano)
	for _, chunk := range chunkStrings(ids, 200) {
		where := "server_id IN (" + inPlaceholders(len(chunk)) + ") AND time >= ?"
		args := append([]any{}, selectArgs...)
		args = append(args, stringArgs(chunk)...)
		args = append(args, since)
		if opts.After != nil {
			where += " AND time >= ?"
			args = append(args, floorTimeToBucket(*opts.After, opts.BucketSeconds).Format(time.RFC3339Nano))
		}
		query := "SELECT " + strings.Join(selects, ", ") + " FROM metrics_snapshots WHERE " + where + " GROUP BY server_id, bucket ORDER BY server_id, bucket"
		if err := s.scanBucketedSeries(ctx, query, args, out, opts.Fields); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Service) scanBucketedSeries(ctx context.Context, query string, args []any, out map[string]Series, fields SeriesFields) error {
	rows, err := orm.Raw(ctx, s.db, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var serverID string
		var bucket int64
		dest := []any{&serverID, &bucket}
		var cpu, rx, tx, load1, load5, load15 float64
		var memUsed, memTotal, diskUsed, diskTotal int64
		if fields.CPU {
			dest = append(dest, &cpu)
		}
		if fields.Memory {
			dest = append(dest, &memUsed, &memTotal)
		}
		if fields.Disk {
			dest = append(dest, &diskUsed, &diskTotal)
		}
		if fields.Network {
			dest = append(dest, &rx, &tx)
		}
		if fields.Load {
			dest = append(dest, &load1, &load5, &load15)
		}
		if err := rows.Scan(dest...); err != nil {
			return err
		}
		series, ok := out[serverID]
		if !ok {
			continue
		}
		at := time.Unix(bucket, 0).UTC()
		if fields.CPU {
			series.CPU = append(series.CPU, CPUPoint{Time: at, UsagePercent: cpu})
		}
		if fields.Memory {
			series.Memory = append(series.Memory, MemoryPoint{Time: at, UsedBytes: memUsed, TotalBytes: memTotal})
		}
		if fields.Disk {
			series.Disk = append(series.Disk, DiskPoint{Time: at, UsedBytes: diskUsed, TotalBytes: diskTotal})
		}
		if fields.Network {
			series.Network = append(series.Network, NetPoint{Time: at, RxBytesPerSecond: rx, TxBytesPerSecond: tx})
		}
		if fields.Load {
			series.Load = append(series.Load, LoadPoint{Time: at, Load1: load1, Load5: load5, Load15: load15})
		}
		out[serverID] = series
	}
	return rows.Err()
}

func emptySeries(rng string) Series {
	return Series{Range: rng, CPU: []CPUPoint{}, Memory: []MemoryPoint{}, Disk: []DiskPoint{}, Network: []NetPoint{}, Load: []LoadPoint{}}
}

// floorTimeToBucket 把时间向下对齐到 Unix 纪元对齐的桶起点。
func floorTimeToBucket(t time.Time, bucketSeconds int) time.Time {
	seconds := t.UTC().Unix()
	if bucketSeconds <= 1 {
		return time.Unix(seconds, 0).UTC()
	}
	bucket := int64(bucketSeconds)
	remainder := seconds % bucket
	if remainder < 0 {
		remainder += bucket
	}
	return time.Unix(seconds-remainder, 0).UTC()
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
