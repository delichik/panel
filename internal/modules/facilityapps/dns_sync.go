package facilityapps

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"panel/internal/modules/applications"
	"panel/internal/modules/certificates/dns"
	"panel/internal/modules/servers"
	"panel/internal/modules/tasks"
	"panel/internal/platform/database/orm"
)

const dnsSyncTTL = 120

type dnsSyncParams struct {
	Domains []string `json:"domains"`
}

// affectedFacilityDomains returns the domains whose DNS-relevant
// configuration changed between two saved proxy configs. Path and static
// asset changes do not affect DNS records and are ignored.
func affectedFacilityDomains(previous, next ReverseProxyConfig) []string {
	prevDomains := map[string]FacilityRouteDomain{}
	for _, domain := range previous.Domains {
		prevDomains[normalizeDNSName(domain.Domain)] = domain
	}
	nextDomains := map[string]FacilityRouteDomain{}
	for _, domain := range next.Domains {
		nextDomains[normalizeDNSName(domain.Domain)] = domain
	}
	names := map[string]struct{}{}
	for name := range prevDomains {
		names[name] = struct{}{}
	}
	for name := range nextDomains {
		names[name] = struct{}{}
	}
	for name := range names {
		prev, okPrev := prevDomains[name]
		next, okNext := nextDomains[name]
		if okPrev != okNext || (okPrev && domainDNSSignature(prev) != domainDNSSignature(next)) {
			continue
		}
		delete(names, name)
	}
	if panelEntryDNSSignature(previous.PanelEntry) != panelEntryDNSSignature(next.PanelEntry) {
		for _, panel := range []PanelEntry{previous.PanelEntry, next.PanelEntry} {
			if panel.Enabled {
				if name := normalizeDNSName(panel.Domain); name != "" {
					names[name] = struct{}{}
				}
			}
		}
	}
	out := make([]string, 0, len(names))
	for name := range names {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// dnsSyncDomainsOnSave returns every domain that must be checked when the
// reverse proxy config is saved: all currently configured domains, the Panel
// entry domain, and any domains removed from the previous config. Unchanged
// domains are included for verification but SyncProxyRecords only writes
// when their managed records differ from the desired set.
func dnsSyncDomainsOnSave(previous, next ReverseProxyConfig) []string {
	names := map[string]struct{}{}
	for _, domain := range next.Domains {
		if name := normalizeDNSName(domain.Domain); name != "" {
			names[name] = struct{}{}
		}
	}
	if next.PanelEntry.Enabled {
		if name := normalizeDNSName(next.PanelEntry.Domain); name != "" {
			names[name] = struct{}{}
		}
	}
	for _, name := range affectedFacilityDomains(previous, next) {
		names[name] = struct{}{}
	}
	out := make([]string, 0, len(names))
	for name := range names {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func domainDNSSignature(domain FacilityRouteDomain) string {
	return normalizeDNSName(domain.Domain) + "|" + strings.Join(uniqueSorted(domain.OriginServerIDs), ",") + "|" + anyAccessDNSSignature(domain.AnyAccess)
}

func anyAccessDNSSignature(config applications.AnyAccessConfig) string {
	return fmt.Sprintf("%v|%s|%s", config.Enabled, config.Strategy, config.PrimaryOriginServerID)
}

func panelEntryDNSSignature(entry PanelEntry) string {
	return fmt.Sprintf("%v|%s|%s", entry.Enabled, entry.ServerID, normalizeDNSName(entry.Domain))
}

func normalizeDNSName(value string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(value), "."))
}

func domainDNSServers(cfg ReverseProxyConfig, domain FacilityRouteDomain) []string {
	if domain.AnyAccess.Enabled {
		return cfg.DeploymentServers
	}
	return domain.OriginServerIDs
}

// SyncServersDNSEntries enqueues a DNS sync for every proxy domain that uses
// one of the given servers. It is wired as the server-side change trigger.
func (s *Service) SyncServersDNSEntries(ctx context.Context, serverIDs []string) error {
	if s.dns == nil || s.tasks == nil {
		return nil
	}
	cfg, err := s.loadConfig(ctx)
	if err != nil {
		return err
	}
	changed := map[string]struct{}{}
	for _, id := range serverIDs {
		changed[id] = struct{}{}
	}
	affected := map[string]struct{}{}
	for _, domain := range cfg.Domains {
		for _, id := range domainDNSServers(cfg, domain) {
			if _, ok := changed[id]; ok {
				affected[normalizeDNSName(domain.Domain)] = struct{}{}
				break
			}
		}
	}
	if cfg.PanelEntry.Enabled {
		if _, ok := changed[cfg.PanelEntry.ServerID]; ok {
			if name := normalizeDNSName(cfg.PanelEntry.Domain); name != "" {
				affected[name] = struct{}{}
			}
		}
	}
	if len(affected) == 0 {
		return nil
	}
	domains := make([]string, 0, len(affected))
	for name := range affected {
		domains = append(domains, name)
	}
	_, err = s.triggerDNSSync(ctx, domains, "server_change")
	return err
}

func (s *Service) triggerDNSSync(ctx context.Context, domains []string, triggerType string) (string, error) {
	if s.dns == nil || s.tasks == nil {
		return "", nil
	}
	domains = uniqueSorted(domains)
	if len(domains) == 0 {
		return "", nil
	}
	now := formatTime(time.Now().UTC())
	states := map[string]DNSSyncState{}
	for _, domain := range domains {
		states[domain] = DNSSyncState{State: DNSSyncPending, UpdatedAt: now}
	}
	if err := s.setDNSSyncStates(ctx, states, nil); err != nil {
		return "", err
	}
	raw, _ := json.Marshal(dnsSyncParams{Domains: domains})
	task, created, err := tasks.NewManager(s.tasks).Create(ctx, tasks.CreateInput{
		Type:         dnsSyncTaskType,
		ResourceType: "facility_app",
		ResourceID:   ReverseProxyID,
		TriggerType:  triggerType,
		ParamsJSON:   string(raw),
		Summary:      "Syncing reverse proxy DNS records",
	}, tasks.Trigger{Type: triggerType})
	if err != nil {
		return "", err
	}
	if !created {
		return task.ID, nil
	}
	if err := s.tasks.Start(ctx, task.ID); err != nil {
		return task.ID, err
	}
	go func() {
		runCtx := s.tasks.ExecutionContext(task.ID)
		defer s.tasks.FinishExecution(task.ID)
		if err := s.runDNSSync(runCtx, domains); err != nil {
			_ = s.tasks.Fail(runCtx, task.ID, err)
			return
		}
		_ = s.tasks.Complete(runCtx, task.ID, "DNS records synced")
	}()
	return task.ID, nil
}

// RunDNSSyncTask is the task framework executor for dns_proxy_records_sync.
func (s *Service) RunDNSSyncTask(tc tasks.TaskContext) error {
	if err := s.tasks.Start(tc.Context, tc.Task.ID); err != nil {
		return err
	}
	var params dnsSyncParams
	_ = json.Unmarshal([]byte(tc.Task.ParamsJSON), &params)
	if err := s.runDNSSync(tc.Context, params.Domains); err != nil {
		return err
	}
	return s.tasks.Complete(tc.Context, tc.Task.ID, "DNS records synced")
}

func (s *Service) runDNSSync(ctx context.Context, domains []string) error {
	if s.dns == nil {
		return nil
	}
	// 回收机制：执行时合并所有当前 pending 域名，避免同 key 活跃任务存在时
	// 新触发标记的 pending 域名被现有任务 params 遗漏而长期停留在 pending。
	domains = uniqueSorted(append(append([]string(nil), domains...), s.pendingDNSDomains(ctx)...))
	cfg, err := s.loadConfig(ctx)
	if err != nil {
		return err
	}
	servers := map[string]server.Server{}
	if s.servers != nil {
		list, err := s.servers.List(ctx)
		if err != nil {
			// 服务器列表读取失败时直接失败，绝不能把“读不到服务器”当成
			// “没有服务器”而进入无服务器清理分支，导致误删 DNS 记录。
			return err
		}
		for _, srv := range list {
			servers[srv.ID] = srv
		}
	}
	zones, err := s.dns.ListDomains(ctx)
	if err != nil {
		return err
	}
	zoneNames := make([]string, 0, len(zones))
	for _, zone := range zones {
		zoneNames = append(zoneNames, normalizeDNSName(zone.Name))
	}

	domainEntries := map[string]FacilityRouteDomain{}
	for _, domain := range cfg.Domains {
		domainEntries[normalizeDNSName(domain.Domain)] = domain
	}
	panelDomain := ""
	if cfg.PanelEntry.Enabled {
		panelDomain = normalizeDNSName(cfg.PanelEntry.Domain)
	}

	states := map[string]DNSSyncState{}
	removals := []string{}
	type zoneSync struct {
		target  dns.ProxyRecordTarget
		domains []string
	}
	byZone := map[string]*zoneSync{}
	for _, name := range uniqueSorted(domains) {
		name = normalizeDNSName(name)
		if name == "" {
			continue
		}
		domain, exists := domainEntries[name]
		if !exists && name != panelDomain {
			// Removed domain: its managed records must be cleaned up.
		} else {
			if name == panelDomain {
				domain = FacilityRouteDomain{Domain: name, OriginServerIDs: nonEmptyStrings(cfg.PanelEntry.ServerID)}
				exists = true
			}
			serverIDs := presentServers(domainDNSServers(cfg, domain), servers)
			if len(serverIDs) == 0 {
				zone := matchProxyZone(zoneNames, name)
				if zone == "" {
					removals = append(removals, name)
					continue
				}
				entry := byZone[zone]
				if entry == nil {
					entry = &zoneSync{target: dns.ProxyRecordTarget{Zone: zone}}
					byZone[zone] = entry
				}
				entry.domains = append(entry.domains, name)
				entry.target.Names = append(entry.target.Names, name)
				continue
			}
			records := desiredProxyRecords(name, serverIDs, servers)
			if len(records) == 0 {
				states[name] = DNSSyncState{State: DNSSyncSkipped, UpdatedAt: formatTime(time.Now().UTC()), Error: "servers have no ipv4/ipv6 configured"}
				continue
			}
			zone := matchProxyZone(zoneNames, name)
			if zone == "" {
				states[name] = DNSSyncState{State: DNSSyncSkipped, UpdatedAt: formatTime(time.Now().UTC()), Error: "DNS zone is not managed"}
				continue
			}
			entry := byZone[zone]
			if entry == nil {
				entry = &zoneSync{target: dns.ProxyRecordTarget{Zone: zone}}
				byZone[zone] = entry
			}
			entry.domains = append(entry.domains, name)
			entry.target.Records = append(entry.target.Records, records...)
			entry.target.Names = append(entry.target.Names, name)
			continue
		}
		zone := matchProxyZone(zoneNames, name)
		if zone == "" {
			removals = append(removals, name)
			continue
		}
		entry := byZone[zone]
		if entry == nil {
			entry = &zoneSync{target: dns.ProxyRecordTarget{Zone: zone}}
			byZone[zone] = entry
		}
		entry.domains = append(entry.domains, name)
		entry.target.Names = append(entry.target.Names, name)
	}

	now := formatTime(time.Now().UTC())
	var firstSyncErr error
	for _, entry := range byZone {
		results, syncErr := s.dns.SyncProxyRecords(ctx, []dns.ProxyRecordTarget{entry.target})
		var message string
		if syncErr != nil {
			message = syncErr.Error()
			if firstSyncErr == nil {
				firstSyncErr = syncErr
			}
		} else if len(results) > 0 && results[0].Error != "" {
			message = results[0].Error
			if firstSyncErr == nil {
				firstSyncErr = errors.New(results[0].Error)
			}
		}
		if message != "" {
			for _, name := range entry.domains {
				states[name] = DNSSyncState{State: DNSSyncFailed, UpdatedAt: now, Error: message}
			}
			continue
		}
		for _, name := range entry.domains {
			if _, exists := domainEntries[name]; !exists && name != panelDomain {
				removals = append(removals, name)
				continue
			}
			states[name] = DNSSyncState{State: DNSSyncSynced, UpdatedAt: now}
		}
	}
	if err := s.setDNSSyncStates(ctx, states, removals); err != nil {
		return err
	}
	return firstSyncErr
}

func presentServers(serverIDs []string, servers map[string]server.Server) []string {
	out := []string{}
	for _, serverID := range serverIDs {
		if _, ok := servers[serverID]; ok {
			out = append(out, serverID)
		}
	}
	return out
}

func desiredProxyRecords(name string, serverIDs []string, servers map[string]server.Server) []dns.RecordInput {
	records := []dns.RecordInput{}
	proxied := false
	for _, serverID := range serverIDs {
		srv := servers[serverID]
		if strings.TrimSpace(srv.IPv4) != "" {
			records = append(records, dns.RecordInput{Name: name, Type: "A", Value: strings.TrimSpace(srv.IPv4), TTL: dnsSyncTTL, Proxied: &proxied, Comment: dns.ProxyManagedRecordComment})
		}
		if strings.TrimSpace(srv.IPv6) != "" {
			records = append(records, dns.RecordInput{Name: name, Type: "AAAA", Value: strings.TrimSpace(srv.IPv6), TTL: dnsSyncTTL, Proxied: &proxied, Comment: dns.ProxyManagedRecordComment})
		}
	}
	return records
}

func matchProxyZone(zones []string, domain string) string {
	domain = normalizeDNSName(domain)
	base := domain
	if strings.HasPrefix(base, "*.") {
		base = strings.TrimPrefix(base, "*.")
	}
	best := ""
	for _, zone := range zones {
		zone = normalizeDNSName(zone)
		if zone == "" {
			continue
		}
		if zone == domain || zone == base {
			if len(zone) > len(best) {
				best = zone
			}
			continue
		}
		if strings.HasSuffix(base, "."+zone) && len(zone) > len(best) {
			best = zone
		}
	}
	return best
}

func (s *Service) setDNSSyncStates(ctx context.Context, updates map[string]DNSSyncState, removals []string) error {
	if s.db == nil {
		return nil
	}
	var raw string
	err := orm.New(s.db).From("facility_app_configs").Select("dns_sync_json").Where("id=?", ReverseProxyID).ScanValue(ctx, &raw)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	current := map[string]DNSSyncState{}
	_ = json.Unmarshal([]byte(raw), &current)
	if current == nil {
		current = map[string]DNSSyncState{}
	}
	for name, state := range updates {
		current[normalizeDNSName(name)] = state
	}
	for _, name := range removals {
		delete(current, normalizeDNSName(name))
	}
	nextRaw, err := json.Marshal(current)
	if err != nil {
		return err
	}
	_, err = orm.RawExec(ctx, s.db, `UPDATE facility_app_configs SET dns_sync_json=?,updated_at=? WHERE id=?`, string(nextRaw), formatTime(time.Now().UTC()), ReverseProxyID)
	return err
}

func (s *Service) pendingDNSDomains(ctx context.Context) []string {
	if s.db == nil {
		return nil
	}
	var raw string
	if err := orm.New(s.db).From("facility_app_configs").Select("dns_sync_json").Where("id=?", ReverseProxyID).ScanValue(ctx, &raw); err != nil {
		return nil
	}
	current := map[string]DNSSyncState{}
	_ = json.Unmarshal([]byte(raw), &current)
	out := []string{}
	for name, state := range current {
		if state.State == DNSSyncPending {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

func nonEmptyStrings(values ...string) []string {
	out := []string{}
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, strings.TrimSpace(value))
		}
	}
	return out
}
