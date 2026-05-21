package metrics

import (
	"context"
	"database/sql"
	"time"

	"panel/internal/linux"
	"panel/internal/panelerr"
	"panel/internal/server"
	"panel/internal/sshx"
)

type Service struct {
	db      *sql.DB
	servers *server.Service
	exec    sshx.RemoteExecutor
	adapter linux.DebianAdapter
}

type Series struct {
	Range   string        `json:"range"`
	CPU     []CPUPoint    `json:"cpu"`
	Memory  []MemoryPoint `json:"memory"`
	Disk    []DiskPoint   `json:"disk"`
	Network []NetPoint    `json:"network"`
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

func NewService(db *sql.DB, servers *server.Service, exec sshx.RemoteExecutor) *Service {
	return &Service{db: db, servers: servers, exec: exec, adapter: linux.DebianAdapter{}}
}

func (s *Service) Collect(ctx context.Context, serverID string) error {
	srv, err := s.servers.Get(ctx, serverID)
	if err != nil {
		return err
	}
	if !srv.OS.Supported {
		return panelerr.Validation("server_not_supported", "Server distribution is not supported")
	}
	snap, err := s.adapter.CollectMetrics(ctx, s.exec, srv.Target())
	if err != nil {
		return err
	}
	snap.ServerID = serverID
	return s.Save(ctx, snap)
}

func (s *Service) Save(ctx context.Context, snap linux.MetricsSnapshot) error {
	if snap.Time.IsZero() {
		snap.Time = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO metrics_snapshots(server_id,time,cpu_usage_percent,memory_used_bytes,memory_total_bytes,disk_used_bytes,disk_total_bytes,network_rx_bps,network_tx_bps,load_average,uptime_seconds,hostname,kernel_version,os_version) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		snap.ServerID, snap.Time.Format(time.RFC3339Nano), snap.CPUUsagePercent, snap.MemoryUsedBytes, snap.MemoryTotalBytes, snap.DiskUsedBytes, snap.DiskTotalBytes, snap.NetworkRxBytesRate, snap.NetworkTxBytesRate, snap.Status.LoadAverage, snap.Status.UptimeSeconds, snap.Status.Hostname, snap.Status.KernelVersion, snap.Status.OSVersion)
	return err
}

func (s *Service) Query(ctx context.Context, serverID, rng string) (Series, error) {
	duration := map[string]time.Duration{"1h": time.Hour, "6h": 6 * time.Hour, "24h": 24 * time.Hour}[rng]
	if duration == 0 {
		return Series{}, panelerr.Validation("range_invalid", "Range must be 1h, 6h, or 24h")
	}
	since := time.Now().UTC().Add(-duration).Format(time.RFC3339Nano)
	rows, err := s.db.QueryContext(ctx, `SELECT time,cpu_usage_percent,memory_used_bytes,memory_total_bytes,disk_used_bytes,disk_total_bytes,network_rx_bps,network_tx_bps FROM metrics_snapshots WHERE server_id=? AND time>=? ORDER BY time ASC`, serverID, since)
	if err != nil {
		return Series{}, err
	}
	defer rows.Close()
	series := Series{Range: rng, CPU: []CPUPoint{}, Memory: []MemoryPoint{}, Disk: []DiskPoint{}, Network: []NetPoint{}}
	for rows.Next() {
		var ts string
		var cpu CPUPoint
		var mem MemoryPoint
		var disk DiskPoint
		var netp NetPoint
		if err := rows.Scan(&ts, &cpu.UsagePercent, &mem.UsedBytes, &mem.TotalBytes, &disk.UsedBytes, &disk.TotalBytes, &netp.RxBytesPerSecond, &netp.TxBytesPerSecond); err != nil {
			return Series{}, err
		}
		t, _ := time.Parse(time.RFC3339Nano, ts)
		cpu.Time, mem.Time, disk.Time, netp.Time = t, t, t, t
		series.CPU = append(series.CPU, cpu)
		series.Memory = append(series.Memory, mem)
		series.Disk = append(series.Disk, disk)
		series.Network = append(series.Network, netp)
	}
	return series, rows.Err()
}

func (s *Service) Cleanup(ctx context.Context, retentionDays int) (int64, error) {
	cutoff := time.Now().UTC().Add(-time.Duration(retentionDays) * 24 * time.Hour).Format(time.RFC3339Nano)
	res, err := s.db.ExecContext(ctx, `DELETE FROM metrics_snapshots WHERE time < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *Service) LatestAt(ctx context.Context, serverID string) (*time.Time, error) {
	var ts sql.NullString
	if err := s.db.QueryRowContext(ctx, `SELECT MAX(time) FROM metrics_snapshots WHERE server_id=?`, serverID).Scan(&ts); err != nil {
		return nil, err
	}
	if !ts.Valid || ts.String == "" {
		return nil, nil
	}
	v, _ := time.Parse(time.RFC3339Nano, ts.String)
	return &v, nil
}

func (s *Service) LatestLoad(ctx context.Context, serverID string) (string, error) {
	var load sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT load_average FROM metrics_snapshots WHERE server_id=? ORDER BY time DESC LIMIT 1`, serverID).Scan(&load)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if !load.Valid {
		return "", nil
	}
	return load.String, nil
}

