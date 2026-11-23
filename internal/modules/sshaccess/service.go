package sshaccess

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"time"

	agentcontract "panel/internal/agent/contract"
	server "panel/internal/modules/servers"
	panelerr "panel/internal/platform/errors"
	"panel/internal/platform/logging"

	"go.uber.org/zap"
)

const (
	// DefaultSuccessRetentionDays 是成功会话的默认保留天数。
	DefaultSuccessRetentionDays = 30
	// DefaultFailureRetentionDays 是失败尝试的默认保留天数。
	DefaultFailureRetentionDays = 3
	// maxAttemptRowsPerServer 限制单服务器失败尝试行数，避免暴力破解撑爆安全库。
	maxAttemptRowsPerServer = 200000
	// coverageLiveWindow 内收到过 live 快照才认为数据是实时的。
	coverageLiveWindow = 90 * time.Second
	// cleanupInterval 是保留期清理与孤儿清扫周期。
	cleanupInterval = time.Hour
)

// serverProvider 是服务依赖的服务器目录。
type serverProvider interface {
	Get(ctx context.Context, serverID string) (server.Server, error)
	List(ctx context.Context) ([]server.Server, error)
}

// retentionSource 提供用户在设置页配置的保留期。
type retentionSource interface {
	SSHAccessRetention() (successDays, failureDays int)
}

// Coverage 描述某台服务器 SSH 数据的实时性。
type Coverage struct {
	State      string     `json:"state"`
	LastLiveAt *time.Time `json:"lastLiveAt,omitempty"`
}

// SessionList 是会话列表响应。
type SessionList struct {
	Items    []Session `json:"items"`
	Total    int       `json:"total"`
	Page     int       `json:"page"`
	PageSize int       `json:"pageSize"`
	Coverage Coverage  `json:"coverage"`
}

// AttemptList 是失败尝试列表响应。
type AttemptList struct {
	Items    []Attempt `json:"items"`
	Total    int       `json:"total"`
	Page     int       `json:"page"`
	PageSize int       `json:"pageSize"`
	Coverage Coverage  `json:"coverage"`
}

// Service 负责把 Agent 上报写入安全库，并提供查询与保留期清理。
type Service struct {
	repo      *Repository
	servers   serverProvider
	retention retentionSource
	now       func() time.Time

	mu       sync.Mutex
	coverage map[string]time.Time
}

// NewService 创建 SSH 访问记录服务。
func NewService(db *sql.DB, servers serverProvider, retention retentionSource) *Service {
	return &Service{
		repo:      NewRepository(db),
		servers:   servers,
		retention: retention,
		now:       func() time.Time { return time.Now().UTC() },
		coverage:  map[string]time.Time{},
	}
}

// ApplyLive 写入当前在线快照，并把本服务器已消失的开放会话标记为关闭。
func (s *Service) ApplyLive(ctx context.Context, serverID string, snapshot agentcontract.SSHSnapshot) error {
	observedAt := snapshot.SampledAt
	if observedAt.IsZero() {
		observedAt = s.now()
	}
	ids, err := s.repo.UpsertSessions(ctx, serverID, snapshot.Items, observedAt, "live")
	if err != nil {
		return err
	}
	if _, err := s.repo.CloseMissingSessions(ctx, serverID, ids, observedAt); err != nil {
		return err
	}
	s.mu.Lock()
	s.coverage[serverID] = s.now()
	s.mu.Unlock()
	return nil
}

// ApplyHistory 用 wtmp 回填补齐登出时间。
func (s *Service) ApplyHistory(ctx context.Context, serverID string, history agentcontract.SSHSessionHistory) error {
	return s.repo.UpsertHistory(ctx, serverID, history.Items)
}

// ApplyFailures 写入 btmp 失败尝试。
func (s *Service) ApplyFailures(ctx context.Context, serverID string, failures agentcontract.SSHFailures) error {
	_, err := s.repo.InsertAttempts(ctx, serverID, failures.Items)
	return err
}

// BackfillSince 返回增量回填游标：最近登录时间减一小时；首次回填最近 7 天。
func (s *Service) BackfillSince(ctx context.Context, serverID string) (time.Time, error) {
	latest, err := s.repo.LatestSessionSince(ctx, serverID)
	if err != nil {
		return time.Time{}, err
	}
	if latest.IsZero() {
		return s.now().Add(-7 * 24 * time.Hour), nil
	}
	return latest.Add(-time.Hour), nil
}

