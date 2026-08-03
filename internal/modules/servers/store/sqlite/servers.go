package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"panel/internal/modules/servers/domain"
	"panel/internal/platform/database/models"
	"panel/internal/platform/database/orm"
	panelerr "panel/internal/platform/errors"
	httpx "panel/internal/platform/http"
	"panel/internal/platform/linux"
)

type ServerRepository struct {
	db *sql.DB
}

func NewServerRepository(db *sql.DB) *ServerRepository {
	return &ServerRepository{db: db}
}

func (r *ServerRepository) List(ctx context.Context) ([]domain.Server, error) {
	var rows []models.Server
	if err := orm.New(r.db).From("servers").OrderBy("created_at DESC").All(ctx, &rows); err != nil {
		return nil, err
	}
	out := make([]domain.Server, 0, len(rows))
	for i := range rows {
		out = append(out, toDomainServer(rows[i]))
	}
	return out, nil
}

func (r *ServerRepository) ListSummaries(ctx context.Context) ([]domain.ServerSummary, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,name,host,port,reachable,sudo_passwordless,privilege_mode,last_checked_at,last_error,updated_at,
		COALESCE(json_extract(traits,'$."agent.enabled"'),''),COALESCE(json_extract(traits,'$."agent.status"'),''),
		COALESCE(json_extract(traits,'$."sys.ufw_supported"'),''),COALESCE(json_extract(traits,'$."sys.ufw_installed"'),'')
		FROM servers ORDER BY created_at DESC,id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.ServerSummary{}
	for rows.Next() {
		var item domain.ServerSummary
		var reachable, sudo int
		var lastChecked sql.NullString
		var updatedAt, agentEnabled, agentStatus, ufwSupported, ufwInstalled string
		if err := rows.Scan(&item.ID, &item.Name, &item.Host, &item.Port, &reachable, &sudo, &item.Privilege.Mode, &lastChecked, &item.LastError, &updatedAt, &agentEnabled, &agentStatus, &ufwSupported, &ufwInstalled); err != nil {
			return nil, err
		}
		item.Reachable = reachable == 1
		item.Sudo.Passwordless = sudo == 1
		item.Privilege.Privileged = item.Privilege.Mode == "root" || item.Privilege.Mode == "passwordless_sudo"
		item.Traits = map[string]string{"agent.enabled": agentEnabled, "agent.status": agentStatus, "sys.ufw_supported": ufwSupported, "sys.ufw_installed": ufwInstalled}
		if lastChecked.Valid {
			parsed, _ := time.Parse(time.RFC3339Nano, lastChecked.String)
			if !parsed.IsZero() {
				item.LastCheckedAt = &parsed
			}
		}
		item.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *ServerRepository) ListSummaryPage(ctx context.Context, page, pageSize int, query string) (httpx.ListPage[domain.ServerSummary], error) {
	filter := "1=1"
	args := []any{}
	if query != "" {
		filter = "(name LIKE ? ESCAPE '\\' OR host LIKE ? ESCAPE '\\')"
		term := "%" + strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(query) + "%"
		args = append(args, term, term)
	}
	var total int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM servers WHERE `+filter, args...).Scan(&total); err != nil {
		return httpx.ListPage[domain.ServerSummary]{}, err
	}
	listArgs := append(append([]any{}, args...), pageSize, (page-1)*pageSize)
	rows, err := r.db.QueryContext(ctx, `SELECT id,name,host,port,reachable,sudo_passwordless,privilege_mode,last_checked_at,last_error,updated_at,
		COALESCE(json_extract(traits,'$."agent.enabled"'),''),COALESCE(json_extract(traits,'$."agent.status"'),''),
		COALESCE(json_extract(traits,'$."sys.ufw_supported"'),''),COALESCE(json_extract(traits,'$."sys.ufw_installed"'),'')
		FROM servers WHERE `+filter+` ORDER BY created_at DESC,id ASC LIMIT ? OFFSET ?`, listArgs...)
	if err != nil {
		return httpx.ListPage[domain.ServerSummary]{}, err
	}
	defer rows.Close()
	items := []domain.ServerSummary{}
	for rows.Next() {
		var item domain.ServerSummary
		var reachable, sudo int
		var lastChecked sql.NullString
		var updatedAt, agentEnabled, agentStatus, ufwSupported, ufwInstalled string
		if err := rows.Scan(&item.ID, &item.Name, &item.Host, &item.Port, &reachable, &sudo, &item.Privilege.Mode, &lastChecked, &item.LastError, &updatedAt, &agentEnabled, &agentStatus, &ufwSupported, &ufwInstalled); err != nil {
			return httpx.ListPage[domain.ServerSummary]{}, err
		}
		item.Reachable, item.Sudo.Passwordless = reachable == 1, sudo == 1
		item.Privilege.Privileged = item.Privilege.Mode == "root" || item.Privilege.Mode == "passwordless_sudo"
		item.Traits = map[string]string{"agent.enabled": agentEnabled, "agent.status": agentStatus, "sys.ufw_supported": ufwSupported, "sys.ufw_installed": ufwInstalled}
		if lastChecked.Valid {
			parsed, _ := time.Parse(time.RFC3339Nano, lastChecked.String)
			if !parsed.IsZero() {
				item.LastCheckedAt = &parsed
			}
		}
		item.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return httpx.ListPage[domain.ServerSummary]{}, err
	}
	return httpx.ListPage[domain.ServerSummary]{Items: items, Total: total, Page: page, PageSize: pageSize}, nil
}

func (r *ServerRepository) Get(ctx context.Context, serverID string) (domain.Server, error) {
	var row models.Server
	err := orm.New(r.db).From("servers").Where("id=?", serverID).First(ctx, &row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Server{}, panelerr.NotFound("server")
	}
	if err != nil {
		return domain.Server{}, err
	}
	return toDomainServer(row), nil
}

func (r *ServerRepository) ListIDs(ctx context.Context) ([]string, error) {
	var ids []string
	if err := orm.New(r.db).From("servers").OrderBy("created_at DESC").Pluck(ctx, "id", &ids); err != nil {
		return nil, err
	}
	return ids, nil
}

func (r *ServerRepository) Insert(ctx context.Context, srv domain.Server) error {
	return orm.New(r.db).Insert(ctx, fromDomainServer(srv))
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
	result, err := orm.RawExec(ctx, r.db, `UPDATE servers SET name=?,host=?,ipv4=?,ipv6=?,port=?,ssh_username=?,credential_id=?,docker_host=?,traits=?,variables_json=?,notes=?,updated_at=? WHERE id=?`,
		srv.Name, srv.Host, srv.IPv4, srv.IPv6, srv.Port, srv.SSHUsername, srv.CredentialID, srv.DockerHost, string(traits), string(variables), srv.Notes,
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
	result, err := orm.RawExec(ctx, r.db, `DELETE FROM servers WHERE id=?`, serverID)
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

// toDomainServer 将 models.Server 中间载体映射为 domain.Server，保持原
// scanServer 的默认值与归一化语义（空 privilege_mode -> none 等）。
func toDomainServer(m models.Server) domain.Server {
	srv := domain.Server{
		ID:            m.ID,
		Name:          m.Name,
		Host:          m.Host,
		IPv4:          m.IPv4,
		IPv6:          m.IPv6,
		Port:          m.Port,
		SSHUsername:   m.SSHUsername,
		CredentialID:  m.CredentialID,
		DockerHost:    m.DockerHost,
		Traits:        stringMap(m.Traits),
		Variables:     stringMap(m.VariablesJSON),
		Notes:         m.Notes,
		OS:            linux.OSRelease{ID: m.OSID, VersionID: m.OSVersionID, PrettyName: m.OSPrettyName, Supported: m.OSSupported},
		Architecture:  domain.ArchitectureInfo{OS: m.ArchitectureOS, Arch: m.ArchitectureArch, RawMachine: m.ArchitectureMachine},
		Sudo:          domain.SudoState{Passwordless: m.SudoPasswordless, LastCheckedAt: m.SudoLastCheckedAt},
		Reachable:     m.Reachable,
		LastCheckedAt: m.LastCheckedAt,
		LastError:     m.LastError,
		CreatedAt:     m.CreatedAt,
		UpdatedAt:     m.UpdatedAt,
	}
	srv.Privilege = domain.PrivilegeState{Mode: m.PrivilegeMode, LastCheckedAt: m.PrivilegeLastCheckedAt}
	if srv.Privilege.Mode == "" {
		if m.SudoPasswordless {
			srv.Privilege.Mode = "passwordless_sudo"
		} else {
			srv.Privilege.Mode = "none"
		}
	}
	srv.Privilege.Privileged = srv.Privilege.Mode == "root" || srv.Privilege.Mode == "passwordless_sudo"
	return srv
}

func fromDomainServer(srv domain.Server) *models.Server {
	return &models.Server{
		ID:                     srv.ID,
		Name:                   srv.Name,
		Host:                   srv.Host,
		IPv4:                   srv.IPv4,
		IPv6:                   srv.IPv6,
		Port:                   srv.Port,
		SSHUsername:            srv.SSHUsername,
		CredentialID:           srv.CredentialID,
		DockerHost:             srv.DockerHost,
		Traits:                 anyMap(srv.Traits),
		VariablesJSON:          anyMap(srv.Variables),
		Notes:                  srv.Notes,
		OSID:                   srv.OS.ID,
		OSVersionID:            srv.OS.VersionID,
		OSPrettyName:           srv.OS.PrettyName,
		OSSupported:            srv.OS.Supported,
		ArchitectureOS:         srv.Architecture.OS,
		ArchitectureArch:       srv.Architecture.Arch,
		ArchitectureMachine:    srv.Architecture.RawMachine,
		Reachable:              srv.Reachable,
		SudoPasswordless:       srv.Sudo.Passwordless,
		SudoLastCheckedAt:      srv.Sudo.LastCheckedAt,
		PrivilegeMode:          srv.Privilege.Mode,
		PrivilegeLastCheckedAt: srv.Privilege.LastCheckedAt,
		LastCheckedAt:          srv.LastCheckedAt,
		LastError:              srv.LastError,
		CreatedAt:              srv.CreatedAt,
		UpdatedAt:              srv.UpdatedAt,
	}
}

// stringMap 保持原 scanServer 的 JSON 容错语义：非法/非字符串值视为空，
// "null" 归一化为 nil。
func stringMap(m map[string]any) map[string]string {
	out := map[string]string{}
	raw, err := json.Marshal(m)
	if err != nil {
		return out
	}
	_ = json.Unmarshal(raw, &out)
	return out
}

func anyMap(m map[string]string) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
