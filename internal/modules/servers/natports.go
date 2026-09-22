package server

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"time"

	"panel/internal/modules/servers/domain"
	"panel/internal/platform/database/models"
	"panel/internal/platform/database/orm"
	panelerr "panel/internal/platform/errors"
	id "panel/internal/platform/identity"
)

// NatPortConfig returns the NAT port configuration for a server: the managed
// external-port mappings plus the read-only reminder of ports that must be
// opened manually on the NAT provider side. It is meaningful for NAT servers;
// non-NAT servers simply return their (empty) mapping list.
func (s *Service) NatPortConfig(ctx context.Context, serverID string) (domain.NatPortConfig, error) {
	srv, err := s.Get(ctx, serverID)
	if err != nil {
		return domain.NatPortConfig{}, err
	}
	mappings, err := s.listNatPortMappings(ctx, serverID)
	if err != nil {
		return domain.NatPortConfig{}, err
	}
	cfg := domain.NatPortConfig{
		ServerID:   srv.ID,
		ServerHost: srv.Host,
		Kind:       srv.Kind,
		Mappings:   mappings,
		NeedOpen:   []domain.NatPortNeedOpen{},
	}
	if srv.Kind != ServerKindNAT {
		return cfg, nil
	}
	// The panel itself only reaches this server through SSH and the agent, so
	// both control ports must already be reachable on the NAT provider. The
	// agent's reachable port honors the configured NAT external port.
	cfg.NeedOpen = append(cfg.NeedOpen,
		domain.NatPortNeedOpen{Kind: "ssh", Port: srv.Port, Label: "SSH"},
		domain.NatPortNeedOpen{Kind: "agent", Port: agentPublicPort(srv), Label: "Agent"},
	)
	for _, m := range mappings {
		cfg.NeedOpen = append(cfg.NeedOpen, domain.NatPortNeedOpen{
			Kind:   "app",
			Port:   m.PublicPort,
			Label:  natPortLabel(m.AppName, m.Label, m.HostPort),
			Target: natTarget(srv.Host, m.PublicPort),
		})
	}
	return cfg, nil
}

// AddNatPort creates a new external-port mapping for a NAT server.
func (s *Service) AddNatPort(ctx context.Context, serverID string, req domain.NatPortMappingSave) (domain.NatPortMapping, error) {
	if err := s.ensureNatServer(ctx, serverID); err != nil {
		return domain.NatPortMapping{}, err
	}
	req = normalizeNatPortSave(req)
	if err := validateNatPortSave(req); err != nil {
		return domain.NatPortMapping{}, err
	}
	if err := s.ensureNatPortUnique(ctx, serverID, req.HostPort, req.PublicPort, ""); err != nil {
		return domain.NatPortMapping{}, err
	}
	now := time.Now().UTC()
	row := models.NatPortMapping{
		ID:         id.New("natm"),
		ServerID:   serverID,
		AppID:      strings.TrimSpace(req.AppID),
		HostPort:   req.HostPort,
		PublicPort: req.PublicPort,
		Protocol:   req.Protocol,
		Label:      strings.TrimSpace(req.Label),
		Notes:      strings.TrimSpace(req.Notes),
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := orm.New(s.db).Insert(ctx, &row); err != nil {
		return domain.NatPortMapping{}, err
	}
	return s.natPortMappingDTO(row), nil
}

// UpdateNatPort updates an existing external-port mapping on a NAT server.
func (s *Service) UpdateNatPort(ctx context.Context, serverID, mappingID string, req domain.NatPortMappingSave) (domain.NatPortMapping, error) {
	if err := s.ensureNatServer(ctx, serverID); err != nil {
		return domain.NatPortMapping{}, err
	}
	req = normalizeNatPortSave(req)
	if err := validateNatPortSave(req); err != nil {
		return domain.NatPortMapping{}, err
	}
	var row models.NatPortMapping
	if err := orm.New(s.db).From("nat_port_mappings").Where("id=? AND server_id=?", mappingID, serverID).First(ctx, &row); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.NatPortMapping{}, panelerr.NotFound("nat_port_mapping")
		}
		return domain.NatPortMapping{}, err
	}
	if err := s.ensureNatPortUnique(ctx, serverID, req.HostPort, req.PublicPort, mappingID); err != nil {
		return domain.NatPortMapping{}, err
	}
	row.AppID = strings.TrimSpace(req.AppID)
	row.HostPort = req.HostPort
	row.PublicPort = req.PublicPort
	row.Protocol = req.Protocol
	row.Label = strings.TrimSpace(req.Label)
	row.Notes = strings.TrimSpace(req.Notes)
	row.UpdatedAt = time.Now().UTC()
	if err := orm.New(s.db).Update(ctx, &row); err != nil {
		return domain.NatPortMapping{}, err
	}
	return s.natPortMappingDTO(row), nil
}