// ListSessions 返回分页会话列表。
func (s *Service) ListSessions(ctx context.Context, serverID string, filter SessionFilter) (SessionList, error) {
	if _, err := s.servers.Get(ctx, serverID); err != nil {
		return SessionList{}, err
	}
	filter, err := normalizeSessionFilter(filter)
	if err != nil {
		return SessionList{}, err
	}
	page, err := s.repo.ListSessions(ctx, serverID, filter)
	if err != nil {
		return SessionList{}, err
	}
	return SessionList{
		Items: page.Items, Total: page.Total, Page: filter.Page, PageSize: filter.PageSize,
		Coverage: s.coverageFor(serverID),
	}, nil
}

// ListAttempts 返回分页失败尝试列表。
func (s *Service) ListAttempts(ctx context.Context, serverID string, filter AttemptFilter) (AttemptList, error) {
	if _, err := s.servers.Get(ctx, serverID); err != nil {
		return AttemptList{}, err
	}
	filter, err := normalizeAttemptFilter(filter)
	if err != nil {
		return AttemptList{}, err
	}
	page, err := s.repo.ListAttempts(ctx, serverID, filter)
	if err != nil {
		return AttemptList{}, err
	}
	return AttemptList{
		Items: page.Items, Total: page.Total, Page: filter.Page, PageSize: filter.PageSize,
		Coverage: s.coverageFor(serverID),
	}, nil
}

// SSHAccessRetention 返回设置页配置的保留期（天数）。
func (s *Service) SSHAccessRetention() (int, int) {
	if s.retention == nil {
		return DefaultSuccessRetentionDays, DefaultFailureRetentionDays
	}
	return s.retention.SSHAccessRetention()
}

// Start 启动每小时清理（保留期 + 每服务器行数上限 + 孤儿记录）。
func (s *Service) Start(ctx context.Context) {
	go func() {
		s.cleanup(ctx)
		ticker := time.NewTicker(cleanupInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.cleanup(ctx)
			}
		}
	}()
}

func (s *Service) cleanup(ctx context.Context) {
	successDays, failureDays := s.SSHAccessRetention()
	if _, _, err := s.repo.Cleanup(ctx, successDays, failureDays, maxAttemptRowsPerServer); err != nil {
		logging.From(ctx).Warn("ssh access cleanup failed", zap.Error(err))
	}
	servers, err := s.servers.List(ctx)
	if err != nil {
		logging.From(ctx).Warn("ssh access orphan sweep skipped", zap.Error(err))
		return
	}
	ids := make([]string, 0, len(servers))
	for _, item := range servers {
		ids = append(ids, item.ID)
	}
	if _, err := s.repo.SweepOrphans(ctx, ids); err != nil {
		logging.From(ctx).Warn("ssh access orphan sweep failed", zap.Error(err))
	}
}

func (s *Service) coverageFor(serverID string) Coverage {
	s.mu.Lock()
	last, ok := s.coverage[serverID]
	s.mu.Unlock()
	if !ok {
		return Coverage{State: "unknown"}
	}
	value := last
	state := "stale"
	if s.now().Sub(last) <= coverageLiveWindow {
		state = "live"
	}
	return Coverage{State: state, LastLiveAt: &value}
}

func normalizeSessionFilter(f SessionFilter) (SessionFilter, error) {
	switch strings.TrimSpace(f.State) {
	case "":
		f.State = "all"
	case "open", "closed", "all":
	case "online":
		f.State = "open"
	case "history":
		f.State = "closed"
	default:
		return f, panelerr.Validation("ssh_access_state_invalid", "state must be open, closed or all")
	}
	page, pageSize, err := normalizePage(f.Page, f.PageSize)
	if err != nil {
		return f, err
	}
	f.Page, f.PageSize = page, pageSize
	return f, nil
}

func normalizeAttemptFilter(f AttemptFilter) (AttemptFilter, error) {
	page, pageSize, err := normalizePage(f.Page, f.PageSize)
	if err != nil {
		return f, err
	}
	f.Page, f.PageSize = page, pageSize
	return f, nil
}

func normalizePage(page, pageSize int) (int, int, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 50
	}
	if pageSize > 200 {
		return page, pageSize, panelerr.Validation("ssh_access_page_invalid", "pageSize must be between 1 and 200")
	}
	return page, pageSize, nil
}
