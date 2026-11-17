// Package metricsfacility 实现“指标”设施：保存指标采集范围（全部节点或指定
// 节点）、保留天数、采集间隔与清理计划，并向 Agent 上报流与指标清理 worker
// 提供带 3 秒 TTL 的快照读取。
//
// 配置是 AppDB metrics_facility_configs 的单行（id='metrics'）。表由
// AutoMigrate 创建；首次启动时 EnsureMigrated 从旧 runtime_settings 的
// metricsRetentionDays/metricsCollectionIntervalSeconds/cleanupSchedule 三项播种，
// 播种只做一次，旧键保留供旧备份恢复再次播种。
package metricsfacility

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	server "panel/internal/modules/servers"
	"panel/internal/platform/database/orm"
	panelerr "panel/internal/platform/errors"
)

const (
	// ModeAll 表示所有节点都采集指标。
	ModeAll = "all"
	// ModeSelected 表示只有配置列表内的节点采集指标。
	ModeSelected = "selected"
	// DefaultRetentionDays 是指标默认保留天数。
	DefaultRetentionDays = 7
	// DefaultCollectionIntervalSeconds 是指标默认采集间隔（秒）。
	DefaultCollectionIntervalSeconds = 60
	// DefaultCleanupSchedule 是指标默认清理计划。
	DefaultCleanupSchedule = "daily"

	// configID 是单行配置的固定主键。
	configID = "metrics"
	// snapshotTTL 是上报流/清理 worker 快照的有效期。上报流每秒都会重建配置，
	// 不能每次都查库。
	snapshotTTL = 3 * time.Second

	// 旧 runtime_settings 键，仅用于首次播种。
	legacyRetentionKey        = "metricsRetentionDays"
	legacyIntervalKey         = "metricsCollectionIntervalSeconds"
	legacyCleanupScheduleKey  = "cleanupSchedule"
	cleanupScheduleHourly     = "hourly"
	cleanupScheduleDaily      = "daily"
	cleanupScheduleWeekly     = "weekly"
	cleanupScheduleInvalidMsg = "cleanupSchedule must be hourly, daily or weekly"
)

// Config 是指标设施的对外配置。从未保存过时 mode=all、version=0、
// updatedAt 为零值。
type Config struct {
	Mode                      string    `json:"mode"`
	ServerIDs                 []string  `json:"serverIds"`
	RetentionDays             int       `json:"retentionDays"`
	CollectionIntervalSeconds int       `json:"collectionIntervalSeconds"`
	CleanupSchedule           string    `json:"cleanupSchedule"`
	Version                   int       `json:"version"`
	UpdatedAt                 time.Time `json:"updatedAt"`
}

// SaveInput 是保存指标设施的输入；不含 version/updatedAt。
type SaveInput struct {
	Mode                      string   `json:"mode"`
	ServerIDs                 []string `json:"serverIds"`
	RetentionDays             int      `json:"retentionDays"`
	CollectionIntervalSeconds int      `json:"collectionIntervalSeconds"`
	CleanupSchedule           string   `json:"cleanupSchedule"`
}

// ReportConfig 是下发给单个节点的指标上报配置。Enabled 为 false 时 Agent 停止
// 采集指标；禁用时 IntervalSeconds 为 0。
type ReportConfig struct {
	Enabled         bool
	IntervalSeconds int
}

// CleanupSettings 是指标清理 worker 使用的保留天数与清理计划。
type CleanupSettings struct {
	RetentionDays int
	Schedule      string
}

// serverProvider 是指标设施读取服务器清单所需的最小接口，*server.Service 满足。
type serverProvider interface {
	List(ctx context.Context) ([]server.Server, error)
}

type configSnapshot struct {
	loadedAt time.Time
	config   Config
}

type serverSnapshot struct {
	loadedAt time.Time
	servers  []server.Server
}

// Service 是指标设施服务。
type Service struct {
	db      *sql.DB
	servers serverProvider
	now     func() time.Time

	mu          sync.Mutex
	configCache *configSnapshot
	serverCache *serverSnapshot
}

// NewService 创建指标设施服务。
func NewService(db *sql.DB, servers serverProvider) *Service {
	return &Service{
		db:      db,
		servers: servers,
		now:     func() time.Time { return time.Now().UTC() },
	}
}

