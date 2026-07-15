package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	agentcontract "panel/internal/agent/contract"
	panelerr "panel/internal/platform/errors"
	id "panel/internal/platform/identity"
)

func (s *Service) Create(ctx context.Context, req SaveRequest) (Server, error) {
	if err := validateSave(req); err != nil {
		return Server{}, err
	}
	now := time.Now().UTC()
	srv := Server{
		ID:           id.New("srv"),
		Name:         req.Name,
		Host:         req.Host,
		Port:         req.Port,
		SSHUsername:  req.SSHUsername,
		CredentialID: req.CredentialID,
		DockerHost:   normalizeDockerHost(req.DockerHost),
		Traits:       req.Traits,
		Variables:    normalizeServerVariables(req.Variables, req.Traits),
		Notes:        req.Notes,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if srv.Traits == nil {
		srv.Traits = map[string]string{}
	}
	if err := s.repo.Insert(ctx, srv); err != nil {
		return Server{}, err
	}
	if s.exec != nil {
		task, err := s.EnsureInitialInfoTask(ctx, srv.ID, true)
		if err != nil {
			_ = s.repo.Delete(ctx, srv.ID)
			return Server{}, err
		}
		srv.InitialTaskID = task.ID
	}
	return srv, nil
}

func (s *Service) Update(ctx context.Context, serverID string, req SaveRequest) (Server, error) {
	if err := validateSave(req); err != nil {
		return Server{}, err
	}
	current, err := s.Get(ctx, serverID)
	if err != nil {
		return Server{}, err
	}
	if req.Traits == nil {
		req.Traits = map[string]string{}
	}
	if strings.TrimSpace(current.Host) != strings.TrimSpace(req.Host) && serverHasAgentConfigured(current, req.Traits) {
		req.Traits[agentcontract.TraitEnabled] = "true"
		req.Traits[agentcontract.TraitURL] = agentDefaultURL(req.Host)
		req.Traits[agentcontract.TraitStatus] = agentcontract.StatusIncompatible
		req.Traits[agentcontract.TraitLastError] = "server host changed; agent redeployment required"
		delete(req.Traits, agentcontract.TraitCertificateFingerprint)
		delete(req.Traits, agentcontract.TraitCertificateNotBefore)
		delete(req.Traits, agentcontract.TraitCertificateNotAfter)
	}
	current.Name = req.Name
	current.Host = req.Host
	current.Port = req.Port
	current.SSHUsername = req.SSHUsername
	current.CredentialID = req.CredentialID
	current.DockerHost = normalizeDockerHost(req.DockerHost)
	current.Traits = req.Traits
	current.Variables = normalizeServerVariables(req.Variables, req.Traits)
	current.Notes = req.Notes
	current.UpdatedAt = time.Now().UTC()
	if err := s.repo.Update(ctx, current); err != nil {
		return Server{}, err
	}
	if s.exec != nil {
		if _, err := s.TestConnectivity(ctx, serverID); err != nil {
			return Server{}, err
		}
	}
	return s.Get(ctx, serverID)
}

func (s *Service) Delete(ctx context.Context, serverID string) error {
	if s.hostGuard != nil {
		isHost, err := s.hostGuard.IsHostServer(ctx, serverID)
		if err != nil {
			return err
		}
		if isHost {
			return panelerr.Conflict("panel_host_server_delete_forbidden", "Panel host server cannot be deleted")
		}
	}
	if s.tasks != nil {
		if _, err := s.tasks.CancelByServer(ctx, serverID, "Task cancelled because the server was removed"); err != nil {
			return err
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := s.removeServerFromApplicationTargets(ctx, tx, serverID); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := s.removeServerFromOverviewCards(ctx, tx, serverID); err != nil {
		_ = tx.Rollback()
		return err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM servers WHERE id=?`, serverID)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		_ = tx.Rollback()
		return panelerr.NotFound("server")
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if s.metricsDB != nil {
		if _, err := s.metricsDB.ExecContext(ctx, `DELETE FROM metrics_snapshots WHERE server_id=?`, serverID); err != nil {
			return err
		}
	}
	if s.tasks != nil {
		if _, err := s.tasks.CancelByServer(ctx, serverID, "Task cancelled because the server was removed"); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) removeServerFromApplicationTargets(ctx context.Context, tx *sql.Tx, serverID string) error {
	rows, err := tx.QueryContext(ctx, `SELECT id,deployment_server_ids_json FROM applications WHERE deployment_server_ids_json<>''`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type update struct {
		id  string
		raw string
	}
	updates := []update{}
	for rows.Next() {
		var appID, raw string
		if err := rows.Scan(&appID, &raw); err != nil {
			return err
		}
		var ids []string
		if err := json.Unmarshal([]byte(raw), &ids); err != nil {
			continue
		}
		next, changed := removeString(ids, serverID)
		if !changed {
			continue
		}
		encoded, err := json.Marshal(next)
		if err != nil {
			return err
		}
		updates = append(updates, update{id: appID, raw: string(encoded)})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, item := range updates {
		if _, err := tx.ExecContext(ctx, `UPDATE applications SET deployment_server_ids_json=?,updated_at=? WHERE id=?`, item.raw, time.Now().UTC().Format(time.RFC3339Nano), item.id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) removeServerFromOverviewCards(ctx context.Context, tx *sql.Tx, serverID string) error {
	rows, err := tx.QueryContext(ctx, `SELECT id,cards_json FROM overview_card_configurations WHERE cards_json<>''`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type update struct {
		id  string
		raw string
	}
	updates := []update{}
	for rows.Next() {
		var id, raw string
		if err := rows.Scan(&id, &raw); err != nil {
			return err
		}
		var cards []map[string]any
		if err := json.Unmarshal([]byte(raw), &cards); err != nil {
			continue
		}
		changed := false
		for _, card := range cards {
			values, ok := card["serverIds"].([]any)
			if !ok {
				continue
			}
			next := make([]any, 0, len(values))
			for _, value := range values {
				if text, ok := value.(string); ok && text == serverID {
					changed = true
					continue
				}
				next = append(next, value)
			}
			card["serverIds"] = next
		}
		if !changed {
			continue
		}
		encoded, err := json.Marshal(cards)
		if err != nil {
			return err
		}
		updates = append(updates, update{id: id, raw: string(encoded)})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, item := range updates {
		if _, err := tx.ExecContext(ctx, `UPDATE overview_card_configurations SET cards_json=?,updated_at=? WHERE id=?`, item.raw, time.Now().UTC().Format(time.RFC3339), item.id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) List(ctx context.Context) ([]Server, error) {
	out, err := s.repo.List(ctx)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i] = s.prepareServerForRead(ctx, out[i])
	}
	for i := range out {
		out[i].LoadAverage = s.latestLoadAverage(ctx, out[i].ID)
	}
	return out, nil
}

func (s *Service) Get(ctx context.Context, serverID string) (Server, error) {
	srv, err := s.repo.Get(ctx, serverID)
	if err != nil {
		return Server{}, err
	}
	return s.prepareServerForRead(ctx, srv), nil
}
