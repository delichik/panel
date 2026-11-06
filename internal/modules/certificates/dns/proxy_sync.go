package dns

import (
	"context"
	"errors"
	"strings"
)

// ProxyManagedRecordComment marks DNS records created by the reverse proxy
// facility so a later sync can safely update or remove only those records.
const ProxyManagedRecordComment = "panel:reverse-proxy"

// ProxyRecordTarget is one managed zone and the desired records Panel owns.
type ProxyRecordTarget struct {
	Zone     string
	Names    []string
	Records  []RecordInput
	Takeover bool
}

// ProxyZoneResult summarizes the mutations applied to one zone.
type ProxyZoneResult struct {
	Zone      string `json:"zone"`
	Created   int    `json:"created"`
	Updated   int    `json:"updated"`
	Deleted   int    `json:"deleted"`
	Unchanged int    `json:"unchanged"`
	Error     string `json:"error,omitempty"`
}

// SyncProxyRecords reconciles the desired Panel-managed records for the given
// zones. It only mutates records carrying ProxyManagedRecordComment; other
// records are left untouched. Every zone is attempted independently so one
// provider failure does not block the remaining zones.
func (s *Service) SyncProxyRecords(ctx context.Context, targets []ProxyRecordTarget) ([]ProxyZoneResult, error) {
	if len(targets) == 0 {
		return []ProxyZoneResult{}, nil
	}
	results := make([]ProxyZoneResult, 0, len(targets))
	var syncErrors []error
	for _, target := range targets {
		result, err := s.syncProxyZone(ctx, target)
		results = append(results, result)
		if err != nil {
			syncErrors = append(syncErrors, err)
		}
	}
	return results, errors.Join(syncErrors...)
}

func (s *Service) syncProxyZone(ctx context.Context, target ProxyRecordTarget) (ProxyZoneResult, error) {
	result := ProxyZoneResult{Zone: target.Zone}
	domain, provider, err := s.resolveProviderByName(ctx, target.Zone)
	if err != nil {
		result.Error = err.Error()
		return result, err
	}
	existing, err := provider.ListRecords(ctx, domain.Name)
	if err != nil {
		result.Error = err.Error()
		return result, err
	}
	managedNames := map[string]struct{}{}
	for _, name := range target.Names {
		managedNames[proxyFullName(domain.Name, name)] = struct{}{}
	}
	desired := []RecordInput{}
	desiredByKey := map[string]RecordInput{}
	for _, record := range target.Records {
		prepared, err := validateRecordInput(record)
		if err != nil {
			result.Error = err.Error()
			return result, err
		}
		// Key includes value so multi-server A/AAAA for the same host are kept.
		key := proxyRecordKey(domain.Name, prepared)
		if _, exists := desiredByKey[key]; !exists {
			desiredByKey[key] = prepared
			desired = append(desired, prepared)
		}
	}

	if target.Takeover {
		remaining, deleted, takeoverErr := deleteUnmanagedHostConflicts(ctx, provider, domain.Name, existing, managedNames)
		if takeoverErr != nil {
			result.Error = takeoverErr.Error()
			return result, takeoverErr
		}
		existing = remaining
		result.Deleted += deleted
	}

	managed := map[string][]Record{}
	for _, record := range existing {
		if record.Comment != ProxyManagedRecordComment {
			continue
		}
		if _, ok := managedNames[proxyFullName(domain.Name, record.Name)]; !ok {
			continue
		}
		key := proxyRecordKey(domain.Name, RecordInput{Name: record.Name, Type: record.Type, Value: record.Value})
		managed[key] = append(managed[key], record)
	}

	for _, record := range desired {
		key := proxyRecordKey(domain.Name, record)
		matches := managed[key]
		if len(matches) > 0 {
			match := matches[0]
			if recordEqual(record, match) {
				result.Unchanged++
			} else {
				updated, updateErr := provider.UpdateRecord(ctx, domain.Name, match.ID, record)
				if updateErr != nil {
					result.Error = updateErr.Error()
					return result, updateErr
				}
				_ = updated
				result.Updated++
			}
			managed[key] = matches[1:]
			if len(managed[key]) == 0 {
				delete(managed, key)
			}
			continue
		}
		if conflict := hostConflict(existing, domain.Name, record, ""); conflict != nil {
			conflictErr := recordConflictError(record, *conflict)
			result.Error = conflictErr.Error()
			return result, conflictErr
		}
		created, createErr := provider.CreateRecord(ctx, domain.Name, record)
		if createErr != nil {
			result.Error = createErr.Error()
			return result, createErr
		}
		_ = created
		result.Created++
	}

	for _, records := range managed {
		for _, record := range records {
			if err := provider.DeleteRecord(ctx, domain.Name, record.ID); err != nil {
				result.Error = err.Error()
				return result, err
			}
			result.Deleted++
		}
	}
	if err := s.refreshRecords(ctx, domain.ID); err != nil {
		result.Error = err.Error()
		return result, err
	}
	return result, nil
}

// deleteUnmanagedHostConflicts removes non-Panel A/AAAA/CNAME records on the
// managed hostnames so Panel can write its reverse-proxy records. TXT/MX and
// other types are left untouched. Only records whose comment is not
// ProxyManagedRecordComment are deleted.
func deleteUnmanagedHostConflicts(ctx context.Context, provider Provider, zone string, existing []Record, managedNames map[string]struct{}) ([]Record, int, error) {
	remaining := make([]Record, 0, len(existing))
	deleted := 0
	for _, record := range existing {
		fullName := proxyFullName(zone, record.Name)
		if _, managed := managedNames[fullName]; !managed {
			remaining = append(remaining, record)
			continue
		}
		if record.Comment == ProxyManagedRecordComment {
			remaining = append(remaining, record)
			continue
		}
		if !cnameHostConflictType(record.Type) {
			remaining = append(remaining, record)
			continue
		}
		if err := provider.DeleteRecord(ctx, zone, record.ID); err != nil {
			return existing, deleted, err
		}
		deleted++
	}
	return remaining, deleted, nil
}

func proxyTypeNameKey(zone string, record RecordInput) string {
	return strings.ToUpper(strings.TrimSpace(record.Type)) + "|" + proxyFullName(zone, record.Name)
}

// proxyRecordKey identifies one managed reverse-proxy DNS record. Value is part
// of the key so multiple A/AAAA addresses on the same hostname converge
// independently (DNS-SYNC-002/004).
func proxyRecordKey(zone string, record RecordInput) string {
	return proxyTypeNameKey(zone, record) + "|" + strings.ToLower(strings.TrimSpace(record.Value))
}

func proxyFullName(zone, name string) string {
	zone = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(zone), "."))
	name = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(name), "."))
	if name == "" || name == "@" || name == zone {
		return zone
	}
	if strings.HasSuffix(name, "."+zone) {
		return name
	}
	return name + "." + zone
}

func recordEqual(desired RecordInput, current Record) bool {
	ttl := desired.TTL
	if ttl <= 0 {
		ttl = 120
	}
	if ttl != current.TTL {
		return false
	}
	proxied := false
	if desired.Proxied != nil {
		proxied = *desired.Proxied
	}
	return proxied == current.Proxied
}
