package facilityapps

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"panel/internal/modules/applications"
	"panel/internal/platform/database/models"
	"panel/internal/platform/database/orm"
	panelerr "panel/internal/platform/errors"
	"panel/internal/platform/i18n"
)

const (
	configRecordKindFacilityDomain   = "facility_domain"
	configRecordKindApplicationRoute = "application_route"
)

func (s *Service) GetProxyPlane(ctx context.Context) (ProxyPlaneState, error) {
	cfg, err := s.loadConfig(ctx)
	if err != nil {
		return ProxyPlaneState{}, err
	}
	out := ProxyPlaneState{
		ID:                ReverseProxyID,
		DeploymentServers: append([]string{}, cfg.DeploymentServers...),
		LastError:         cfg.LastError,
		UpdatedAt:         cfg.UpdatedAt,
		ReconcileStopped:  s.proxyReconcileStopped(ctx),
	}
	if deployments, err := s.deploymentDiagnostics(ctx, cfg.DeploymentServers); err == nil {
		out.Deployments = deployments
	}
	return out, nil
}

func (s *Service) SaveProxyPlane(ctx context.Context, in ProxyPlaneSaveInput) (ProxyPlaneState, error) {
	s.editCommitMu.Lock()
	defer s.editCommitMu.Unlock()
	previous, err := s.loadConfig(ctx)
	if err != nil {
		return ProxyPlaneState{}, err
	}
	nextServers := uniqueSorted(in.DeploymentServers)
	// 移除网关前先确认没有配置记录仍把它当作 origin / AnyAccess primary 或
	// relay 使用，否则会留下指向非网关服务器的上游。全部停用（空集合）是
	// 显式的“关闭入口网关”，保留记录但不再渲染，因此放行。
	if len(nextServers) > 0 {
		if err := s.validateProxyPlaneGatewayRemoval(ctx, nextServers); err != nil {
			return ProxyPlaneState{}, err
		}
	}
	if err := s.saveProxyServers(ctx, previous.Version, nextServers); err != nil {
		return ProxyPlaneState{}, err
	}
	if !equalStringSlices(uniqueSorted(previous.DeploymentServers), nextServers) {
		s.triggerProxyPlaneDNSSync(ctx)
	}
	if err := s.syncReverseProxyTraits(ctx, previous.DeploymentServers, nextServers); err != nil {
		_ = s.setLastError(ctx, err.Error())
	}
	stopIDs := removedServers(previous.DeploymentServers, nextServers)
	// Proxy-plane: recreate/ensure containers (not config reload).
	if err := s.triggerReverseProxyReconcile(ctx, "user", stopIDs, nil); err != nil {
		_ = s.setLastError(ctx, err.Error())
		return ProxyPlaneState{}, panelerr.New(http.StatusInternalServerError, "facility_proxy_ensure_failed", err.Error())
	}
	_ = s.setLastError(ctx, "")
	return s.GetProxyPlane(ctx)
}

func (s *Service) saveProxyServers(ctx context.Context, expectedVersion int, servers []string) error {
	raw, _ := json.Marshal(servers)
	now := formatTime(time.Now().UTC())
	var result sql.Result
	var err error
	if expectedVersion == 0 {
		result, err = s.db.ExecContext(ctx, `INSERT INTO facility_app_configs(id,version,deployment_server_ids_json,last_error,updated_at) VALUES(?,1,?,?,?) ON CONFLICT(id) DO NOTHING`, ReverseProxyID, string(raw), "", now)
	} else {
		result, err = s.db.ExecContext(ctx, `UPDATE facility_app_configs SET version=version+1,deployment_server_ids_json=?,updated_at=? WHERE id=? AND version=?`, string(raw), now, ReverseProxyID, expectedVersion)
	}
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 && expectedVersion != 0 {
		return panelerr.Conflict("resource_version_conflict", "facility proxy plane changed")
	}
	return nil
}

type proxyPlaneReference struct {
	RecordID         string   `json:"recordId"`
	Kind             string   `json:"kind"`
	ApplicationID    string   `json:"applicationId,omitempty"`
	MissingServerIDs []string `json:"missingServerIds"`
}

