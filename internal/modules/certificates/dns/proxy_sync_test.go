package dns

import (
	"context"
	"strings"
	"testing"
)

type statefulFakeProvider struct {
	records []Record
	created int
	updated int
	deleted int
}

func (p *statefulFakeProvider) ListRecords(ctx context.Context, zone string) ([]Record, error) {
	out := make([]Record, len(p.records))
	copy(out, p.records)
	return out, nil
}

func (p *statefulFakeProvider) CreateRecord(ctx context.Context, zone string, record RecordInput) (Record, error) {
	p.created++
	created := Record{ID: "new", Name: record.Name, Type: record.Type, Value: record.Value, TTL: record.TTL, Proxied: record.Proxied != nil && *record.Proxied, Comment: record.Comment}
	p.records = append(p.records, created)
	return created, nil
}

func (p *statefulFakeProvider) UpdateRecord(ctx context.Context, zone string, id string, record RecordInput) (Record, error) {
	p.updated++
	for i := range p.records {
		if p.records[i].ID == id {
			p.records[i] = Record{ID: id, Name: record.Name, Type: record.Type, Value: record.Value, TTL: record.TTL, Proxied: record.Proxied != nil && *record.Proxied, Comment: record.Comment}
		}
	}
	return Record{ID: id, Name: record.Name, Type: record.Type, Value: record.Value, TTL: record.TTL, Proxied: record.Proxied != nil && *record.Proxied}, nil
}

func (p *statefulFakeProvider) DeleteRecord(ctx context.Context, zone string, id string) error {
	p.deleted++
	out := p.records[:0]
	for _, record := range p.records {
		if record.ID != id {
			out = append(out, record)
		}
	}
	p.records = out
	return nil
}

func newProxySyncService(t *testing.T, provider Provider) *Service {
	t.Helper()
	svc, closeStore := newDomainTestService(t)
	t.Cleanup(closeStore)
	svc.providerFactory = func(ResolvedDomain) Provider { return provider }
	if _, err := svc.CreateDomain(context.Background(), SaveDomainRequest{Name: "example.com", Provider: ProviderCloudflare, APIToken: "secret"}); err != nil {
		t.Fatal(err)
	}
	return svc
}

