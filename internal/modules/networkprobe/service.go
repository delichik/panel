// Package networkprobe implements the network-probe facility: its persisted
// configuration, the probe targets handed to agents over the report stream, the
// reported ping results and the read models used by the facility page, the
// server detail tab and the overview card.
package networkprobe

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	server "panel/internal/modules/servers"
	"panel/internal/platform/database/orm"
	panelerr "panel/internal/platform/errors"
)

const (
	// ModeAll 表示所有节点都是探测节点。
	ModeAll = "all"
	// ModeSelected 表示只有配置列表内的节点是探测节点。
	ModeSelected = "selected"
	// ProbeIntervalSeconds 是下发给 Agent 的探测周期（秒）。
	ProbeIntervalSeconds = 60
	// ResultFreshFor 是探测结果被视为新鲜的时长；超过即标记 Stale。
	ResultFreshFor = 5 * time.Minute

	configID = "network_probe"
	// snapshotTTL 是 ReportConfig 的配置+服务器列表快照有效期。上报流每秒都会
	// 重建 config，不能每次都查库。
	snapshotTTL = 3 * time.Second
)

// Config 是网络探测设施的对外配置。
type Config struct {
	Mode      string    `json:"mode"`
	ServerIDs []string  `json:"serverIds"`
	Version   int       `json:"version"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// SaveInput 是保存网络探测设施的输入。
type SaveInput struct {
	Mode      string   `json:"mode"`
	ServerIDs []string `json:"serverIds"`
}

// Node 是一个探测节点（同时也是其它探测节点的目标）。
type Node struct {
	ServerID string `json:"serverId"`
	Name     string `json:"name"`
	Address  string `json:"address"`
}

// Result 是某探测节点到某目标的最新一次 ping 观测。
type Result struct {
	SourceServerID    string    `json:"sourceServerId"`
	TargetServerID    string    `json:"targetServerId"`
	TargetAddress     string    `json:"targetAddress"`
	LatencyMS         float64   `json:"latencyMs"`
	PacketLossPercent float64   `json:"packetLossPercent"`
	Sent              int       `json:"sent"`
	Received          int       `json:"received"`
	SampledAt         time.Time `json:"sampledAt"`
	Error             string    `json:"error,omitempty"`
	Stale             bool      `json:"stale"`
}

// Matrix 是设施页的探测矩阵：节点列表 + 每个探测节点到每个目标的最近结果。
type Matrix struct {
	Mode      string     `json:"mode"`
	Nodes     []Node     `json:"nodes"`
	Results   []Result   `json:"results"`
	UpdatedAt *time.Time `json:"updatedAt"`
}

// ServerResults 是单台服务器的探测页签数据。
type ServerResults struct {
	ServerID  string     `json:"serverId"`
	Enabled   bool       `json:"enabled"`
	Nodes     []Node     `json:"nodes"`
	Results   []Result   `json:"results"`
	UpdatedAt *time.Time `json:"updatedAt"`
}

// CardData 是概览 networkProbe 卡片的数据。
type CardData struct {
	Nodes     []Node     `json:"nodes"`
	Results   []Result   `json:"results"`
	UpdatedAt *time.Time `json:"updatedAt"`
}

// ProbeTarget 是下发给 Agent 的一个 ping 目标。
type ProbeTarget struct {
	ServerID string
	Address  string
	Name     string
}

// ProbeReport 是下发给单个探测节点的探测配置。Enabled 为 false 时 Agent 停止
// 探测；Targets 已排除探测节点自身。
type ProbeReport struct {
	Enabled         bool
	IntervalSeconds int
	Targets         []ProbeTarget
}

// ReportedResult 是 Agent 上报的一条探测结果。
type ReportedResult struct {
	TargetServerID    string
	TargetAddress     string
	LatencyMS         float64
	PacketLossPercent float64
	Sent              int
	Received          int
	Error             string
}

// serverProvider 是探测设施读取服务器清单所需的最小接口，*server.Service 满足。
type serverProvider interface {
	List(ctx context.Context) ([]server.Server, error)
}

// snapshot 是 ReportConfig 使用的短 TTL 配置 + 服务器列表快照。
type snapshot struct {
	loadedAt time.Time
	config   Config
	servers  []server.Server
}

// Service 是网络探测设施服务。
type Service struct {
	db      *sql.DB
	servers serverProvider
	now     func() time.Time

	mu    sync.Mutex
	cache *snapshot
}

// NewService 创建网络探测设施服务。
func NewService(db *sql.DB, servers serverProvider) *Service {
	return &Service{
		db:      db,
		servers: servers,
		now:     func() time.Time { return time.Now().UTC() },
	}
}

// Get 返回当前设施配置。从未保存过时返回 mode=all 的空配置。
func (s *Service) Get(ctx context.Context) (Config, error) {
	return s.loadConfig(ctx)
}

// Save 保存设施配置：mode 只能是 all/selected；selected 模式必须至少选择一个
// 已存在的节点；版本单调加一。
func (s *Service) Save(ctx context.Context, in SaveInput) (Config, error) {
	mode := strings.TrimSpace(in.Mode)
	if mode != ModeAll && mode != ModeSelected {
		return Config{}, panelerr.Validation("network_probe_mode_invalid", "mode must be all or selected")
	}
	ids := normalizeIDs(in.ServerIDs)
	all, err := s.servers.List(ctx)
	if err != nil {
		return Config{}, err
	}
	if mode == ModeSelected {
		if len(ids) == 0 {
			return Config{}, panelerr.Validation("network_probe_servers_required", "at least one probe server is required when mode is selected")
		}
		known := make(map[string]struct{}, len(all))
		for _, srv := range all {
			known[srv.ID] = struct{}{}
		}
		for _, id := range ids {
			if _, ok := known[id]; !ok {
				return Config{}, panelerr.Validation("network_probe_server_not_found", "probe server not found: "+id)
			}
		}
	} else {
		// all 模式按现存节点探测，持久化列表没有意义；清空避免删除节点后留下
		// 过期的 selected 残留。
		ids = []string{}
	}
	previous, err := s.loadConfig(ctx)
	if err != nil {
		return Config{}, err
	}
	now := s.now().UTC()
	cfg := Config{Mode: mode, ServerIDs: ids, Version: previous.Version + 1, UpdatedAt: now}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return Config{}, err
	}
	if _, err := orm.RawExec(ctx, s.db, `INSERT INTO network_probe_configs(id,mode,server_ids_json,version,updated_at)
VALUES(?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET mode=excluded.mode,server_ids_json=excluded.server_ids_json,version=excluded.version,updated_at=excluded.updated_at`,
		configID, cfg.Mode, string(encoded), cfg.Version, formatTime(cfg.UpdatedAt)); err != nil {
		return Config{}, err
	}
	s.invalidate()
	return cfg, nil
}

// ReportConfig 返回下发给指定探测节点的探测配置。配置与服务器列表使用 3 秒
// TTL 快照，避免上报流每个 tick 都查库。
func (s *Service) ReportConfig(ctx context.Context, serverID string) (ProbeReport, error) {
	cfg, all, err := s.snapshot(ctx)
	if err != nil {
		return ProbeReport{}, err
	}
	report := ProbeReport{Enabled: isProbeNode(cfg, serverID), IntervalSeconds: ProbeIntervalSeconds}
	if !report.Enabled {
		return report, nil
	}
	// 已从 Panel 删除的节点不应再收到探测目标；上报流会在下一次 sync 回收。
	if _, ok := serverByID(all)[strings.TrimSpace(serverID)]; !ok {
		return ProbeReport{Enabled: false, IntervalSeconds: ProbeIntervalSeconds}, nil
	}
	report.Targets = targetsForServer(serverID, all)
	return report, nil
}

// SaveReported 用一次上报替换该探测节点的全部结果。事务内先删除旧行再批量插入，
// 保证“每个 (source,target) 只保留最新一行”；sampleAt 统一 UTC 截断到秒。
func (s *Service) SaveReported(ctx context.Context, sourceServerID string, sampleAt time.Time, results []ReportedResult) error {
	source := strings.TrimSpace(sourceServerID)
	if source == "" {
		return fmt.Errorf("network probe source server is required")
	}
	at := sampleAt.UTC().Truncate(time.Second)
	if at.IsZero() {
		at = s.now().UTC().Truncate(time.Second)
	}
	return orm.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		if _, err := orm.RawExec(ctx, tx, `DELETE FROM network_probe_results WHERE source_server_id=?`, source); err != nil {
			return err
		}
		for _, result := range results {
			target := strings.TrimSpace(result.TargetServerID)
			if target == "" || target == source {
				continue
			}
			if _, err := orm.RawExec(ctx, tx, `INSERT INTO network_probe_results(source_server_id,target_server_id,target_address,sampled_at,latency_ms,packet_loss_percent,sent,received,error)
VALUES(?,?,?,?,?,?,?,?,?)
ON CONFLICT(source_server_id,target_server_id) DO UPDATE SET target_address=excluded.target_address,sampled_at=excluded.sampled_at,latency_ms=excluded.latency_ms,packet_loss_percent=excluded.packet_loss_percent,sent=excluded.sent,received=excluded.received,error=excluded.error`,
				source, target, strings.TrimSpace(result.TargetAddress), formatTime(at),
				sanitizeFloat(result.LatencyMS), sanitizeFloat(result.PacketLossPercent),
				result.Sent, result.Received, strings.TrimSpace(result.Error)); err != nil {
				return err
			}
		}
		return nil
	})
}

// Matrix 返回设施页矩阵：nodes 为探测节点（可选按 filterServerIDs 过滤），
// results 只包含 source 属于 nodes 且 target 仍存在的行。
func (s *Service) Matrix(ctx context.Context, filterServerIDs []string) (Matrix, error) {
	cfg, all, err := s.snapshot(ctx)
	if err != nil {
		return Matrix{}, err
	}
	nodes := probeNodes(cfg, all)
	if filter := normalizeIDs(filterServerIDs); len(filter) > 0 {
		nodes = filterNodes(nodes, filter)
	}
	results, err := s.results(ctx, nodeIDSet(nodes), serverIDSet(all))
	if err != nil {
		return Matrix{}, err
	}
	return Matrix{Mode: cfg.Mode, Nodes: nodes, Results: results, UpdatedAt: latestSample(results)}, nil
}

// ServerResults 返回指定服务器的探测页签数据：是否为探测节点、目标节点表和该
// 节点作为 source 的结果（target 仍需存在）。
func (s *Service) ServerResults(ctx context.Context, serverID string) (ServerResults, error) {
	id := strings.TrimSpace(serverID)
	cfg, all, err := s.snapshot(ctx)
	if err != nil {
		return ServerResults{}, err
	}
	if _, ok := serverByID(all)[id]; !ok {
		return ServerResults{}, panelerr.NotFound("server")
	}
	results, err := s.results(ctx, map[string]struct{}{id: {}}, serverIDSet(all))
	if err != nil {
		return ServerResults{}, err
	}
	return ServerResults{
		ServerID:  id,
		Enabled:   isProbeNode(cfg, id),
		Nodes:     targetNodes(id, all),
		Results:   results,
		UpdatedAt: latestSample(results),
	}, nil
}

// CardData 返回概览卡片数据。serverIDs 为空表示不按卡片选择过滤；非空时只保留
// 该集合内、且当前确实是探测节点的 source 结果。
func (s *Service) CardData(ctx context.Context, serverIDs []string) (CardData, error) {
	cfg, all, err := s.snapshot(ctx)
	if err != nil {
		return CardData{}, err
	}
	nodes := probeNodes(cfg, all)
	if filter := normalizeIDs(serverIDs); len(filter) > 0 {
		nodes = filterNodes(nodes, filter)
	}
	results, err := s.results(ctx, nodeIDSet(nodes), serverIDSet(all))
	if err != nil {
		return CardData{}, err
	}
	return CardData{Nodes: nodes, Results: results, UpdatedAt: latestSample(results)}, nil
}

func (s *Service) invalidate() {
	s.mu.Lock()
	s.cache = nil
	s.mu.Unlock()
}

func (s *Service) snapshot(ctx context.Context) (Config, []server.Server, error) {
	now := s.now()
	s.mu.Lock()
	if cached := s.cache; cached != nil && now.Sub(cached.loadedAt) < snapshotTTL {
		out := append([]server.Server(nil), cached.servers...)
		cfg := cached.config
		s.mu.Unlock()
		return cfg, out, nil
	}
	s.mu.Unlock()
	cfg, err := s.loadConfig(ctx)
	if err != nil {
		return Config{}, nil, err
	}
	all, err := s.servers.List(ctx)
	if err != nil {
		return Config{}, nil, err
	}
	snapshotServers := append([]server.Server(nil), all...)
	s.mu.Lock()
	s.cache = &snapshot{loadedAt: now, config: cfg, servers: snapshotServers}
	s.mu.Unlock()
	return cfg, snapshotServers, nil
}

func (s *Service) loadConfig(ctx context.Context) (Config, error) {
	cfg := Config{Mode: ModeAll, ServerIDs: []string{}}
	var mode, rawIDs, updatedAt string
	var version int
	err := s.db.QueryRowContext(ctx, `SELECT mode,server_ids_json,version,updated_at FROM network_probe_configs WHERE id=?`, configID).
		Scan(&mode, &rawIDs, &version, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return cfg, nil
	}
	if err != nil {
		return Config{}, err
	}
	cfg.Mode = normalizeMode(mode)
	cfg.ServerIDs = decodeIDs(rawIDs)
	cfg.Version = version
	cfg.UpdatedAt = parseTime(updatedAt)
	return cfg, nil
}

func (s *Service) results(ctx context.Context, sources, targets map[string]struct{}) ([]Result, error) {
	if len(sources) == 0 {
		return []Result{}, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT source_server_id,target_server_id,target_address,sampled_at,latency_ms,packet_loss_percent,sent,received,error FROM network_probe_results`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	now := s.now()
	out := []Result{}
	for rows.Next() {
		var item Result
		var sampledAt string
		if err := rows.Scan(&item.SourceServerID, &item.TargetServerID, &item.TargetAddress, &sampledAt, &item.LatencyMS, &item.PacketLossPercent, &item.Sent, &item.Received, &item.Error); err != nil {
			return nil, err
		}
		if _, ok := sources[item.SourceServerID]; !ok {
			continue
		}
		if _, ok := targets[item.TargetServerID]; !ok {
			continue
		}
		item.SampledAt = parseTime(sampledAt)
		item.Stale = item.SampledAt.IsZero() || now.Sub(item.SampledAt) > ResultFreshFor
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].SourceServerID != out[j].SourceServerID {
			return out[i].SourceServerID < out[j].SourceServerID
		}
		return out[i].TargetServerID < out[j].TargetServerID
	})
	return out, nil
}

