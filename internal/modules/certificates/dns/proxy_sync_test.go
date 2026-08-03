package dns

import (
	"context"
	"testing"
)

type statefulFakeProvider struct {
	records  []Record
	created  int
	updated  int
	deleted  int
}

func (p *statefulFakeProvider) ListRecords(ctx context.Context, zone string) ([]Record, error) {
	return p.records, nil
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
	if len(results) != 1 || results[0].Created != 1 || results[0].Updated != 1 || results[0].Deleted != 0 {
		t.Fatalf("results = %#v", results)
	}
	byID := map[string]Record{}
	for _, record := range provider.records {
		byID[record.ID] = record
	}
	if byID["user_record"].Value != "198.51.100.9" || byID["other_managed"].Value != "203.0.113.6" {
		t.Fatalf("untouched records were modified: %#v", provider.records)
	}
	if byID["managed_old"].Value != "203.0.113.10" {
		t.Fatalf("managed record was not updated: %#v", provider.records)
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
	if results[0].Updated != 1 || results[0].Deleted != 1 {
		t.Fatalf("results = %#v", results)
	}
	if len(provider.records) != 1 || provider.records[0].Value != "203.0.113.10" {
		t.Fatalf("records after sync = %#v", provider.records)
	}
}

func TestSyncProxyRecordsReturnsErrorForUnmanagedZone(t *testing.T) {
	svc := newProxySyncService(t, &statefulFakeProvider{})
	_, err := svc.SyncProxyRecords(context.Background(), []ProxyRecordTarget{{Zone: "not-managed.example"}})
	if err == nil {
		t.Fatal("expected unmanaged zone error")
	}
}
