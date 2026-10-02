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
	"panel/internal/platform/logging"

	"go.uber.org/zap"
)

func (s *Service) Create(ctx context.Context, req SaveRequest) (Server, error) {
	if err := validateSave(req); err != nil {
		return Server{}, err
	}
	now := time.Now().UTC()
	srv := Server{
		ID:           id.New("srv"),
		Name:         req.Name,
		Host:         derivedServerHost(req),
		IPv4:         strings.TrimSpace(req.IPv4),
		IPv6:         strings.TrimSpace(req.IPv6),
		Port:         req.Port,
		SSHUsername:  req.SSHUsername,
		CredentialID: req.CredentialID,
		DockerHost:   normalizeDockerHost(req.DockerHost),
		Traits:       map[string]string{},
		Variables:    normalizeServerVariables(req.Variables, map[string]string{}),
		Notes:        req.Notes,
		CreatedAt:    now,
		UpdatedAt:    now,

		TailscaleEnabled:            req.TailscaleEnabled,
		TailscalePreferAgent:        req.TailscaleEnabled && req.TailscalePreferAgent,
		TailscalePreferInterconnect: req.TailscaleEnabled && req.TailscalePreferInterconnect,
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
			// 记录一旦写入就不再由系统自动删除；派发失败时保留记录并标记失败态，供用户重试、编辑或自行删除。
			_ = s.recordReachability(ctx, srv.ID, false, false, "initial server task could not be started: "+err.Error())
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
	nextHost := derivedServerHost(req)
	nextIPv4 := strings.TrimSpace(req.IPv4)
	nextIPv6 := strings.TrimSpace(req.IPv6)
	previousIPv4 := strings.TrimSpace(current.IPv4)
	previousIPv6 := strings.TrimSpace(current.IPv6)
	nextAgentURL := agentURLForPort(nextHost, defaultAgentPort)
	hostChanged := strings.TrimSpace(current.Host) != nextHost
	previousTailscaleEnabled := current.TailscaleEnabled
	previousTailscalePreferAgent := current.TailscalePreferAgent
	previousTailscalePreferInterconnect := current.TailscalePreferInterconnect
	if hostChanged && serverHasAgentConfigured(current, current.Traits) {
		current.Traits[agentcontract.TraitEnabled] = "true"
		current.Traits[agentcontract.TraitURL] = nextAgentURL
		current.Traits[agentcontract.TraitStatus] = agentcontract.StatusIncompatible
		current.Traits[agentcontract.TraitLastError] = "server host changed; agent redeployment required"
		delete(current.Traits, agentcontract.TraitCertificateFingerprint)
		delete(current.Traits, agentcontract.TraitCertificateNotBefore)
		delete(current.Traits, agentcontract.TraitCertificateNotAfter)
	} else if serverHasAgentConfigured(current, current.Traits) &&
		strings.TrimSpace(current.Traits[agentcontract.TraitURL]) != nextAgentURL {
		// The agent's reachable URL changed (e.g. a NAT external port was set).
		// No redeployment is required — the provider maps the public port to the
		// internal 9786 — but the panel must reconnect to the new endpoint.
		current.Traits[agentcontract.TraitEnabled] = "true"
		current.Traits[agentcontract.TraitURL] = nextAgentURL
		current.Traits[agentcontract.TraitStatus] = agentcontract.StatusIncompatible
		current.Traits[agentcontract.TraitLastError] = "agent URL changed; reconnecting"
	}
	current.Name = req.Name
	current.Host = nextHost
	current.IPv4 = nextIPv4
	current.IPv6 = nextIPv6
	current.Port = req.Port
	current.SSHUsername = req.SSHUsername
	current.CredentialID = req.CredentialID
	current.DockerHost = normalizeDockerHost(req.DockerHost)
	current.Variables = normalizeServerVariables(req.Variables, current.Traits)
	current.Notes = req.Notes
	current.UpdatedAt = time.Now().UTC()
	// 未启用 tailscale 的节点不可能拥有 tailnet 地址，因此两个优先开关必须
	// 一起归零，避免持久化出无法生效的意图。
	current.TailscaleEnabled = req.TailscaleEnabled
	current.TailscalePreferAgent = req.TailscaleEnabled && req.TailscalePreferAgent
	current.TailscalePreferInterconnect = req.TailscaleEnabled && req.TailscalePreferInterconnect
	if !req.TailscaleEnabled {
		clearTailscaleTraits(current.Traits)
	}
	if err := s.repo.Update(ctx, current); err != nil {
		return Server{}, err
	}
	if s.exec != nil {
		// The server is already persisted. A failed connectivity probe only
		// marks the server unreachable (TestConnectivity records that) and must
		// not fail the update or skip the DNS sync below.
		if _, err := s.TestConnectivity(ctx, serverID); err != nil {
			logging.L().Warn("server update connectivity probe failed", zap.String("server_id", serverID), zap.Error(err))
		}
	}
	s.notifyDNSSync(ctx, serverID, previousIPv4 != nextIPv4 || previousIPv6 != nextIPv6)
	s.reconcileTailscaleIntent(ctx, serverID, req.TailscaleEnabled)
	s.reconcileAgentCertificateHosts(ctx, serverID, previousTailscalePreferAgent, req.TailscaleEnabled && req.TailscalePreferAgent)
	// 互联地址选择依赖“启用 + 优先”两个意图：任一变化（含关闭节点后清除观测
	// 地址）都必须让设施重算内嵌的对端地址。
	if previousTailscaleEnabled != current.TailscaleEnabled || previousTailscalePreferInterconnect != current.TailscalePreferInterconnect {
		s.notifyInterconnectChange(ctx, serverID)
	}
	return s.Get(ctx, serverID)
}

// reconcileAgentCertificateHosts 在“优先使用 tailscale 地址连接 agent”开关变化
// 后校验节点证书的 SAN 集合。启用时需要把 tailnet 地址加入证书，关闭时需要把
// 它移除；两者都通过既有的 Agent 部署通道完成（仅证书刷新，不重传二进制）。
func (s *Service) reconcileAgentCertificateHosts(ctx context.Context, serverID string, previousPrefer, nextPrefer bool) {
	if previousPrefer == nextPrefer {
		return
	}
	srv, err := s.Get(ctx, serverID)
	if err != nil {
		return
	}
	if strings.TrimSpace(srv.Traits[agentcontract.TraitEnabled]) != "true" {
		return
	}
	if sameStringSet(agentCertificateHosts(srv), splitTraitList(srv.Traits[agentcontract.TraitCertificateHosts])) {
		return
	}
	if err := s.markAgentStatus(ctx, serverID, agentcontract.StatusIncompatible, "", "tailscale agent address preference changed; agent certificate refresh required"); err != nil {
		logging.L().Warn("mark agent certificate refresh failed", zap.String("server_id", serverID), zap.Error(err))
	}
}

// clearTailscaleTraits 在用户关闭 tailscale 时清除观测态：保留过期的 tailnet
// 地址会让他人（互联地址选择、界面）继续把节点当作 tailnet 成员。
func clearTailscaleTraits(traits map[string]string) {
	for _, key := range []string{
		agentcontract.TraitTailscaleStatus,
		agentcontract.TraitTailscaleIPv4,
		agentcontract.TraitTailscaleIPv6,
		agentcontract.TraitTailscaleHostname,
		agentcontract.TraitTailscaleVersion,
		agentcontract.TraitTailscaleLastError,
		agentcontract.TraitTailscaleUpdatedAt,
	} {
		delete(traits, key)
	}
}

func (s *Service) Delete(ctx context.Context, serverID string) error {
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
	if s.agentKeys != nil {
		// ID-unique asset naming keeps an orphan harmless, so cleanup failure
		// must not turn an already-committed delete into a reported failure.
		if err := s.agentKeys.DeleteAgentServerCertificate(ctx, serverID); err != nil {
			logging.L().Warn("agent server certificate cleanup failed", zap.String("server_id", serverID), zap.Error(err))
		}
	}
	if s.tasks != nil {
		if _, err := s.tasks.CancelByServer(ctx, serverID, "Task cancelled because the server was removed"); err != nil {
			return err
		}
	}
	s.notifyDNSSync(ctx, serverID, true)
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