// validateProxyPlaneGatewayRemoval enforces FAC-RP-006 / UI-FAC-006: the proxy
// plane must not leave configuration records pointing at servers that are no
// longer gateways. Records are owned by the config plane, so the proxy plane
// blocks instead of rewriting (and bumping) them.
func (s *Service) validateProxyPlaneGatewayRemoval(ctx context.Context, nextServers []string) error {
	next := stringSetValues(nextServers)
	references := make([]proxyPlaneReference, 0)
	appendReference := func(recordID, kind, applicationID string, candidates ...[]string) {
		missing := make([]string, 0)
		seen := map[string]struct{}{}
		for _, list := range candidates {
			for _, serverID := range list {
				serverID = strings.TrimSpace(serverID)
				if serverID == "" {
					continue
				}
				if _, ok := next[serverID]; ok {
					continue
				}
				if _, ok := seen[serverID]; ok {
					continue
				}
				seen[serverID] = struct{}{}
				missing = append(missing, serverID)
			}
		}
		if len(missing) > 0 {
			references = append(references, proxyPlaneReference{
				RecordID:         recordID,
				Kind:             kind,
				ApplicationID:    applicationID,
				MissingServerIDs: uniqueSorted(missing),
			})
		}
	}
	var rows []models.ReverseProxyRoute
	if err := orm.New(s.db).From("reverse_proxy_routes").Where("app_id IN (?,?)", proxyApplicationID, "facility_reverse_proxy").OrderBy("domain ASC").All(ctx, &rows); err != nil {
		return err
	}
	for _, row := range rows {
		anyAccess := mustAnyAccess(row)
		appendReference(strings.ToLower(strings.TrimSpace(row.Domain)), configRecordKindFacilityDomain, "", row.OriginServerIDs, []string{anyAccess.PrimaryOriginServerID}, anyAccess.RelayServerIDs)
	}
	if s.apps != nil {
		apps, err := s.apps.ApplicationReverseProxyConfigs(ctx)
		if err != nil {
			return err
		}
		for _, app := range apps {
			for _, route := range app.Routes {
				appendReference(strings.ToLower(strings.TrimSpace(route.Domain)), configRecordKindApplicationRoute, app.ApplicationID, route.OriginServerIDs, []string{route.AnyAccess.PrimaryOriginServerID}, route.AnyAccess.RelayServerIDs)
			}
		}
	}
	if len(references) == 0 {
		return nil
	}
	return panelerr.WithDetails(panelerr.Conflict("facility_proxy_plane_gateway_in_use", "Some gateways are still referenced by configuration records; update those records before removing them"), map[string]any{"records": references})
}

// triggerProxyPlaneDNSSync refreshes DNS for records whose published target is
// the gateway set (AnyAccess enabled). Gateway membership changes alter those
// targets even though no configuration record version changes.
func (s *Service) triggerProxyPlaneDNSSync(ctx context.Context) {
	domains := make([]string, 0)
	var rows []models.ReverseProxyRoute
	if err := orm.New(s.db).From("reverse_proxy_routes").Where("app_id IN (?,?)", proxyApplicationID, "facility_reverse_proxy").All(ctx, &rows); err != nil {
		return
	}
	for _, row := range rows {
		if mustAnyAccess(row).Enabled {
			domains = append(domains, strings.ToLower(strings.TrimSpace(row.Domain)))
		}
	}
	if s.apps != nil {
		if apps, err := s.apps.ApplicationReverseProxyConfigs(ctx); err == nil {
			for _, app := range apps {
				for _, route := range app.Routes {
					if route.AnyAccess.Enabled {
						domains = append(domains, strings.ToLower(strings.TrimSpace(route.Domain)))
					}
				}
			}
		}
	}
	domains = uniqueSorted(domains)
	if len(domains) == 0 {
		return
	}
	if _, err := s.triggerDNSSync(ctx, domains, "facility_app", false); err != nil {
		_ = s.setLastError(ctx, err.Error())
	}
}

