package metrics

import (
	"context"
	"database/sql"
	"errors"
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
	Network []NetPoint    `json:"network"`
	Load    []LoadPoint   `json:"load"`
	DiskIO  []DiskIOPoint `json:"diskIo"`
	// DiskCurrent 是范围内最后一次有效磁盘采样（disk_total_bytes > 0）。
	// 磁盘使用率不再提供历史曲线，只返回当前值；nil 表示范围内没有有效采样。
	DiskCurrent *MemoryPoint `json:"diskCurrent"`
	// Latest 是最新原始指标样本，供降采样（bucket）查询一次取回“当前值”。
	// 仅 QueryBucketed 填充；原始 Query/QueryAfter 保持 nil，响应不出现该字段。
	Latest *LatestSample `json:"latest,omitempty"`
}

// LatestSample 是一台服务器最新一条原始指标样本，语义上等价于原始查询的最后
// 一个点，但由降采样响应一并返回，避免前端为当前值再发一次原始请求。
type LatestSample struct {
	Time             time.Time  `json:"time"`
	CPUUsagePercent  float64    `json:"cpuUsagePercent"`
	MemoryUsedBytes  int64      `json:"memoryUsedBytes"`
	MemoryTotalBytes int64      `json:"memoryTotalBytes"`
	DiskUsedBytes    int64      `json:"diskUsedBytes"`
	DiskTotalBytes   int64      `json:"diskTotalBytes"`
	DiskSampledAt    *time.Time `json:"diskSampledAt"`
	Load1            float64    `json:"load1"`
	NetworkRXBps     float64    `json:"rxBytesPerSecond"`
	NetworkTXBps     float64    `json:"txBytesPerSecond"`
}

