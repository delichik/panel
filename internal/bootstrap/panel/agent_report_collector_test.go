package panel

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	agentcontract "panel/internal/agent/contract"
	server "panel/internal/modules/servers"
	"panel/internal/modules/settings"
	"panel/internal/platform/config"
	storage "panel/internal/platform/database"
)

func TestSampleAligned(t *testing.T) {
	if !sampleAligned(time.Unix(12, 0), 3) {
		t.Fatal("12 should align to 3 second interval")
	}
	if sampleAligned(time.Unix(10, 0), 3) {
		t.Fatal("10 should not align to 3 second interval")
	}
}

func TestAgentReportStreamEndpointChangeKeepsNewEntry(t *testing.T) {
	recorder := &fakeReportServerProvider{servers: []server.Server{reportReadyServer("srv-1", "https://127.0.0.1:9786")}}
	collector := newAgentReportCollector(recorder, nil, newReportCollectorSettings(t), nil, nil)
	ctx := context.Background()

	collector.ensureStream(ctx, recorder.servers[0])
	oldEntry := collector.streams["srv-1"]
	recorder.servers[0].Traits[agentcontract.TraitURL] = "https://127.0.0.1:9787"
	collector.ensureStream(ctx, recorder.servers[0])
	newEntry := collector.streams["srv-1"]

	if oldEntry == nil || newEntry == nil || oldEntry == newEntry {
		t.Fatalf("expected endpoint change to replace stream entry, old=%p new=%p", oldEntry, newEntry)
	}
	if newEntry.endpoint != "https://127.0.0.1:9787" {
		t.Fatalf("endpoint = %q, want updated endpoint", newEntry.endpoint)
	}
	collector.deleteEntryIfCurrent(oldEntry)
	if collector.streams["srv-1"] != newEntry {
		t.Fatal("old stream cleanup deleted the replacement stream")
	}
}

func TestAgentReportAuditMarksSilentStreamDisconnected(t *testing.T) {
	recorder := &fakeReportServerProvider{}
	collector := newAgentReportCollector(recorder, nil, newReportCollectorSettings(t), nil, nil)
	cancelled := false
	collector.streams["srv-1"] = &agentReportStream{
		serverID:  "srv-1",
		endpoint:  "https://127.0.0.1:9786",
		cancel:    func() { cancelled = true },
		startedAt: time.Now().UTC().Add(-time.Minute),
	}

	collector.auditSilentStreams()

	if !cancelled {
		t.Fatal("silent stream should be cancelled")
	}
	if collector.streams["srv-1"] != nil {
		t.Fatal("silent stream should be removed")
	}
	if len(recorder.reportRecords) != 1 || recorder.reportRecords[0].connected {
		t.Fatalf("expected one disconnected record, got %#v", recorder.reportRecords)
	}
	if recorder.reportRecords[0].message == "" {
		t.Fatal("disconnected record should include timeout message")
	}
}

func TestAgentReportMarkConnectedUpdatesLastMessage(t *testing.T) {
	recorder := &fakeReportServerProvider{}
	collector := newAgentReportCollector(recorder, nil, newReportCollectorSettings(t), nil, nil)
	entry := &agentReportStream{serverID: "srv-1", endpoint: "https://127.0.0.1:9786", cancel: func() {}, startedAt: time.Now().UTC()}
	collector.streams["srv-1"] = entry
	sampleAt := time.Unix(30, 0).UTC()

	collector.markConnected(entry, sampleAt)

	if !entry.lastMessageAt.Equal(sampleAt) {
		t.Fatalf("lastMessageAt = %s, want %s", entry.lastMessageAt, sampleAt)
	}
	if len(recorder.reportRecords) != 1 || !recorder.reportRecords[0].connected {
		t.Fatalf("expected one connected record, got %#v", recorder.reportRecords)
	}
}

func newReportCollectorSettings(t *testing.T) *settings.Service {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.TaskDatabase = filepath.Join(dir, "tasks.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	svc, err := settings.NewService(store.AppDB(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func reportReadyServer(id, endpoint string) server.Server {
	return server.Server{
		ID: id,
		Traits: map[string]string{
			agentcontract.TraitEnabled: "true",
			agentcontract.TraitURL:     endpoint,
			agentcontract.TraitStatus:  agentcontract.StatusCompatible,
		},
	}
}

type fakeReportServerProvider struct {
	servers       []server.Server
	reportRecords []fakeReportRecord
}

type fakeReportRecord struct {
	serverID      string
	connected     bool
	lastMessageAt time.Time
	message       string
}

func (f *fakeReportServerProvider) List(context.Context) ([]server.Server, error) {
	return append([]server.Server(nil), f.servers...), nil
}

func (f *fakeReportServerProvider) RecordAgentReportStream(_ context.Context, serverID string, connected bool, lastMessageAt time.Time, message string) error {
	f.reportRecords = append(f.reportRecords, fakeReportRecord{serverID: serverID, connected: connected, lastMessageAt: lastMessageAt, message: message})
	return nil
}