func equalStringSlices(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func (s *Service) ListConfigRecords(ctx context.Context) ([]ConfigRecord, error) {
	facility, err := s.listFacilityDomainRecords(ctx)
	if err != nil {
		return nil, err
	}
	apps, err := s.listApplicationRouteRecords(ctx)
	if err != nil {
		return nil, err
	}
	return append(facility, apps...), nil
}

func (s *Service) GetConfigRecord(ctx context.Context, recordID string) (ConfigRecord, error) {
	recordID = strings.ToLower(strings.TrimSpace(recordID))
	if recordID == "" {
		return ConfigRecord{}, panelerr.Validation("facility_record_id_required", "recordId is required")
	}
	row, err := s.loadRouteRow(ctx, recordID)
	if err != nil {
		return ConfigRecord{}, err
	}
	domain, err := s.routeRowToDomain(ctx, row)
	if err != nil {
		return ConfigRecord{}, err
	}
	domain.Version = row.Version
	assets, _ := s.listStaticAssetsForRecord(ctx, recordID)
	kind := configRecordKindFacilityDomain
	appID, appName := "", ""
	if row.AppID != proxyApplicationID && row.AppID != "facility_reverse_proxy" {
		kind = configRecordKindApplicationRoute
		appID = row.AppID
	}
	return ConfigRecord{
		RecordID:        recordID,
		Kind:            kind,
		Version:         row.Version,
		Domain:          domain,
		StaticAssets:    assets,
		ApplicationID:   appID,
		ApplicationName: appName,
		UpdatedAt:       row.UpdatedAt,
	}, nil
}

func (s *Service) DeleteConfigRecord(ctx context.Context, recordID string, expectedVersion int) error {
	recordID = strings.ToLower(strings.TrimSpace(recordID))
	s.editCommitMu.Lock()
	defer s.editCommitMu.Unlock()
	row, err := s.loadRouteRow(ctx, recordID)
	if err != nil {
		return err
	}
	if expectedVersion > 0 && row.Version != expectedVersion {
		return panelerr.Conflict("resource_version_conflict", "facility config record changed")
	}
	if row.AppID != proxyApplicationID && row.AppID != "facility_reverse_proxy" {
		return panelerr.Validation("facility_record_not_facility_domain", "application route records must be deleted from the application editor")
	}
	gateways, _ := s.loadConfig(ctx)
	affected := anyAccessRouteServerIDs(mustAnyAccess(row), row.OriginServerIDs, gateways.DeploymentServers)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM facility_static_assets WHERE record_id=?`, recordID); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM reverse_proxy_routes WHERE domain=? AND version=?`, recordID, row.Version)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return panelerr.Conflict("resource_version_conflict", "facility config record changed")
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	_ = s.triggerConfigReload(ctx, recordID, affected)
	if _, dnsErr := s.triggerDNSSync(ctx, []string{recordID}, "facility_app", false); dnsErr != nil {
		_ = s.setLastError(ctx, dnsErr.Error())
	}
	return nil
}

func (s *Service) ApplyConfigRecord(ctx context.Context, recordID string) error {
	record, err := s.GetConfigRecord(ctx, recordID)
	if err != nil {
		return err
	}
	cfg, err := s.loadConfig(ctx)
	if err != nil {
		return err
	}
	serverIDs := anyAccessRouteServerIDs(record.Domain.AnyAccess, record.Domain.OriginServerIDs, cfg.DeploymentServers)
	if err := s.triggerConfigReload(ctx, recordID, serverIDs); err != nil {
		return panelerr.New(http.StatusInternalServerError, "facility_config_apply_failed", err.Error())
	}
	return nil
}

func (s *Service) triggerConfigReload(ctx context.Context, recordID string, serverIDs []string) error {
	return s.triggerReverseProxyReconcile(ctx, "facility_app", nil, uniqueSorted(serverIDs))
}

func (s *Service) listFacilityDomainRecords(ctx context.Context) ([]ConfigRecord, error) {
	var rows []models.ReverseProxyRoute
	if err := orm.New(s.db).From("reverse_proxy_routes").Where("app_id=?", proxyApplicationID).OrderBy("domain ASC").All(ctx, &rows); err != nil {
		return nil, err
	}
	out := make([]ConfigRecord, 0, len(rows))
	for _, row := range rows {
		domain, err := s.routeRowToDomain(ctx, row)
		if err != nil {
			return nil, err
		}
		domain.Version = row.Version
		assets, _ := s.listStaticAssetsForRecord(ctx, row.Domain)
		out = append(out, ConfigRecord{
			RecordID:     strings.ToLower(row.Domain),
			Kind:         configRecordKindFacilityDomain,
			Version:      row.Version,
			Domain:       domain,
			StaticAssets: assets,
			UpdatedAt:    row.UpdatedAt,
		})
	}
	return out, nil
}

