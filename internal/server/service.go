package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"panel/internal/id"
	"panel/internal/linux"
	"panel/internal/panelerr"
	"panel/internal/sshx"
	"panel/internal/tasks"
)

const connectivitySudoTimeout = 8 * time.Second

type Service struct {
	db      *sql.DB
	exec    sshx.RemoteExecutor
	tasks   *tasks.Service
	adapter linux.DebianAdapter
}

func NewService(db *sql.DB, exec sshx.RemoteExecutor, taskSvc *tasks.Service) *Service {
	return &Service{db: db, exec: exec, tasks: taskSvc, adapter: linux.DebianAdapter{}}
}

func (s *Service) Create(ctx context.Context, req SaveRequest) (Server, error) {
	if err := validateSave(req); err != nil {
		return Server{}, err
	}
	now := time.Now().UTC()
	srv := Server{ID: id.New("srv"), Name: req.Name, Host: req.Host, Port: req.Port, SSHUsername: req.SSHUsername, CredentialID: req.CredentialID, Labels: req.Labels, Notes: req.Notes, CreatedAt: now, UpdatedAt: now}
	labels, _ := json.Marshal(srv.Labels)
	_, err := s.db.ExecContext(ctx, `INSERT INTO servers(id,name,host,port,ssh_username,credential_id,labels,notes,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		srv.ID, srv.Name, srv.Host, srv.Port, srv.SSHUsername, srv.CredentialID, string(labels), srv.Notes, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	return srv, err
}

func (s *Service) Update(ctx context.Context, serverID string, req SaveRequest) (Server, error) {
	if err := validateSave(req); err != nil {
		return Server{}, err
	}
	labels, _ := json.Marshal(req.Labels)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.ExecContext(ctx, `UPDATE servers SET name=?,host=?,port=?,ssh_username=?,credential_id=?,labels=?,notes=?,updated_at=? WHERE id=?`,
		req.Name, req.Host, req.Port, req.SSHUsername, req.CredentialID, string(labels), req.Notes, now, serverID)
	if err != nil {
		return Server{}, err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return Server{}, panelerr.NotFound("server")
	}
	return s.Get(ctx, serverID)
}

func (s *Service) Delete(ctx context.Context, serverID string) error {
	var running int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks WHERE server_id=? AND status IN ('queued','running')`, serverID).Scan(&running); err != nil {
		return err
	}
	if running > 0 {
		return panelerr.Conflict("server_has_running_tasks", "Server has running tasks")
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM servers WHERE id=?`, serverID)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return panelerr.NotFound("server")
	}
	return nil
}

func (s *Service) List(ctx context.Context) ([]Server, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,host,port,ssh_username,credential_id,labels,notes,os_id,os_version_id,os_pretty_name,os_supported,reachable,sudo_passwordless,sudo_last_checked_at,last_checked_at,last_error,created_at,updated_at FROM servers ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Server
	for rows.Next() {
		srv, err := scanServer(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, srv)
	}
	return out, rows.Err()
}

func (s *Service) Get(ctx context.Context, serverID string) (Server, error) {
	srv, err := scanServer(s.db.QueryRowContext(ctx, `SELECT id,name,host,port,ssh_username,credential_id,labels,notes,os_id,os_version_id,os_pretty_name,os_supported,reachable,sudo_passwordless,sudo_last_checked_at,last_checked_at,last_error,created_at,updated_at FROM servers WHERE id=?`, serverID))
	if err == sql.ErrNoRows {
		return Server{}, panelerr.NotFound("server")
	}
	return srv, err
}

func (s *Service) TestConnectivity(ctx context.Context, serverID string) (tasks.Task, error) {
	srv, err := s.Get(ctx, serverID)
	if err != nil {
		return tasks.Task{}, err
	}
	task, err := s.tasks.Create(ctx, tasks.CreateInput{Type: "server_connectivity_test", ServerID: serverID, Summary: "Testing SSH connectivity"})
	if err != nil {
		return tasks.Task{}, err
	}
	go func() {
		taskCtx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		s.runConnectivityTest(taskCtx, task.ID, srv)
	}()
	return task, nil
}

func (s *Service) runConnectivityTest(ctx context.Context, taskID string, srv Server) {
	_ = s.tasks.Start(ctx, taskID)
	target := srv.Target()
	_ = s.tasks.Advance(ctx, taskID, "connecting", "connecting to server")
	osInfo, err := linux.Detect(ctx, s.exec, target)
	if err != nil {
		_ = s.markCheck(ctx, srv.ID, false, linux.OSRelease{}, false, err.Error())
		_ = s.tasks.Fail(ctx, taskID, err)
		return
	}
	_ = s.tasks.Advance(ctx, taskID, "verifying", "checking passwordless sudo")
	sudoRes, sudoErr := s.exec.ExecSudo(ctx, target, sshx.CommandSpec{Command: "true", Timeout: connectivitySudoTimeout})
	passwordless := sudoErr == nil && sudoRes.ExitCode == 0
	if sudoErr != nil {
		_ = s.tasks.AppendLog(ctx, taskID, "system", "passwordless sudo unavailable: "+sudoErr.Error())
	}
	if !osInfo.Supported {
		_ = s.markCheck(ctx, srv.ID, true, osInfo, passwordless, "unsupported distribution")
		_ = s.tasks.Complete(ctx, taskID, "Connected, but distribution is unsupported")
		return
	}
	_ = s.markCheck(ctx, srv.ID, true, osInfo, passwordless, "")
	if passwordless {
		_ = s.tasks.Complete(ctx, taskID, "Connectivity test passed")
	} else {
		_ = s.tasks.Complete(ctx, taskID, "Connected, passwordless sudo unavailable")
	}
}

func (s *Service) markCheck(ctx context.Context, serverID string, reachable bool, osInfo linux.OSRelease, sudo bool, msg string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `UPDATE servers SET reachable=?,os_id=?,os_version_id=?,os_pretty_name=?,os_supported=?,sudo_passwordless=?,sudo_last_checked_at=?,last_checked_at=?,last_error=?,updated_at=? WHERE id=?`,
		boolInt(reachable), osInfo.ID, osInfo.VersionID, osInfo.PrettyName, boolInt(osInfo.Supported), boolInt(sudo), now, now, msg, now, serverID)
	return err
}

func (srv Server) Target() sshx.Target {
	return sshx.Target{ServerID: srv.ID, Host: srv.Host, Port: srv.Port, Username: srv.SSHUsername, CredentialID: srv.CredentialID}
}

func validateSave(req SaveRequest) error {
	if strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.Host) == "" || strings.TrimSpace(req.CredentialID) == "" {
		return panelerr.Validation("server_invalid", "Server name, host, and credentialId are required")
	}
	if req.Port <= 0 || req.Port > 65535 {
		return panelerr.Validation("server_port_invalid", "Server port must be between 1 and 65535")
	}
	return nil
}

type serverScanner interface{ Scan(dest ...any) error }

func scanServer(row serverScanner) (Server, error) {
	var srv Server
	var labels, created, updated string
	var osSupported, reachable, sudo int
	var sudoAt, checkedAt sql.NullString
	err := row.Scan(&srv.ID, &srv.Name, &srv.Host, &srv.Port, &srv.SSHUsername, &srv.CredentialID, &labels, &srv.Notes, &srv.OS.ID, &srv.OS.VersionID, &srv.OS.PrettyName, &osSupported, &reachable, &sudo, &sudoAt, &checkedAt, &srv.LastError, &created, &updated)
	if err != nil {
		return Server{}, err
	}
	_ = json.Unmarshal([]byte(labels), &srv.Labels)
	srv.OS.Supported = osSupported == 1
	srv.Reachable = reachable == 1
	srv.Sudo.Passwordless = sudo == 1
	if sudoAt.Valid {
		v, _ := time.Parse(time.RFC3339Nano, sudoAt.String)
		srv.Sudo.LastCheckedAt = &v
	}
	if checkedAt.Valid {
		v, _ := time.Parse(time.RFC3339Nano, checkedAt.String)
		srv.LastCheckedAt = &v
	}
	srv.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	srv.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return srv, nil
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