func probeNodes(cfg Config, all []server.Server) []Node {
	selected := make(map[string]struct{}, len(cfg.ServerIDs))
	for _, id := range cfg.ServerIDs {
		selected[id] = struct{}{}
	}
	nodes := []Node{}
	for _, srv := range all {
		if cfg.Mode == ModeSelected {
			if _, ok := selected[srv.ID]; !ok {
				continue
			}
		}
		nodes = append(nodes, Node{ServerID: srv.ID, Name: srv.Name, Address: addressFor(srv)})
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ServerID < nodes[j].ServerID })
	return nodes
}

func targetNodes(serverID string, all []server.Server) []Node {
	nodes := make([]Node, 0, len(all))
	for _, target := range targetsForServer(serverID, all) {
		nodes = append(nodes, Node{ServerID: target.ServerID, Address: target.Address, Name: target.Name})
	}
	return nodes
}

func targetsForServer(serverID string, all []server.Server) []ProbeTarget {
	targets := make([]ProbeTarget, 0, len(all))
	seen := make(map[string]struct{}, len(all))
	for _, srv := range all {
		if srv.ID == serverID {
			continue
		}
		if _, ok := seen[srv.ID]; ok {
			continue
		}
		address := addressFor(srv)
		if address == "" {
			continue
		}
		seen[srv.ID] = struct{}{}
		targets = append(targets, ProbeTarget{ServerID: srv.ID, Address: address, Name: srv.Name})
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].ServerID < targets[j].ServerID })
	return targets
}

