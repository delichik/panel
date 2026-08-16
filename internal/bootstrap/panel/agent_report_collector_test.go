package panel

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	agentclient "panel/internal/agent/client"
	agentcontract "panel/internal/agent/contract"
	containerization "panel/internal/modules/containers"
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

func TestReconnectBackoffResetsAfterDeliveredConnection(t *testing.T) {
	cases := []struct {
		name        string
		hadMessages bool
		current     time.Duration
		wantWait    time.Duration
		wantNext    time.Duration
	}{
		{"first failure waits initial 5s and doubles for next", false, 5 * time.Second, 5 * time.Second, 10 * time.Second},
		{"consecutive failures keep doubling", false, 20 * time.Second, 20 * time.Second, 40 * time.Second},
		{"backoff caps at 5 minutes", false, 5 * time.Minute, 5 * time.Minute, 5 * time.Minute},
		{"never waits below 5s", false, 0, 5 * time.Second, 10 * time.Second},
		{"delivered connection resets wait to 5s", true, 5 * time.Minute, 5 * time.Second, 10 * time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wait, next := reconnectBackoff(tc.hadMessages, tc.current)
			if wait != tc.wantWait || next != tc.wantNext {
				t.Fatalf("reconnectBackoff(%v, %s) = (%s, %s), want (%s, %s)", tc.hadMessages, tc.current, wait, next, tc.wantWait, tc.wantNext)
			}
		})
	}
}

func TestAgentReportHandleReportContainerSaveFailureKeepsStreamAlive(t *testing.T) {
	recorder := &fakeReportServerProvider{}
	collector := newAgentReportCollector(recorder, nil, newReportCollectorSettings(t), nil, &containerization.Service{}, nil)
	// 空 serverID 会让 SaveReportedContainers 返回校验错误；容器分支必须
	// 只记录日志并把错误吞掉，否则整个上报流会被杀掉并触发重连退避。
	err := collector.handleReport(context.Background(), "", agentclient.AgentReport{
		SampleAt:      time.Unix(30, 0).UTC(),
		HasContainers: true,
		Containers:    []agentcontract.DockerContainer{{ID: "c1"}},
		Reason:        "container_change",
	})
	if err != nil {
		t.Fatalf("handleReport must not fail the stream on container save errors, got %v", err)
	}
}

func TestAgentReportStreamEndpointChangeKeepsNewEntry(t *testing.T) {
	recorder := &fakeReportServerProvider{servers: []server.Server{reportReadyServer("srv-1", "https://127.0.0.1:9786")}}
	collector := newAgentReportCollector(recorder, nil, newReportCollectorSettings(t), nil, nil, nil)
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
	collector := newAgentReportCollector(recorder, nil, newReportCollectorSettings(t), nil, nil, nil)
	cancelled := false
	collector.streams["srv-1"] = &agentReportStream{
		serverID:  "srv-1",
		endpoint:  "https://127.0.0.1:9786",
		cancel:    func() { cancelled = true },
		startedAt: time.Now().UTC().Add(-3 * time.Minute),
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
	collector := newAgentReportCollector(recorder, nil, newReportCollectorSettings(t), nil, nil, nil)
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
	cfg.LogDatabase = filepath.Join(dir, "log.db")
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

func TestAgentReportCollectorWiresImageReporter(t *testing.T) {
	recorder := &fakeReportServerProvider{}
	containerSvc := &containerization.Service{}
	collector := newAgentReportCollector(recorder, nil, newReportCollectorSettings(t), nil, containerSvc, nil)
	if collector.images != containerSvc {
		t.Fatal("image reporter should be wired to the container service")
	}
}

func TestAgentReportHandleReportSavesImages(t *testing.T) {
	recorder := &fakeReportServerProvider{}
	collector := newAgentReportCollector(recorder, nil, newReportCollectorSettings(t), nil, nil, nil)
	saved := &fakeImageReporter{}
	collector.images = saved
	err := collector.handleReport(context.Background(), "srv-1", agentclient.AgentReport{
		SampleAt: time.Unix(30, 0).UTC(),
		Images:   []agentcontract.DockerImage{{ID: "sha256:abc"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.calls) != 1 {
		t.Fatalf("expected one image report save, got %d", len(saved.calls))
	}
	if saved.calls[0].serverID != "srv-1" || len(saved.calls[0].images) != 1 || saved.calls[0].images[0].ID != "sha256:abc" {
		t.Fatalf("unexpected image report save: %#v", saved.calls[0])
	}
}

type fakeImageReportCall struct {
	serverID string
	images   []agentcontract.DockerImage
}

type fakeImageReporter struct {
	calls []fakeImageReportCall
}

func (f *fakeImageReporter) SaveReportedImages(_ context.Context, serverID string, images []agentcontract.DockerImage) error {
	f.calls = append(f.calls, fakeImageReportCall{serverID: serverID, images: images})
	return nil
}