func TestSyncProxyRecordsCreatesAndDeletesOnlyManagedRecords(t *testing.T) {
	provider := &statefulFakeProvider{records: []Record{
		{ID: "managed_old", Name: "app.example.com", Type: "A", Value: "203.0.113.5", TTL: 120, Comment: ProxyManagedRecordComment},
		{ID: "user_record", Name: "app.example.com", Type: "A", Value: "198.51.100.9", TTL: 300},
		{ID: "other_managed", Name: "other.example.com", Type: "A", Value: "203.0.113.6", TTL: 120, Comment: ProxyManagedRecordComment},
	}}
	svc := newProxySyncService(t, provider)

	proxied := false
	results, err := svc.SyncProxyRecords(context.Background(), []ProxyRecordTarget{{
		Zone:  "example.com",
		Names: []string{"app.example.com"},
		Records: []RecordInput{
			{Name: "app", Type: "A", Value: "203.0.113.10", TTL: 120, Proxied: &proxied, Comment: ProxyManagedRecordComment},
			{Name: "app", Type: "AAAA", Value: "2001:db8::10", TTL: 120, Proxied: &proxied, Comment: ProxyManagedRecordComment},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	// Value changes replace by create+delete (keyed by type|name|value); AAAA is new.
	if len(results) != 1 || results[0].Created != 2 || results[0].Updated != 0 || results[0].Deleted != 1 {
		t.Fatalf("results = %#v", results)
	}
	byID := map[string]Record{}
	for _, record := range provider.records {
		byID[record.ID] = record
	}
	if byID["user_record"].Value != "198.51.100.9" || byID["other_managed"].Value != "203.0.113.6" {
		t.Fatalf("untouched records were modified: %#v", provider.records)
	}
	if _, ok := byID["managed_old"]; ok {
		t.Fatalf("replaced managed A should be deleted: %#v", provider.records)
	}
	foundNewA, foundAAAA := false, false
	for _, record := range provider.records {
		if record.Type == "A" && record.Value == "203.0.113.10" && record.Comment == ProxyManagedRecordComment {
			foundNewA = true
		}
		if record.Type == "AAAA" && record.Value == "2001:db8::10" {
			foundAAAA = true
		}
	}
	if !foundNewA || !foundAAAA {
		t.Fatalf("expected new managed A and AAAA: %#v", provider.records)
	}
}

func TestSyncProxyRecordsUpdatesValueAndCleansRemovedNames(t *testing.T) {
	provider := &statefulFakeProvider{records: []Record{
		{ID: "a1", Name: "app.example.com", Type: "A", Value: "203.0.113.5", TTL: 120, Comment: ProxyManagedRecordComment},
		{ID: "gone", Name: "old.example.com", Type: "A", Value: "203.0.113.9", TTL: 120, Comment: ProxyManagedRecordComment},
	}}
	svc := newProxySyncService(t, provider)

	proxied := false
	results, err := svc.SyncProxyRecords(context.Background(), []ProxyRecordTarget{{
		Zone:    "example.com",
		Names:   []string{"app.example.com", "old.example.com"},
		Records: []RecordInput{{Name: "app.example.com", Type: "A", Value: "203.0.113.10", TTL: 120, Proxied: &proxied, Comment: ProxyManagedRecordComment}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Created != 1 || results[0].Deleted != 2 {
		t.Fatalf("results = %#v", results)
	}
	if len(provider.records) != 1 || provider.records[0].Value != "203.0.113.10" {
		t.Fatalf("records after sync = %#v", provider.records)
	}
}

func TestSyncProxyRecordsKeepsMultipleARecordsForSameHost(t *testing.T) {
	provider := &statefulFakeProvider{records: []Record{
		{ID: "a1", Name: "app.example.com", Type: "A", Value: "203.0.113.5", TTL: 120, Comment: ProxyManagedRecordComment},
		{ID: "a2", Name: "app.example.com", Type: "A", Value: "203.0.113.6", TTL: 120, Comment: ProxyManagedRecordComment},
	}}
	svc := newProxySyncService(t, provider)

	proxied := false
	target := ProxyRecordTarget{
		Zone:  "example.com",
		Names: []string{"app.example.com"},
		Records: []RecordInput{
			{Name: "app", Type: "A", Value: "203.0.113.5", TTL: 120, Proxied: &proxied, Comment: ProxyManagedRecordComment},
			{Name: "app", Type: "A", Value: "203.0.113.6", TTL: 120, Proxied: &proxied, Comment: ProxyManagedRecordComment},
		},
	}
	results, err := svc.SyncProxyRecords(context.Background(), []ProxyRecordTarget{target})
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Created != 0 || results[0].Updated != 0 || results[0].Deleted != 0 || results[0].Unchanged != 2 {
		t.Fatalf("first sync results = %#v", results)
	}
	results, err = svc.SyncProxyRecords(context.Background(), []ProxyRecordTarget{target})
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Created != 0 || results[0].Deleted != 0 || results[0].Unchanged != 2 {
		t.Fatalf("idempotent sync results = %#v", results)
	}
	target.Records = target.Records[:1]
	results, err = svc.SyncProxyRecords(context.Background(), []ProxyRecordTarget{target})
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Deleted != 1 || len(provider.records) != 1 || provider.records[0].Value != "203.0.113.5" {
		t.Fatalf("after removing one A: results=%#v records=%#v", results, provider.records)
	}
}

func TestSyncProxyRecordsDetectsUserCNAMEConflict(t *testing.T) {
	provider := &statefulFakeProvider{records: []Record{
		{ID: "user_cname", Name: "app.example.com", Type: "CNAME", Value: "target.example.com", TTL: 300},
	}}
	svc := newProxySyncService(t, provider)

	proxied := false
	results, err := svc.SyncProxyRecords(context.Background(), []ProxyRecordTarget{{
		Zone:  "example.com",
		Names: []string{"app.example.com"},
		Records: []RecordInput{
			{Name: "app", Type: "A", Value: "203.0.113.10", TTL: 120, Proxied: &proxied, Comment: ProxyManagedRecordComment},
		},
	}})
	if err == nil || !strings.Contains(err.Error(), "CNAME") {
		t.Fatalf("error = %v, want CNAME conflict", err)
	}
	if provider.created != 0 {
		t.Fatalf("provider created %d records, want 0", provider.created)
	}
	if len(results) != 1 || results[0].Error == "" {
		t.Fatalf("results = %#v", results)
	}
}

func TestSyncProxyRecordsTakeoverDeletesUnmanagedConflicts(t *testing.T) {
	provider := &statefulFakeProvider{records: []Record{
		{ID: "user_cname", Name: "app.example.com", Type: "CNAME", Value: "target.example.com", TTL: 300},
		{ID: "user_a", Name: "app.example.com", Type: "A", Value: "198.51.100.9", TTL: 300},
		{ID: "keep_txt", Name: "app.example.com", Type: "TXT", Value: "v=spf1", TTL: 300},
		{ID: "other", Name: "other.example.com", Type: "A", Value: "203.0.113.6", TTL: 120},
	}}
	svc := newProxySyncService(t, provider)

	proxied := false
	results, err := svc.SyncProxyRecords(context.Background(), []ProxyRecordTarget{{
		Zone:     "example.com",
		Names:    []string{"app.example.com"},
		Takeover: true,
		Records: []RecordInput{
			{Name: "app", Type: "A", Value: "203.0.113.10", TTL: 120, Proxied: &proxied, Comment: ProxyManagedRecordComment},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Created != 1 || results[0].Deleted < 2 {
		t.Fatalf("results = %#v", results)
	}
	byID := map[string]Record{}
	for _, record := range provider.records {
		byID[record.ID] = record
	}
	if _, ok := byID["user_cname"]; ok {
		t.Fatalf("user CNAME should be removed on takeover: %#v", provider.records)
	}
	if _, ok := byID["user_a"]; ok {
		t.Fatalf("user A should be removed on takeover: %#v", provider.records)
	}
	if byID["keep_txt"].Value != "v=spf1" || byID["other"].Value != "203.0.113.6" {
		t.Fatalf("unrelated records must stay: %#v", provider.records)
	}
	foundManaged := false
	for _, record := range provider.records {
		if record.Comment == ProxyManagedRecordComment && record.Value == "203.0.113.10" {
			foundManaged = true
		}
	}
	if !foundManaged {
		t.Fatalf("managed record missing after takeover: %#v", provider.records)
	}
}

func TestSyncProxyRecordsReturnsErrorForUnmanagedZone(t *testing.T) {
	svc := newProxySyncService(t, &statefulFakeProvider{})
	_, err := svc.SyncProxyRecords(context.Background(), []ProxyRecordTarget{{Zone: "not-managed.example"}})
	if err == nil {
		t.Fatal("expected unmanaged zone error")
	}
}