// SeriesFields 选择批量查询需要物化的指标序列，避免概览卡片为不使用的序列付出
// 聚合与序列化成本。
type SeriesFields struct {
	CPU         bool
	Memory      bool
	Network     bool
	Load        bool
	DiskIO      bool
	DiskCurrent bool
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
type NetPoint struct {
	Time             time.Time `json:"time"`
	RxBytesPerSecond float64   `json:"rxBytesPerSecond"`
	TxBytesPerSecond float64   `json:"txBytesPerSecond"`
}
type DiskIOPoint struct {
	Time                time.Time `json:"time"`
	ReadBytesPerSecond  float64   `json:"readBytesPerSecond"`
	WriteBytesPerSecond float64   `json:"writeBytesPerSecond"`
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
	// 补报重放可能重复发送同一 (server_id, time) 样本。这里在插入前显式去重，
	// 因为表上没有唯一约束；并发重复插入的窗口由 report 流串行写入收敛。
	exists, err := orm.New(s.db).From("metrics_snapshots").
		Where("server_id = ?", snap.ServerID).
		And("time = ?", snap.Time.Format(time.RFC3339Nano)).
		Exists(ctx)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	// 可空磁盘 IO 列：未测得时写 NULL，避免把“未知”伪装成 0 写入历史曲线。
	var readRate, writeRate *float64
	if snap.DiskIOAvailable {
		read := snap.DiskReadBytesRate
		write := snap.DiskWriteBytesRate
		readRate = &read
		writeRate = &write
	}
	return orm.New(s.db).Insert(ctx, &models.MetricsSnapshot{
		ServerID:           snap.ServerID,
		Time:               snap.Time,
		CPUUsagePercent:    snap.CPUUsagePercent,
		MemoryUsedBytes:    snap.MemoryUsedBytes,
		MemoryTotalBytes:   snap.MemoryTotalBytes,
		DiskUsedBytes:      snap.DiskUsedBytes,
		DiskTotalBytes:     snap.DiskTotalBytes,
		DiskReadBytesRate:  readRate,
		DiskWriteBytesRate: writeRate,
		NetworkRXBps:       snap.NetworkRxBytesRate,
		NetworkTXBps:       snap.NetworkTxBytesRate,
		LoadAverage:        snap.Status.LoadAverage,
		Load1:              snap.Status.Load1,
		Load5:              snap.Status.Load5,
		Load15:             snap.Status.Load15,
		UptimeSeconds:      snap.Status.UptimeSeconds,
		Hostname:           snap.Status.Hostname,
		KernelVersion:      snap.Status.KernelVersion,
		OSVersion:          snap.Status.OSVersion,
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
	series := Series{Range: rng, CPU: []CPUPoint{}, Memory: []MemoryPoint{}, Network: []NetPoint{}, Load: []LoadPoint{}, DiskIO: []DiskIOPoint{}}
	for _, row := range rows {
		t := row.Time
		series.CPU = append(series.CPU, CPUPoint{Time: t, UsagePercent: row.CPUUsagePercent})
		series.Memory = append(series.Memory, MemoryPoint{Time: t, UsedBytes: row.MemoryUsedBytes, TotalBytes: row.MemoryTotalBytes})
		series.Network = append(series.Network, NetPoint{Time: t, RxBytesPerSecond: row.NetworkRXBps, TxBytesPerSecond: row.NetworkTXBps})
		series.Load = append(series.Load, LoadPoint{Time: t, Load1: row.Load1, Load5: row.Load5, Load15: row.Load15})
		// ORM 把可空的 disk_read_bytes_rate/disk_write_bytes_rate 扫描成 *float64，
		// NULL 即 nil。两个都未测得的样本不产生 diskIo 点。
		if row.DiskReadBytesRate != nil || row.DiskWriteBytesRate != nil {
			point := DiskIOPoint{Time: t}
			if row.DiskReadBytesRate != nil {
				point.ReadBytesPerSecond = *row.DiskReadBytesRate
			}
			if row.DiskWriteBytesRate != nil {
				point.WriteBytesPerSecond = *row.DiskWriteBytesRate
			}
			series.DiskIO = append(series.DiskIO, point)
		}
		if row.DiskTotalBytes > 0 {
			series.DiskCurrent = &MemoryPoint{Time: t, UsedBytes: row.DiskUsedBytes, TotalBytes: row.DiskTotalBytes}
		}
	}
	return series, nil
}

// QueryBucketed 返回单台服务器的降采样指标序列：按 bucketSeconds 聚合原始采样
// （Unix 纪元对齐的固定时间桶取平均值），使响应点数只与时间范围有关；同时在
// Latest 中附带最新原始样本的当前值，避免前端为“当前值”再发一次原始请求。
// 空数据返回空数组且 Latest 为 nil。
func (s *Service) QueryBucketed(ctx context.Context, serverID, rng string, bucketSeconds int) (Series, error) {
	byServer, err := s.QueryMany(ctx, []string{serverID}, rng, QueryManyOptions{
		BucketSeconds: bucketSeconds,
		Fields: SeriesFields{
			CPU:         true,
			Memory:      true,
			Network:     true,
			Load:        true,
			DiskIO:      true,
			DiskCurrent: true,
		},
	})
	if err != nil {
		return Series{}, err
	}
	id := strings.TrimSpace(serverID)
	series, ok := byServer[id]
	if !ok {
		// QueryMany 会清洗空 ID；这里保持与“无数据服务器”一致的响应形状。
		series = emptySeries(rng)
	}
	duration, ok := RangeDuration(rng)
	if !ok {
		return Series{}, panelerr.Validation("range_invalid", "Range must be 1h, 6h, 1d, 24h, or 7d")
	}
	since := time.Now().UTC().Add(-duration).Format(time.RFC3339Nano)
	latest, err := s.latestSample(ctx, id, since)
	if err != nil {
		return Series{}, err
	}
	series.Latest = latest
	return series, nil
}

// latestSample 读取范围内最新一条原始指标样本，并附带范围内最近一次有效磁盘
// 采样（disk_total_bytes > 0，语义同 diskCurrent）。两条查询都是按 server_id 的
// 单行查询，不随服务器数量产生 N+1。
func (s *Service) latestSample(ctx context.Context, serverID, since string) (*LatestSample, error) {
	var rawTime string
	var cpu, load1, rxBps, txBps float64
	var memUsed, memTotal int64
	err := s.db.QueryRowContext(ctx, `
		SELECT time, cpu_usage_percent, memory_used_bytes, memory_total_bytes, load_1, network_rx_bps, network_tx_bps
		FROM metrics_snapshots
		WHERE server_id = ? AND time >= ?
		ORDER BY strftime('%s', time) DESC, time DESC
		LIMIT 1`, serverID, since).
		Scan(&rawTime, &cpu, &memUsed, &memTotal, &load1, &rxBps, &txBps)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	at, ok := parseMetricTime(rawTime)
	if !ok {
		return nil, nil
	}
	latest := &LatestSample{
		Time:             at,
		CPUUsagePercent:  cpu,
		MemoryUsedBytes:  memUsed,
		MemoryTotalBytes: memTotal,
		Load1:            load1,
		NetworkRXBps:     rxBps,
		NetworkTXBps:     txBps,
	}
	var diskTime string
	var diskUsed, diskTotal int64
	err = s.db.QueryRowContext(ctx, `
		SELECT time, disk_used_bytes, disk_total_bytes
		FROM metrics_snapshots
		WHERE server_id = ? AND time >= ? AND disk_total_bytes > 0
		ORDER BY strftime('%s', time) DESC, time DESC
		LIMIT 1`, serverID, since).
		Scan(&diskTime, &diskUsed, &diskTotal)
	if errors.Is(err, sql.ErrNoRows) {
		return latest, nil
	}
	if err != nil {
		return nil, err
	}
	if diskAt, ok := parseMetricTime(diskTime); ok {
		latest.DiskUsedBytes = diskUsed
		latest.DiskTotalBytes = diskTotal
		latest.DiskSampledAt = &diskAt
	}
	return latest, nil
}

// parseMetricTime 解析 metrics_snapshots.time（RFC3339Nano 文本，兼容 RFC3339）。
func parseMetricTime(raw string) (time.Time, bool) {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed.UTC(), true
		}
	}
	return time.Time{}, false
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
	if opts.Fields.DiskIO {
		selects = append(selects, "AVG(disk_read_bytes_rate) AS disk_read_bytes_rate", "AVG(disk_write_bytes_rate) AS disk_write_bytes_rate")
	}
	if opts.Fields.Network {
		selects = append(selects, "AVG(network_rx_bps) AS network_rx_bps", "AVG(network_tx_bps) AS network_tx_bps")
	}
	if opts.Fields.Load {
		selects = append(selects, "AVG(load_1) AS load_1", "AVG(load_5) AS load_5", "AVG(load_15) AS load_15")
	}
	hasBucketedFields := opts.Fields.CPU || opts.Fields.Memory || opts.Fields.DiskIO || opts.Fields.Network || opts.Fields.Load
	since := time.Now().UTC().Add(-duration).Format(time.RFC3339Nano)
	for _, chunk := range chunkStrings(ids, 200) {
		if hasBucketedFields {
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
		if opts.Fields.DiskCurrent {
			if err := s.scanDiskCurrent(ctx, chunk, since, out); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

// scanDiskCurrent 为每个服务器填充范围内最后一次有效磁盘采样（disk_total_bytes > 0）。
// 该值表示“当前磁盘占用”，必须忽略 After/桶：增量刷新时磁盘卡仍拿到完整当前值，
// 而不是只拿到自上次刷新以来的空序列。
func (s *Service) scanDiskCurrent(ctx context.Context, serverIDs []string, since string, out map[string]Series) error {
	query := `
		SELECT server_id, time, disk_used_bytes, disk_total_bytes
		FROM (
			SELECT server_id, time, disk_used_bytes, disk_total_bytes,
				ROW_NUMBER() OVER (PARTITION BY server_id ORDER BY strftime('%s', time) DESC) AS rn
			FROM metrics_snapshots
			WHERE server_id IN (` + inPlaceholders(len(serverIDs)) + `) AND time >= ? AND disk_total_bytes > 0
		) WHERE rn = 1`
	args := append([]any{}, stringArgs(serverIDs)...)
	args = append(args, since)
	rows, err := orm.Raw(ctx, s.db, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var serverID, rawTime string
		var used, total int64
		if err := rows.Scan(&serverID, &rawTime, &used, &total); err != nil {
			return err
		}
		series, ok := out[serverID]
		if !ok {
			continue
		}
		at, err := time.Parse(time.RFC3339Nano, rawTime)
		if err != nil {
			continue
		}
		series.DiskCurrent = &MemoryPoint{Time: at.UTC(), UsedBytes: used, TotalBytes: total}
		out[serverID] = series
	}
	return rows.Err()
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
		var memUsed, memTotal int64
		var diskRead, diskWrite sql.NullFloat64
		if fields.CPU {
			dest = append(dest, &cpu)
		}
		if fields.Memory {
			dest = append(dest, &memUsed, &memTotal)
		}
		if fields.DiskIO {
			dest = append(dest, &diskRead, &diskWrite)
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
		if fields.DiskIO && (diskRead.Valid || diskWrite.Valid) {
			// 桶内两个可空列都 NULL 表示整桶未测得磁盘 IO，不产生点。
			point := DiskIOPoint{Time: at}
			if diskRead.Valid {
				point.ReadBytesPerSecond = diskRead.Float64
			}
			if diskWrite.Valid {
				point.WriteBytesPerSecond = diskWrite.Float64
			}
			series.DiskIO = append(series.DiskIO, point)
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
	return Series{Range: rng, CPU: []CPUPoint{}, Memory: []MemoryPoint{}, Network: []NetPoint{}, Load: []LoadPoint{}, DiskIO: []DiskIOPoint{}}
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