// DeleteNatPort removes an external-port mapping from a NAT server.
func (s *Service) DeleteNatPort(ctx context.Context, serverID, mappingID string) error {
	if err := s.ensureNatServer(ctx, serverID); err != nil {
		return err
	}
	res, err := orm.RawExec(ctx, s.db, `DELETE FROM nat_port_mappings WHERE id=? AND server_id=?`, mappingID, serverID)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return panelerr.NotFound("nat_port_mapping")
	}
	return nil
}

// ensureNatServer verifies the server exists and is NAT-type. Only NAT servers
// carry external-port mappings.
func (s *Service) ensureNatServer(ctx context.Context, serverID string) error {
	isNAT, err := s.IsNAT(ctx, serverID)
	if err != nil {
		return err
	}
	if !isNAT {
		return panelerr.Validation("nat_port_server_not_nat", "NAT port mappings can only be managed on NAT servers")
	}
	return nil
}

func (s *Service) listNatPortMappings(ctx context.Context, serverID string) ([]domain.NatPortMapping, error) {
	var rows []models.NatPortMapping
	if err := orm.New(s.db).From("nat_port_mappings").Where("server_id=?", serverID).OrderBy("public_port ASC").All(ctx, &rows); err != nil {
		return nil, err
	}
	appIDs := make([]string, 0, len(rows))
	for i := range rows {
		appIDs = append(appIDs, rows[i].AppID)
	}
	names := s.resolveAppNames(ctx, appIDs)
	out := make([]domain.NatPortMapping, 0, len(rows))
	for i := range rows {
		out = append(out, natPortMappingDTO(rows[i], names[rows[i].AppID]))
	}
	return out, nil
}

func (s *Service) natPortMappingDTO(row models.NatPortMapping) domain.NatPortMapping {
	names := s.resolveAppNames(context.Background(), []string{row.AppID})
	return natPortMappingDTO(row, names[row.AppID])
}

func natPortMappingDTO(row models.NatPortMapping, appName string) domain.NatPortMapping {
	return domain.NatPortMapping{
		ID:         row.ID,
		ServerID:   row.ServerID,
		AppID:      row.AppID,
		AppName:    appName,
		HostPort:   row.HostPort,
		PublicPort: row.PublicPort,
		Protocol:   row.Protocol,
		Label:      row.Label,
		Notes:      row.Notes,
		CreatedAt:  row.CreatedAt,
		UpdatedAt:  row.UpdatedAt,
	}
}

func (s *Service) resolveAppNames(ctx context.Context, appIDs []string) map[string]string {
	out := map[string]string{}
	seen := map[string]struct{}{}
	ids := []string{}
	for _, a := range appIDs {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		if _, ok := seen[a]; ok {
			continue
		}
		seen[a] = struct{}{}
		ids = append(ids, a)
	}
	if len(ids) == 0 {
		return out
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, len(ids))
	for i, a := range ids {
		args[i] = a
	}
	rows, err := orm.Raw(ctx, s.db, `SELECT id,name FROM applications WHERE id IN (`+placeholders+`)`, args...)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var mappingID, name string
		if err := rows.Scan(&mappingID, &name); err == nil {
			out[mappingID] = name
		}
	}
	return out
}

// ensureNatPortUnique rejects a public or host port that is already mapped on
// the same server. excludeID lets an update skip its own row.
func (s *Service) ensureNatPortUnique(ctx context.Context, serverID string, hostPort, publicPort int, excludeID string) error {
	var exist models.NatPortMapping
	err := orm.New(s.db).From("nat_port_mappings").Where("server_id=? AND public_port=?", serverID, publicPort).First(ctx, &exist)
	if err == nil && (excludeID == "" || exist.ID != excludeID) {
		return panelerr.Conflict("nat_port_public_conflict", "Public port is already mapped on this server")
	} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	err = orm.New(s.db).From("nat_port_mappings").Where("server_id=? AND host_port=?", serverID, hostPort).First(ctx, &exist)
	if err == nil && (excludeID == "" || exist.ID != excludeID) {
		return panelerr.Conflict("nat_port_host_conflict", "Host port is already mapped on this server")
	} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	return nil
}

func normalizeNatPortSave(req domain.NatPortMappingSave) domain.NatPortMappingSave {
	req.Protocol = strings.ToLower(strings.TrimSpace(req.Protocol))
	if req.Protocol != "udp" {
		req.Protocol = "tcp"
	}
	return req
}

func validateNatPortSave(req domain.NatPortMappingSave) error {
	if req.HostPort <= 0 || req.HostPort > 65535 {
		return panelerr.Validation("nat_port_host_port_invalid", "Host port must be between 1 and 65535")
	}
	if req.PublicPort <= 0 || req.PublicPort > 65535 {
		return panelerr.Validation("nat_port_public_port_invalid", "Public port must be between 1 and 65535")
	}
	return nil
}

func natPortLabel(appName, label string, hostPort int) string {
	if label != "" {
		return label
	}
	if appName != "" {
		return appName + ":" + strconv.Itoa(hostPort)
	}
	return strconv.Itoa(hostPort)
}

func natTarget(host string, publicPort int) string {
	return host + ":" + strconv.Itoa(publicPort)
}