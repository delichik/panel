package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	agentcontract "panel/internal/agent/contract"
	"panel/internal/platform/database/models"
	"panel/internal/platform/database/orm"
	panelerr "panel/internal/platform/errors"
	httpx "panel/internal/platform/http"
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
	if err := orm.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		if err := s.removeServerFromApplicationTargets(ctx, tx, serverID); err != nil {
			return err
		}
		if err := s.removeServerFromOverviewCards(ctx, tx, serverID); err != nil {
			return err
		}
		res, err := orm.RawExec(ctx, tx, `DELETE FROM servers WHERE id=?`, serverID)
		if err != nil {
			return err
		}
		affected, _ := res.RowsAffected()
		if affected == 0 {
			return panelerr.NotFound("server")
		}
		return nil
	}); err != nil {
		return err
	}
	if s.metricsDB != nil {
		if err := orm.New(s.metricsDB).From("metrics_snapshots").Where("server_id=?", serverID).Delete(ctx); err != nil {
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
	var rows []models.Application
	if err := orm.New(tx).From("applications").Where("deployment_server_ids_json<>?", "").All(ctx, &rows); err != nil {
		return err
	}
	type update struct {
		id  string
		raw string
	}
	updates := []update{}
	for _, app := range rows {
		next, changed := removeString(app.DeploymentServerIDsJSON, serverID)
		if !changed {
			continue
		}
		encoded, err := json.Marshal(next)
		if err != nil {
			return err
		}
		updates = append(updates, update{id: app.ID, raw: string(encoded)})
	}
	for _, item := range updates {
		if _, err := orm.RawExec(ctx, tx, `UPDATE applications SET version=version+1,deployment_server_ids_json=?,updated_at=? WHERE id=?`, item.raw, time.Now().UTC().Format(time.RFC3339Nano), item.id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) removeServerFromOverviewCards(ctx context.Context, tx *sql.Tx, serverID string) error {
	var rows []models.OverviewCardConfiguration
	if err := orm.New(tx).From("overview_card_configurations").Where("cards_json<>?", "").All(ctx, &rows); err != nil {
		return err
	}
	type update struct {
		id  string
		raw string
	}
	updates := []update{}
	for _, row := range rows {
		cards := row.CardsJSON
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
		updates = append(updates, update{id: row.ID, raw: string(encoded)})
	}
	for _, item := range updates {
		if err := orm.New(tx).From("overview_card_configurations").Where("id=?", item.id).UpdateColumns(ctx, map[string]any{
			"cards_json": item.raw,
			"updated_at": time.Now().UTC().Format(time.RFC3339),
		}); err != nil {
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

func (s *Service) ListSummaries(ctx context.Context) ([]ServerSummary, error) {
	return s.repo.ListSummaries(ctx)
}

func (s *Service) ListSummaryPage(ctx context.Context, page, pageSize int, query string) (httpx.ListPage[ServerSummary], error) {
	return s.repo.ListSummaryPage(ctx, page, pageSize, query)
}

func (s *Service) Get(ctx context.Context, serverID string) (Server, error) {
	srv, err := s.repo.Get(ctx, serverID)
	if err != nil {
		return Server{}, err
	}
	return s.prepareServerForRead(ctx, srv), nil
}
