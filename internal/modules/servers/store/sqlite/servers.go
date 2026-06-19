package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"panel/internal/modules/servers/domain"
	panelerr "panel/internal/platform/errors"
)

type ServerRepository struct {
	db *sql.DB
}

func NewServerRepository(db *sql.DB) *ServerRepository {
	return &ServerRepository{db: db}
}

func (r *ServerRepository) List(ctx context.Context) ([]domain.Server, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,name,host,port,ssh_username,credential_id,docker_host,traits,variables_json,notes,os_id,os_version_id,os_pretty_name,os_supported,architecture_os,architecture_arch,architecture_machine,reachable,sudo_passwordless,sudo_last_checked_at,privilege_mode,privilege_last_checked_at,last_checked_at,last_error,created_at,updated_at FROM servers ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	out := []domain.Server{}
	for rows.Next() {
		srv, err := scanServer(rows)
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		out = append(out, srv)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *ServerRepository) Get(ctx context.Context, serverID string) (domain.Server, error) {
	srv, err := scanServer(r.db.QueryRowContext(ctx, `SELECT id,name,host,port,ssh_username,credential_id,docker_host,traits,variables_json,notes,os_id,os_version_id,os_pretty_name,os_supported,architecture_os,architecture_arch,architecture_machine,reachable,sudo_passwordless,sudo_last_checked_at,privilege_mode,privilege_last_checked_at,last_checked_at,last_error,created_at,updated_at FROM servers WHERE id=?`, serverID))
	if err == sql.ErrNoRows {
		return domain.Server{}, panelerr.NotFound("server")
	}
	return srv, err
}

func (r *ServerRepository) ListIDs(ctx context.Context) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id FROM servers ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (r *ServerRepository) Insert(ctx context.Context, srv domain.Server) error {
	traits, err := json.Marshal(srv.Traits)
	if err != nil {
		return err
	}
	variables, err := json.Marshal(srv.Variables)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `INSERT INTO servers(id,name,host,port,ssh_username,credential_id,docker_host,traits,variables_json,notes,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		srv.ID, srv.Name, srv.Host, srv.Port, srv.SSHUsername, srv.CredentialID, srv.DockerHost, string(traits), string(variables), srv.Notes,
		srv.CreatedAt.UTC().Format(time.RFC3339Nano), srv.UpdatedAt.UTC().Format(time.RFC3339Nano))
	return err
}

func (r *ServerRepository) Update(ctx context.Context, srv domain.Server) error {
	traits, err := json.Marshal(srv.Traits)
	if err != nil {
		return err
	}
	variables, err := json.Marshal(srv.Variables)
	if err != nil {
		return err
	}
	result, err := r.db.ExecContext(ctx, `UPDATE servers SET name=?,host=?,port=?,ssh_username=?,credential_id=?,docker_host=?,traits=?,variables_json=?,notes=?,updated_at=? WHERE id=?`,
		srv.Name, srv.Host, srv.Port, srv.SSHUsername, srv.CredentialID, srv.DockerHost, string(traits), string(variables), srv.Notes,
		srv.UpdatedAt.UTC().Format(time.RFC3339Nano), srv.ID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return panelerr.NotFound("server")
	}
	return nil
}

func (r *ServerRepository) Delete(ctx context.Context, serverID string) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM servers WHERE id=?`, serverID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return panelerr.NotFound("server")
	}
	return nil
}

type serverScanner interface{ Scan(dest ...any) error }

func scanServer(row serverScanner) (domain.Server, error) {
	var srv domain.Server
	var traits, variables, created, updated string
	var osSupported, reachable, sudo int
	var sudoAt, privilegeAt, checkedAt sql.NullString
	err := row.Scan(&srv.ID, &srv.Name, &srv.Host, &srv.Port, &srv.SSHUsername, &srv.CredentialID, &srv.DockerHost, &traits, &variables, &srv.Notes, &srv.OS.ID, &srv.OS.VersionID, &srv.OS.PrettyName, &osSupported, &srv.Architecture.OS, &srv.Architecture.Arch, &srv.Architecture.RawMachine, &reachable, &sudo, &sudoAt, &srv.Privilege.Mode, &privilegeAt, &checkedAt, &srv.LastError, &created, &updated)
	if err != nil {
		return domain.Server{}, err
	}
	srv.Traits = map[string]string{}
	_ = json.Unmarshal([]byte(traits), &srv.Traits)
	if srv.Architecture.RawMachine == "" {
		srv.Architecture.RawMachine = srv.Traits["sys.architecture"]
	}
	srv.Variables = map[string]string{}
	_ = json.Unmarshal([]byte(variables), &srv.Variables)
	srv.OS.Supported = osSupported == 1
	srv.Reachable = reachable == 1
	srv.Sudo.Passwordless = sudo == 1
	if srv.Privilege.Mode == "" {
		if srv.Sudo.Passwordless {
			srv.Privilege.Mode = "passwordless_sudo"
		} else {
			srv.Privilege.Mode = "none"
		}
	}
	srv.Privilege.Privileged = srv.Privilege.Mode == "root" || srv.Privilege.Mode == "passwordless_sudo"
	if sudoAt.Valid {
		v, _ := time.Parse(time.RFC3339Nano, sudoAt.String)
		srv.Sudo.LastCheckedAt = &v
	}
	if privilegeAt.Valid {
		v, _ := time.Parse(time.RFC3339Nano, privilegeAt.String)
		srv.Privilege.LastCheckedAt = &v
	}
	if checkedAt.Valid {
		v, _ := time.Parse(time.RFC3339Nano, checkedAt.String)
		srv.LastCheckedAt = &v
	}
	srv.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	srv.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return srv, nil
}
