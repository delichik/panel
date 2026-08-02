package installation

import (
	"context"
	"database/sql"
	"strings"
	"time"

	panelerr "panel/internal/platform/errors"
)

const singletonID = "default"

type State struct {
	HostServerID    string    `json:"hostServerId"`
	PendingServerID string    `json:"pendingServerId,omitempty"`
	Stage           string    `json:"stage,omitempty"`
	LastError       string    `json:"lastError,omitempty"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

type Service struct {
	db *sql.DB
}

func NewService(db *sql.DB) *Service {
	return &Service{db: db}
}

func (s *Service) Get(ctx context.Context) (State, error) {
	var state State
	var hostServerID, pendingServerID sql.NullString
	var createdAt, updatedAt string
	err := s.db.QueryRowContext(ctx, `SELECT host_server_id,pending_server_id,stage,last_error,created_at,updated_at FROM panel_installation WHERE id=?`, singletonID).
		Scan(&hostServerID, &pendingServerID, &state.Stage, &state.LastError, &createdAt, &updatedAt)
	if err == sql.ErrNoRows {
		return State{}, nil
	}
	if err != nil {
		return State{}, err
	}
	state.HostServerID = hostServerID.String
	state.PendingServerID = pendingServerID.String
	state.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	state.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	return state, nil
}

func (s *Service) HostServerID(ctx context.Context) (string, error) {
	state, err := s.Get(ctx)
	return state.HostServerID, err
}

func (s *Service) SetHostServer(ctx context.Context, serverID string) (State, error) {
	serverID = strings.TrimSpace(serverID)
	if serverID == "" {
		return State{}, panelerr.Validation("panel_host_server_required", "Panel host server is required")
	}
	var exists int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM servers WHERE id=?`, serverID).Scan(&exists); err != nil {
		return State{}, err
	}
	if exists == 0 {
		return State{}, panelerr.NotFound("server")
	}
	current, err := s.Get(ctx)
	if err != nil {
		return State{}, err
	}
	if current.HostServerID != "" && current.HostServerID != serverID {
		return State{}, panelerr.Conflict("panel_host_server_already_set", "Panel host server is already configured")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = s.db.ExecContext(ctx, `INSERT INTO panel_installation(id,host_server_id,pending_server_id,stage,last_error,created_at,updated_at)
		VALUES(?,?,NULL,'complete','',?,?)
		ON CONFLICT(id) DO UPDATE SET host_server_id=excluded.host_server_id,pending_server_id=NULL,stage='complete',last_error='',updated_at=excluded.updated_at`, singletonID, serverID, now, now)
	if err != nil {
		return State{}, err
	}
	return s.Get(ctx)
}

// RegisterHostServer 把 serverID 登记为唯一 Panel 宿主节点；已登记为同一节点时幂等。
func (s *Service) RegisterHostServer(ctx context.Context, serverID string) error {
	_, err := s.SetHostServer(ctx, serverID)
	return err
}

func (s *Service) SetPendingServer(ctx context.Context, serverID, stage string) (State, error) {
	serverID = strings.TrimSpace(serverID)
	if serverID == "" {
		return State{}, panelerr.Validation("panel_host_server_required", "Panel host server is required")
	}
	current, err := s.Get(ctx)
	if err != nil {
		return State{}, err
	}
	if current.HostServerID != "" {
		return current, nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = s.db.ExecContext(ctx, `INSERT INTO panel_installation(id,host_server_id,pending_server_id,stage,last_error,created_at,updated_at)
		VALUES(?,NULL,?,?, '',?,?)
		ON CONFLICT(id) DO UPDATE SET pending_server_id=excluded.pending_server_id,stage=excluded.stage,last_error='',updated_at=excluded.updated_at`, singletonID, serverID, strings.TrimSpace(stage), now, now)
	if err != nil {
		return State{}, err
	}
	return s.Get(ctx)
}

func (s *Service) RecordFailure(ctx context.Context, stage string, cause error) error {
	message := ""
	if cause != nil {
		message = cause.Error()
	}
	_, err := s.db.ExecContext(ctx, `UPDATE panel_installation SET stage=?,last_error=?,updated_at=? WHERE id=?`, strings.TrimSpace(stage), message, time.Now().UTC().Format(time.RFC3339Nano), singletonID)
	return err
}

func (s *Service) IsHostServer(ctx context.Context, serverID string) (bool, error) {
	hostServerID, err := s.HostServerID(ctx)
	return hostServerID != "" && hostServerID == strings.TrimSpace(serverID), err
}