func isProbeNode(cfg Config, serverID string) bool {
	id := strings.TrimSpace(serverID)
	if id == "" {
		return false
	}
	if cfg.Mode != ModeSelected {
		return true
	}
	for _, candidate := range cfg.ServerIDs {
		if candidate == id {
			return true
		}
	}
	return false
}

func filterNodes(nodes []Node, filter []string) []Node {
	allowed := make(map[string]struct{}, len(filter))
	for _, id := range filter {
		allowed[id] = struct{}{}
	}
	out := make([]Node, 0, len(nodes))
	for _, node := range nodes {
		if _, ok := allowed[node.ServerID]; ok {
			out = append(out, node)
		}
	}
	return out
}

func nodeIDSet(nodes []Node) map[string]struct{} {
	out := make(map[string]struct{}, len(nodes))
	for _, node := range nodes {
		out[node.ServerID] = struct{}{}
	}
	return out
}

func serverIDSet(all []server.Server) map[string]struct{} {
	out := make(map[string]struct{}, len(all))
	for _, srv := range all {
		out[srv.ID] = struct{}{}
	}
	return out
}

func serverByID(all []server.Server) map[string]server.Server {
	out := make(map[string]server.Server, len(all))
	for _, srv := range all {
		out[srv.ID] = srv
	}
	return out
}