// EnsureMigrated 在表为空时把旧运行时设置播种进指标设施配置，只做一次。
// 旧键缺失或非法时对应字段使用默认值；旧键本身保留不删除。启动装配调用，
// 失败应导致启动失败。
func (s *Service) EnsureMigrated(ctx context.Context) error {
	var existing int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM metrics_facility_configs WHERE id=?`, configID).Scan(&existing); err != nil {
		return err
	}
	if existing > 0 {
		return nil
	}
	seed, err := s.seedValues(ctx)
	if err != nil {
		return err
	}
	cfg := Config{
		Mode:                      ModeAll,
		ServerIDs:                 []string{},
		RetentionDays:             seed.RetentionDays,
		CollectionIntervalSeconds: seed.CollectionIntervalSeconds,
		CleanupSchedule:           seed.CleanupSchedule,
		Version:                   1,
		UpdatedAt:                 s.now().UTC(),
	}
	encoded, err := json.Marshal(cfg.ServerIDs)
	if err != nil {
		return err
	}
	if _, err := orm.RawExec(ctx, s.db, `INSERT INTO metrics_facility_configs(id,mode,server_ids_json,retention_days,collection_interval_seconds,cleanup_schedule,version,updated_at)
VALUES(?,?,?,?,?,?,?,?)
ON CONFLICT(id) DO NOTHING`,
		configID, cfg.Mode, string(encoded), cfg.RetentionDays, cfg.CollectionIntervalSeconds, cfg.CleanupSchedule, cfg.Version, formatTime(cfg.UpdatedAt)); err != nil {
		return err
	}
	s.invalidate()
	return nil
}

// Get 返回当前设施配置。从未保存过（或尚未播种）时返回默认配置、version=0。
func (s *Service) Get(ctx context.Context) (Config, error) {
	return s.loadConfig(ctx)
}

// Save 校验并保存设施配置：mode 只能是 all/selected；selected 时 serverIDs 必须
// 非空且每项当前存在；retentionDays/collectionIntervalSeconds 均须 >= 1；
// cleanupSchedule 只能是 hourly/daily/weekly。任一校验失败不写库、不 bump
// version。成功写单行 upsert、version+1、更新 updated_at，并使配置快照失效。
func (s *Service) Save(ctx context.Context, in SaveInput) (Config, error) {
	mode := strings.TrimSpace(in.Mode)
	if mode != ModeAll && mode != ModeSelected {
		return Config{}, panelerr.Validation("metrics_facility_mode_invalid", "mode must be all or selected")
	}
	if in.RetentionDays < 1 {
		return Config{}, panelerr.Validation("metrics_facility_retention_invalid", "retentionDays must be at least 1")
	}
	if in.CollectionIntervalSeconds < 1 {
		return Config{}, panelerr.Validation("metrics_facility_interval_invalid", "collectionIntervalSeconds must be at least 1")
	}
	schedule := normalizeCleanupSchedule(in.CleanupSchedule)
	if schedule == "" {
		return Config{}, panelerr.Validation("metrics_facility_cleanup_schedule_invalid", cleanupScheduleInvalidMsg)
	}
	ids := []string{}
	if mode == ModeSelected {
		ids = normalizeIDs(in.ServerIDs)
		if len(ids) == 0 {
			return Config{}, panelerr.Validation("metrics_facility_servers_required", "at least one server is required when mode is selected")
		}
		all, err := s.servers.List(ctx)
		if err != nil {
			return Config{}, err
		}
		known := make(map[string]struct{}, len(all))
		for _, srv := range all {
			known[srv.ID] = struct{}{}
		}
		for _, id := range ids {
			if _, ok := known[id]; !ok {
				return Config{}, panelerr.Validation("metrics_facility_server_not_found", "server not found: "+id)
			}
		}
	}
	previous, err := s.loadConfig(ctx)
	if err != nil {
		return Config{}, err
	}
	now := s.now().UTC()
	cfg := Config{
		Mode:                      mode,
		ServerIDs:                 ids,
		RetentionDays:             in.RetentionDays,
		CollectionIntervalSeconds: in.CollectionIntervalSeconds,
		CleanupSchedule:           schedule,
		Version:                   previous.Version + 1,
		UpdatedAt:                 now,
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return Config{}, err
	}
	if _, err := orm.RawExec(ctx, s.db, `INSERT INTO metrics_facility_configs(id,mode,server_ids_json,retention_days,collection_interval_seconds,cleanup_schedule,version,updated_at)
VALUES(?,?,?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET mode=excluded.mode,server_ids_json=excluded.server_ids_json,retention_days=excluded.retention_days,collection_interval_seconds=excluded.collection_interval_seconds,cleanup_schedule=excluded.cleanup_schedule,version=excluded.version,updated_at=excluded.updated_at`,
		configID, cfg.Mode, string(encoded), cfg.RetentionDays, cfg.CollectionIntervalSeconds, cfg.CleanupSchedule, cfg.Version, formatTime(cfg.UpdatedAt)); err != nil {
		return Config{}, err
	}
	s.invalidate()
	return cfg, nil
}

// ReportConfig 返回下发给指定服务器的指标上报配置。mode=all 时所有现存服务器
// 启用；mode=selected 时仅选中且仍存在的服务器启用；已删除节点返回 disabled。
// 配置与服务器列表使用 3 秒 TTL 快照，避免上报流每个 tick 都查库。
func (s *Service) ReportConfig(ctx context.Context, serverID string) (ReportConfig, error) {
	cfg, err := s.cachedConfig(ctx)
	if err != nil {
		return ReportConfig{}, err
	}
	id := strings.TrimSpace(serverID)
	if id == "" {
		return ReportConfig{}, nil
	}
	all, err := s.cachedServers(ctx)
	if err != nil {
		return ReportConfig{}, err
	}
	if _, ok := serverByID(all)[id]; !ok {
		return ReportConfig{}, nil
	}
	if cfg.Mode == ModeSelected && !containsID(cfg.ServerIDs, id) {
		return ReportConfig{}, nil
	}
	return ReportConfig{Enabled: true, IntervalSeconds: cfg.CollectionIntervalSeconds}, nil
}

// IntervalSeconds 返回采集间隔，带 3 秒 TTL 快照；读失败回退默认值。
func (s *Service) IntervalSeconds() int {
	cfg, err := s.cachedConfig(context.Background())
	if err != nil {
		return DefaultCollectionIntervalSeconds
	}
	return cfg.CollectionIntervalSeconds
}

// CleanupSettings 返回保留天数与清理计划，带 3 秒 TTL 快照；读失败回退默认值。
func (s *Service) CleanupSettings() CleanupSettings {
	cfg, err := s.cachedConfig(context.Background())
	if err != nil {
		return CleanupSettings{RetentionDays: DefaultRetentionDays, Schedule: DefaultCleanupSchedule}
	}
	return CleanupSettings{RetentionDays: cfg.RetentionDays, Schedule: cfg.CleanupSchedule}
}

func (s *Service) invalidate() {
	s.mu.Lock()
	s.configCache = nil
	s.serverCache = nil
	s.mu.Unlock()
}

// cachedConfig 返回 3 秒内加载过的配置快照。保存成功后由 invalidate 清空。
func (s *Service) cachedConfig(ctx context.Context) (Config, error) {
	now := s.now()
	s.mu.Lock()
	if cached := s.configCache; cached != nil && now.Sub(cached.loadedAt) < snapshotTTL {
		cfg := cached.config
		s.mu.Unlock()
		return cfg, nil
	}
	s.mu.Unlock()
	cfg, err := s.loadConfig(ctx)
	if err != nil {
		return Config{}, err
	}
	s.mu.Lock()
	s.configCache = &configSnapshot{loadedAt: now, config: cfg}
	s.mu.Unlock()
	return cfg, nil
}

// cachedServers 返回 3 秒内加载过的服务器列表快照。
func (s *Service) cachedServers(ctx context.Context) ([]server.Server, error) {
	now := s.now()
	s.mu.Lock()
	if cached := s.serverCache; cached != nil && now.Sub(cached.loadedAt) < snapshotTTL {
		out := append([]server.Server(nil), cached.servers...)
		s.mu.Unlock()
		return out, nil
	}
	s.mu.Unlock()
	all, err := s.servers.List(ctx)
	if err != nil {
		return nil, err
	}
	snapshotServers := append([]server.Server(nil), all...)
	s.mu.Lock()
	s.serverCache = &serverSnapshot{loadedAt: now, servers: snapshotServers}
	s.mu.Unlock()
	return snapshotServers, nil
}

func (s *Service) loadConfig(ctx context.Context) (Config, error) {
	cfg := defaultConfig()
	var mode, rawIDs, cleanupSchedule, updatedAt string
	err := s.db.QueryRowContext(ctx, `SELECT mode,server_ids_json,retention_days,collection_interval_seconds,cleanup_schedule,version,updated_at FROM metrics_facility_configs WHERE id=?`, configID).
		Scan(&mode, &rawIDs, &cfg.RetentionDays, &cfg.CollectionIntervalSeconds, &cleanupSchedule, &cfg.Version, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return cfg, nil
	}
	if err != nil {
		return Config{}, err
	}
	// 防御性归一化：旧 alpha 行、手工数据或越界值都回落默认，保证下发给 Agent
	// 的采集节奏与清理参数总是合法。
	cfg.Mode = normalizeMode(mode)
	cfg.ServerIDs = decodeIDs(rawIDs)
	if cfg.Mode == ModeAll {
		// all 模式的节点集合由服务器表现算，持久化列表没有意义。
		cfg.ServerIDs = []string{}
	}
	cfg.RetentionDays = normalizeRetentionDays(cfg.RetentionDays)
	cfg.CollectionIntervalSeconds = normalizeIntervalSeconds(cfg.CollectionIntervalSeconds)
	cfg.CleanupSchedule = normalizeCleanupScheduleOr(cleanupSchedule)
	cfg.UpdatedAt = parseTime(updatedAt)
	return cfg, nil
}

type seedValues struct {
	RetentionDays             int
	CollectionIntervalSeconds int
	CleanupSchedule           string
}

// seedValues 从 AppDB runtime_settings 读取旧指标设置，非法/缺失时用默认值。
func (s *Service) seedValues(ctx context.Context) (seedValues, error) {
	seed := seedValues{
		RetentionDays:             DefaultRetentionDays,
		CollectionIntervalSeconds: DefaultCollectionIntervalSeconds,
		CleanupSchedule:           DefaultCleanupSchedule,
	}
	rows, err := s.db.QueryContext(ctx, `SELECT key,value FROM runtime_settings WHERE key IN (?,?,?)`,
		legacyRetentionKey, legacyIntervalKey, legacyCleanupScheduleKey)
	if err != nil {
		return seedValues{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return seedValues{}, err
		}
		switch key {
		case legacyRetentionKey:
			if n, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && n >= 1 {
				seed.RetentionDays = n
			}
		case legacyIntervalKey:
			if n, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && n >= 1 {
				seed.CollectionIntervalSeconds = n
			}
		case legacyCleanupScheduleKey:
			if schedule := normalizeCleanupSchedule(value); schedule != "" {
				seed.CleanupSchedule = schedule
			}
		}
	}
	if err := rows.Err(); err != nil {
		return seedValues{}, err
	}
	return seed, nil
}

func defaultConfig() Config {
	return Config{
		Mode:                      ModeAll,
		ServerIDs:                 []string{},
		RetentionDays:             DefaultRetentionDays,
		CollectionIntervalSeconds: DefaultCollectionIntervalSeconds,
		CleanupSchedule:           DefaultCleanupSchedule,
	}
}

func normalizeMode(mode string) string {
	switch strings.TrimSpace(mode) {
	case ModeSelected:
		return ModeSelected
	default:
		return ModeAll
	}
}

func normalizeRetentionDays(raw int) int {
	if raw < 1 {
		return DefaultRetentionDays
	}
	return raw
}

func normalizeIntervalSeconds(raw int) int {
	if raw < 1 {
		return DefaultCollectionIntervalSeconds
	}
	return raw
}

// normalizeCleanupSchedule 返回合法的清理计划；非法/空返回空串，由调用方决定
// 拒绝（保存）或回落默认（读取/播种）。
func normalizeCleanupSchedule(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case cleanupScheduleHourly:
		return cleanupScheduleHourly
	case cleanupScheduleDaily:
		return cleanupScheduleDaily
	case cleanupScheduleWeekly:
		return cleanupScheduleWeekly
	default:
		return ""
	}
}

func normalizeCleanupScheduleOr(raw string) string {
	if schedule := normalizeCleanupSchedule(raw); schedule != "" {
		return schedule
	}
	return DefaultCleanupSchedule
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

func serverByID(all []server.Server) map[string]server.Server {
	out := make(map[string]server.Server, len(all))
	for _, srv := range all {
		out[srv.ID] = srv
	}
	return out
}

func containsID(ids []string, id string) bool {
	for _, candidate := range ids {
		if candidate == id {
			return true
		}
	}
	return false
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
