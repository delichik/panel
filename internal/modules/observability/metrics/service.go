package metrics

import (
	"context"
	"database/sql"
	"strings"
	"time"

	agentcontract "panel/internal/agent/contract"
	"panel/internal/modules/servers"
	"panel/internal/platform/database/models"
	"panel/internal/platform/database/orm"
	panelerr "panel/internal/platform/errors"
	"panel/internal/platform/linux"
	"panel/internal/platform/ssh"
)

type Service struct {
	db          *sql.DB
	servers     serverProvider
	agentErrors agentErrorReporter
	exec        sshx.RemoteExecutor
	agent       agentcontract.Client
}

type serverProvider interface {
	Get(context.Context, string) (server.Server, error)
}

type agentErrorReporter interface {
	HandleAgentError(context.Context, server.Server, error) bool
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

type Option func(*Service)

func WithAgentClient(client agentcontract.Client) Option {
	return func(s *Service) { s.agent = client }
}

func NewService(db *sql.DB, servers serverProvider, exec sshx.RemoteExecutor, opts ...Option) *Service {
	s := &Service{db: db, servers: servers, exec: exec}
	if reporter, ok := servers.(agentErrorReporter); ok {
		s.agentErrors = reporter
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *Service) Collect(ctx context.Context, serverID string) error {
	return s.CollectAt(ctx, serverID, time.Now().UTC())
}

func (s *Service) CollectAt(ctx context.Context, serverID string, collectedAt time.Time) error {
	srv, err := s.servers.Get(ctx, serverID)
	if err != nil {
		return err
	}
	if !srv.OS.Supported {
		return panelerr.Validation("server_not_supported", "Server distribution is not supported")
	}
	snap, err := s.collectSnapshot(ctx, srv)
	if err != nil {
		if reporter, ok := s.servers.(reachabilityReporter); ok {
			_ = reporter.RecordMetricsReachability(ctx, serverID, false, err.Error())
		}
		return err
	}
	snap.ServerID = serverID
	snap.Time = collectedAt
	if err := s.Save(ctx, snap); err != nil {
		return err
	}
	if reporter, ok := s.servers.(reachabilityReporter); ok {
		_ = reporter.RecordMetricsReachability(ctx, serverID, true, "")
	}
	return nil
}

func (s *Service) collectSnapshot(ctx context.Context, srv server.Server) (linux.MetricsSnapshot, error) {
	baseURL, err := metricAgentURL(srv)
	if err != nil {
		return linux.MetricsSnapshot{}, err
	}
	if s.agent == nil {
		return linux.MetricsSnapshot{}, panelerr.Validation("agent_runtime_unavailable", "Agent runtime client is unavailable")
	}
	snap, err := s.agent.MetricsSnapshot(ctx, baseURL, srv.ID)
	if err != nil && s.agentErrors != nil {
		_ = s.agentErrors.HandleAgentError(ctx, srv, err)
	}
	return snap, err
}

func metricAgentURL(srv server.Server) (string, error) {
	if !agentTraitEnabled(srv.Traits[agentcontract.TraitEnabled]) {
		return "", panelerr.Validation("agent_required", "Agent is required for metrics collection")
	}
	url := strings.TrimSpace(srv.Traits[agentcontract.TraitURL])
	if url == "" {
		return "", panelerr.Validation("agent_required", "Agent is required for metrics collection")
	}
	if srv.Traits[agentcontract.TraitStatus] != agentcontract.StatusCompatible {
		return "", panelerr.Validation("agent_incompatible", "Agent is not compatible with metrics collection")
	}
	return url, nil
}

func agentTraitEnabled(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "1", "yes", "on":
		return true
	default:
		return false
	}
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
		return Series{}, panelerr.Validation("range_invalid", "Range must be 1h, 6h, 1d, or 7d")
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
	cutoff := time.Now().UTC().Add(-time.Duration(retentionDays) * 24 * time.Hour).Format(time.RFC3339Nano)
	res, err := orm.RawExec(ctx, s.db, `DELETE FROM metrics_snapshots WHERE time < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *Service) LatestAt(ctx context.Context, serverID string) (*time.Time, error) {
	var ts sql.NullString
	if err := orm.RawRow(ctx, s.db, `SELECT MAX(time) FROM metrics_snapshots WHERE server_id=?`, serverID).Scan(&ts); err != nil {
		return nil, err
	}
	if !ts.Valid || ts.String == "" {
		return nil, nil
	}
	v, _ := time.Parse(time.RFC3339Nano, ts.String)
	return &v, nil
}

func (s *Service) LatestLoad(ctx context.Context, serverID string) (string, error) {
	var row models.MetricsSnapshot
	err := orm.New(s.db).From("metrics_snapshots").Where("server_id = ?", serverID).OrderBy("time DESC").First(ctx, &row)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return row.LoadAverage, nil
}

func alignMetricTime(t time.Time) time.Time {
	return t.UTC().Truncate(time.Second)
}