// addressFor 返回 Agent 探测目标应使用的地址：IPv4 优先，缺失时取 IPv6，最后
// 回落到服务器连接地址。
func addressFor(srv server.Server) string {
	for _, value := range []string{srv.IPv4, srv.IPv6, srv.Host} {
		if address := strings.TrimSpace(value); address != "" {
			return address
		}
	}
	return ""
}

func latestSample(results []Result) *time.Time {
	var latest time.Time
	for _, result := range results {
		if result.SampledAt.After(latest) {
			latest = result.SampledAt
		}
	}
	if latest.IsZero() {
		return nil
	}
	return &latest
}

func normalizeMode(mode string) string {
	switch strings.TrimSpace(mode) {
	case "", ModeAll:
		return ModeAll
	case ModeSelected:
		return ModeSelected
	default:
		return strings.TrimSpace(mode)
	}
}

// normalizeIDs 去空白、去重并按 serverID 升序排列。
func normalizeIDs(ids []string) []string {
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func decodeIDs(raw string) []string {
	ids := []string{}
	if err := json.Unmarshal([]byte(raw), &ids); err != nil {
		return []string{}
	}
	return normalizeIDs(ids)
}

func sanitizeFloat(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	return value
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func parseTime(value string) time.Time {
	if strings.TrimSpace(value) == "" {
		return time.Time{}
	}
	parsed, _ := time.Parse(time.RFC3339Nano, value)
	if parsed.IsZero() {
		parsed, _ = time.Parse(time.RFC3339, value)
	}
	return parsed.UTC()
}