func (s *Service) listApplicationRouteRecords(ctx context.Context) ([]ConfigRecord, error) {
	if s.apps == nil {
		return nil, nil
	}
	routes, err := s.apps.ApplicationReverseProxyConfigs(ctx)
	if err != nil {
		return nil, err
	}
	out := []ConfigRecord{}
	for _, app := range routes {
		for _, route := range app.Routes {
			domain := strings.ToLower(strings.TrimSpace(route.Domain))
			row, err := s.loadRouteRow(ctx, domain)
			version := 1
			updated := time.Time{}
			if err == nil {
				version = row.Version
				updated = row.UpdatedAt
			}
			out = append(out, ConfigRecord{
				RecordID: domain,
				Kind:     configRecordKindApplicationRoute,
				Version:  version,
				Domain: FacilityRouteDomain{
					Domain:          domain,
					Version:         version,
					OriginServerIDs: append([]string{}, route.OriginServerIDs...),
					AnyAccess:       route.AnyAccess,
				},
				ApplicationID:   app.ApplicationID,
				ApplicationName: app.ApplicationName,
				UpdatedAt:       updated,
			})
		}
	}
	return out, nil
}

func (s *Service) loadRouteRow(ctx context.Context, domain string) (models.ReverseProxyRoute, error) {
	var row models.ReverseProxyRoute
	if err := orm.New(s.db).From("reverse_proxy_routes").Where("domain=?", strings.ToLower(strings.TrimSpace(domain))).First(ctx, &row); err != nil {
		if err == sql.ErrNoRows {
			return models.ReverseProxyRoute{}, panelerr.NotFound("facility_config_record")
		}
		return models.ReverseProxyRoute{}, err
	}
	return row, nil
}

func (s *Service) routeRowToDomain(ctx context.Context, row models.ReverseProxyRoute) (FacilityRouteDomain, error) {
	var anyAccess applications.AnyAccessConfig
	raw, _ := json.Marshal(row.AnyAccessJSON)
	_ = json.Unmarshal(raw, &anyAccess)
	var paths []FacilityRoutePath
	pathsRaw, _ := json.Marshal(row.PathsJSON)
	_ = json.Unmarshal(pathsRaw, &paths)
	domain := FacilityRouteDomain{
		Domain:          strings.ToLower(strings.TrimSpace(row.Domain)),
		Version:         row.Version,
		OriginServerIDs: uniqueSorted(row.OriginServerIDs),
		AnyAccess:       anyAccess,
		Paths:           paths,
	}
	for i := range domain.Paths {
		s.normalizeStoredRouteAsset(ctx, &domain.Paths[i])
	}
	return domain, nil
}

func mustAnyAccess(row models.ReverseProxyRoute) applications.AnyAccessConfig {
	var anyAccess applications.AnyAccessConfig
	raw, _ := json.Marshal(row.AnyAccessJSON)
	_ = json.Unmarshal(raw, &anyAccess)
	return anyAccess
}

func (s *Service) listStaticAssetsForRecord(ctx context.Context, recordID string) ([]StaticAsset, error) {
	var rows []models.FacilityStaticAsset
	if err := orm.New(s.db).From("facility_static_assets").Where("record_id=?", strings.ToLower(strings.TrimSpace(recordID))).OrderBy("name ASC").All(ctx, &rows); err != nil {
		return nil, err
	}
	out := make([]StaticAsset, 0, len(rows))
	for _, row := range rows {
		out = append(out, StaticAsset{
			ID: row.ID, Name: row.Name, Kind: row.Kind, ContentMode: row.ContentMode,
			Filename: row.Filename, Size: int64(row.Size), SHA256: row.SHA256,
			CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		})
	}
	return out, nil
}

func recordResourceVersion(record ConfigRecord) applications.ResourceVersion {
	return applications.ResourceVersion{Value: strconv.Itoa(record.Version), UpdatedAt: record.UpdatedAt}
}

func facilityApplyFailedDiag(err error) applications.Diagnostic {
	return applications.Diagnostic{
		Code:     "facility_apply_request_failed",
		Severity: "warning",
		Message:  i18n.Translate("facility_apply_request_failed", "Configuration was committed, but applying it could not be requested"),
		Details:  map[string]any{"error": err.Error()},
	}
}
