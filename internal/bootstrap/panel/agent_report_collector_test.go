package panel

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	agentclient "panel/internal/agent/client"
	agentcontract "panel/internal/agent/contract"
	containerization "panel/internal/modules/containers"
	"panel/internal/modules/networkprobe"
	"panel/internal/modules/observability/metrics"
	"panel/internal/modules/observability/metricsfacility"
	server "panel/internal/modules/servers"
	"panel/internal/modules/settings"
	"panel/internal/platform/config"
	storage "panel/internal/platform/database"
	"panel/internal/platform/linux"
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
	// streaming=true 表示连接已建立但静默无上报：audit 必须回收。
	collector.streams["srv-1"] = &agentReportStream{
		serverID:  "srv-1",
		endpoint:  "https://127.0.0.1:9786",
		cancel:    func() { cancelled = true },
		startedAt: time.Now().UTC().Add(-3 * time.Minute),
		streaming: true,
	}
	// streaming=false 表示处于重连退避等待中的条目：audit 不得回收，
	// 否则会把刚积累的退避清零，断线 Agent 永远按 5s 轮询。
	collector.streams["srv-2"] = &agentReportStream{
		serverID:  "srv-2",
		endpoint:  "https://127.0.0.1:9786",
		cancel:    func() { t.Fatal("backoff entry must not be cancelled") },
		startedAt: time.Now().UTC().Add(-3 * time.Minute),
	}

	collector.auditSilentStreams()

	if !cancelled {
		t.Fatal("silent stream should be cancelled")
	}
	if collector.streams["srv-1"] != nil {
		t.Fatal("silent stream should be removed")
	}
	if collector.streams["srv-2"] == nil {
		t.Fatal("backoff entry should stay in the stream map")
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
	cfg.CoordinationDatabase = filepath.Join(dir, "coordination.db")
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
	servers          []server.Server
	reportRecords    []fakeReportRecord
	tailscaleRecords []fakeTailscaleRecord
}

type fakeReportRecord struct {
	serverID      string
	connected     bool
	lastMessageAt time.Time
	message       string
}

type fakeTailscaleRecord struct {
	serverID string
	status   agentcontract.TailscaleStatus
}

func (f *fakeReportServerProvider) List(context.Context) ([]server.Server, error) {
	return append([]server.Server(nil), f.servers...), nil
}

func (f *fakeReportServerProvider) RecordAgentReportStream(_ context.Context, serverID string, connected bool, lastMessageAt time.Time, message string) error {
	f.reportRecords = append(f.reportRecords, fakeReportRecord{serverID: serverID, connected: connected, lastMessageAt: lastMessageAt, message: message})
	return nil
}

// ApplyTailscaleReport 记录上报的 tailscale 观测态，使测试可以断言只有携带该
// 字段的报告才会写入观测。
func (f *fakeReportServerProvider) ApplyTailscaleReport(_ context.Context, serverID string, status agentcontract.TailscaleStatus) error {
	f.tailscaleRecords = append(f.tailscaleRecords, fakeTailscaleRecord{serverID: serverID, status: status})
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

// fakeProbeReporter 模拟网络探测设施在 Agent 上报链路中的两个入口。
type fakeProbeReporter struct {
	report         networkprobe.ProbeReport
	reportErr      error
	reportServerID string
	saved          []fakeProbeSave
	saveErr        error
}

type fakeProbeSave struct {
	serverID string
	sampleAt time.Time
	results  []networkprobe.ReportedResult
}

func (f *fakeProbeReporter) ReportConfig(_ context.Context, serverID string) (networkprobe.ProbeReport, error) {
	f.reportServerID = serverID
	return f.report, f.reportErr
}

func (f *fakeProbeReporter) SaveReported(_ context.Context, serverID string, sampleAt time.Time, results []networkprobe.ReportedResult) error {
	f.saved = append(f.saved, fakeProbeSave{serverID: serverID, sampleAt: sampleAt, results: results})
	return f.saveErr
}

// TestAgentReportReportProbeConfigConverts covers FAC-PRB: the probe config read
// from the facility is converted into the agent protocol structure, and a failed
// lookup degrades to disabled instead of failing the report stream.
func TestAgentReportReportProbeConfigConverts(t *testing.T) {
	recorder := &fakeReportServerProvider{}
	collector := newAgentReportCollector(recorder, nil, newReportCollectorSettings(t), nil, nil, nil)
	probes := &fakeProbeReporter{report: networkprobe.ProbeReport{
		Enabled:         true,
		IntervalSeconds: 60,
		Targets: []networkprobe.ProbeTarget{
			{ServerID: "srv-2", Address: "10.0.0.2", Name: "b"},
			{ServerID: "srv-3", Address: "fd00::3", Name: "c"},
		},
	}}
	collector.SetNetworkProbe(probes)
	got := collector.reportProbeConfig(context.Background(), "srv-1")
	if probes.reportServerID != "srv-1" || !got.Enabled || got.IntervalSeconds != 60 || len(got.Targets) != 2 {
		t.Fatalf("probe config = %#v (serverID=%q)", got, probes.reportServerID)
	}
	if got.Targets[0].ServerID != "srv-2" || got.Targets[0].Address != "10.0.0.2" || got.Targets[0].Name != "b" {
		t.Fatalf("converted target = %#v", got.Targets[0])
	}

	failing := &fakeProbeReporter{reportErr: context.DeadlineExceeded}
	collector.SetNetworkProbe(failing)
	disabled := collector.reportProbeConfig(context.Background(), "srv-1")
	if disabled.Enabled || len(disabled.Targets) != 0 {
		t.Fatalf("probe config lookup failure must degrade to disabled, got %#v", disabled)
	}

	collector.SetNetworkProbe(nil)
	if empty := collector.reportProbeConfig(context.Background(), "srv-1"); empty.Enabled || len(empty.Targets) != 0 {
		t.Fatalf("missing probe facility must leave probes disabled, got %#v", empty)
	}
}

// TestAgentReportHandleReportSavesProbes covers AGT-RPT-008: only reports that
// explicitly carry probe results are persisted, and failures never break the
// stream.
func TestAgentReportHandleReportSavesProbes(t *testing.T) {
	recorder := &fakeReportServerProvider{}
	collector := newAgentReportCollector(recorder, nil, newReportCollectorSettings(t), nil, nil, nil)
	probes := &fakeProbeReporter{}
	collector.SetNetworkProbe(probes)
	sampleAt := time.Unix(30, 0).UTC()
	err := collector.handleReport(context.Background(), "srv-1", agentclient.AgentReport{
		SampleAt:  sampleAt,
		HasProbes: true,
		Probes: []agentclient.ProbeResult{
			{TargetServerID: "srv-2", TargetAddress: "10.0.0.2", LatencyMS: 1.5, PacketLossPercent: 0, Sent: 3, Received: 3},
			{TargetServerID: "srv-3", TargetAddress: "fd00::3", LatencyMS: 0, PacketLossPercent: 100, Sent: 3, Received: 0, Error: "timeout"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(probes.saved) != 1 {
		t.Fatalf("probe save calls = %d, want 1", len(probes.saved))
	}
	saved := probes.saved[0]
	if saved.serverID != "srv-1" || !saved.sampleAt.Equal(sampleAt) || len(saved.results) != 2 {
		t.Fatalf("saved probe report = %#v", saved)
	}
	if saved.results[0].TargetServerID != "srv-2" || saved.results[0].LatencyMS != 1.5 || saved.results[0].Received != 3 {
		t.Fatalf("converted probe result = %#v", saved.results[0])
	}
	if saved.results[1].Error != "timeout" {
		t.Fatalf("probe error must be preserved: %#v", saved.results[1])
	}

	collector.SetNetworkProbe(&fakeProbeReporter{saveErr: context.DeadlineExceeded})
	if err := collector.handleReport(context.Background(), "srv-1", agentclient.AgentReport{
		SampleAt:  sampleAt,
		HasProbes: true,
		Probes:    []agentclient.ProbeResult{{TargetServerID: "srv-2"}},
	}); err != nil {
		t.Fatalf("probe save failure must not fail the stream: %v", err)
	}
}

// TestAgentReportHandleReportIgnoresProbesWithoutFlag keeps old agents from
// clearing reported results.
func TestAgentReportHandleReportIgnoresProbesWithoutFlag(t *testing.T) {
	recorder := &fakeReportServerProvider{}
	collector := newAgentReportCollector(recorder, nil, newReportCollectorSettings(t), nil, nil, nil)
	probes := &fakeProbeReporter{}
	collector.SetNetworkProbe(probes)
	if err := collector.handleReport(context.Background(), "srv-1", agentclient.AgentReport{
		SampleAt: time.Unix(30, 0).UTC(),
		Probes:   []agentclient.ProbeResult{{TargetServerID: "srv-2"}},
	}); err != nil {
		t.Fatal(err)
	}
	if len(probes.saved) != 0 {
		t.Fatalf("probes without HasProbes must not be saved, got %#v", probes.saved)
	}
}

// TestAgentReportHandleReportReplayedMetrics covers OBS-MET-009: replayed
// samples skip the interval alignment check, and samples that did not refresh
// disk usage are stored with zeroed disk fields while old agents keep them.
func TestAgentReportHandleReportReplayedMetrics(t *testing.T) {
	settingsSvc, metricsSvc, metricsDB := newReportCollectorMetrics(t)
	collector := newAgentReportCollector(&fakeReportServerProvider{}, nil, settingsSvc, metricsSvc, nil, nil)
	ctx := context.Background()

	// 31s 与默认 60s 采集间隔不对齐；Replayed=true 时仍必须落库。
	sampled := false
	notSampled := linux.MetricsSnapshot{DiskUsedBytes: 4096, DiskTotalBytes: 8192, CPUUsagePercent: 11}
	if err := collector.handleReport(ctx, "srv-replay", agentclient.AgentReport{
		SampleAt:                time.Unix(31, 0).UTC(),
		Metrics:                 &notSampled,
		MetricsDiskUsageSampled: &sampled,
		Replayed:                true,
	}); err != nil {
		t.Fatal(err)
	}
	var used, total int64
	if err := metricsDB.QueryRow(`SELECT disk_used_bytes,disk_total_bytes FROM metrics_snapshots WHERE server_id='srv-replay'`).Scan(&used, &total); err != nil {
		t.Fatal(err)
	}
	if used != 0 || total != 0 {
		t.Fatalf("unsampled disk usage must be zeroed, got used=%d total=%d", used, total)
	}

	// 旧 Agent（nil 标记）保持上报值。
	oldAgent := linux.MetricsSnapshot{DiskUsedBytes: 1024, DiskTotalBytes: 2048, CPUUsagePercent: 5}
	if err := collector.handleReport(ctx, "srv-old", agentclient.AgentReport{
		SampleAt: time.Unix(120, 0).UTC(),
		Metrics:  &oldAgent,
	}); err != nil {
		t.Fatal(err)
	}
	if err := metricsDB.QueryRow(`SELECT disk_used_bytes,disk_total_bytes FROM metrics_snapshots WHERE server_id='srv-old'`).Scan(&used, &total); err != nil {
		t.Fatal(err)
	}
	if used != 1024 || total != 2048 {
		t.Fatalf("old agent disk values must be preserved, got used=%d total=%d", used, total)
	}

	// 非补报且不对齐的样本仍然跳过。
	missed := linux.MetricsSnapshot{CPUUsagePercent: 7}
	if err := collector.handleReport(ctx, "srv-missed", agentclient.AgentReport{
		SampleAt: time.Unix(31, 0).UTC(),
		Metrics:  &missed,
	}); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := metricsDB.QueryRow(`SELECT COUNT(*) FROM metrics_snapshots WHERE server_id='srv-missed'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("unaligned non-replayed sample must be skipped, got %d rows", count)
	}
}

// fakeMetricsFacility 模拟指标设施在 Agent 上报链路中的两个入口：按服务器
// 计算采集目标，以及供静默检测使用的设施间隔。
type fakeMetricsFacility struct {
	reports  map[string]metricsfacility.ReportConfig
	err      error
	interval int
	asked    []string
}

func (f *fakeMetricsFacility) ReportConfig(_ context.Context, serverID string) (metricsfacility.ReportConfig, error) {
	f.asked = append(f.asked, serverID)
	if f.err != nil {
		return metricsfacility.ReportConfig{}, f.err
	}
	return f.reports[serverID], nil
}

func (f *fakeMetricsFacility) IntervalSeconds() int { return f.interval }

// TestAgentReportStreamConfigTargetsMetricsPerServer covers FAC-MET-002: the
// report stream tells each server whether it collects metrics (0 = disabled)
// and with which facility interval, while the container interval still comes
// from runtime settings. An absent facility degrades to enabled/60s.
func TestAgentReportStreamConfigTargetsMetricsPerServer(t *testing.T) {
	settingsSvc := newReportCollectorSettings(t)
	collector := newAgentReportCollector(&fakeReportServerProvider{}, nil, settingsSvc, nil, nil, nil)
	collector.SetMetricsFacility(&fakeMetricsFacility{reports: map[string]metricsfacility.ReportConfig{
		"srv-on":  {Enabled: true, IntervalSeconds: 120},
		"srv-off": {Enabled: false},
	}})
	ctx := context.Background()

	on := collector.reportStreamConfig(ctx, server.Server{ID: "srv-on"})
	if on.ServerID != "srv-on" || on.MetricsIntervalSeconds != 120 {
		t.Fatalf("enabled server config = %#v, want metrics interval 120", on)
	}
	if want := settingsSvc.Runtime().ContainerReportIntervalSeconds; on.ContainersIntervalSeconds != want {
		t.Fatalf("containers interval = %d, want runtime settings value %d", on.ContainersIntervalSeconds, want)
	}
	off := collector.reportStreamConfig(ctx, server.Server{ID: "srv-off"})
	if off.MetricsIntervalSeconds != 0 {
		t.Fatalf("disabled server must be told metrics interval 0, got %#v", off)
	}

	// 未注入设施（测试/降级）回退 enabled=true、默认 60s。
	fallback := newAgentReportCollector(&fakeReportServerProvider{}, nil, newReportCollectorSettings(t), nil, nil, nil)
	if got := fallback.reportStreamConfig(ctx, server.Server{ID: "srv-x"}); got.MetricsIntervalSeconds != defaultMetricsIntervalSeconds {
		t.Fatalf("missing facility fallback = %#v, want %ds", got, defaultMetricsIntervalSeconds)
	}
}

// TestAgentReportStreamConfigFacilityFailureDisablesMetrics covers FAC-MET-008:
// when the facility cannot be read the delivery path must degrade to disabled
// (only a log), never keep a stale collection interval.
func TestAgentReportStreamConfigFacilityFailureDisablesMetrics(t *testing.T) {
	collector := newAgentReportCollector(&fakeReportServerProvider{}, nil, newReportCollectorSettings(t), nil, nil, nil)
	collector.SetMetricsFacility(&fakeMetricsFacility{err: context.DeadlineExceeded})
	got := collector.reportStreamConfig(context.Background(), server.Server{ID: "srv-1"})
	if got.MetricsIntervalSeconds != 0 {
		t.Fatalf("facility read failure must disable metrics delivery, got %#v", got)
	}
}

// TestAgentReportHandleReportSkipsDisabledMetricsNode covers FAC-MET-002/004:
// samples from a node that is outside the selected collection scope are not
// persisted even if an old agent keeps reporting.
func TestAgentReportHandleReportSkipsDisabledMetricsNode(t *testing.T) {
	settingsSvc, metricsSvc, metricsDB := newReportCollectorMetrics(t)
	collector := newAgentReportCollector(&fakeReportServerProvider{}, nil, settingsSvc, metricsSvc, nil, nil)
	collector.SetMetricsFacility(&fakeMetricsFacility{reports: map[string]metricsfacility.ReportConfig{
		"srv-disabled": {Enabled: false},
	}})
	snapshot := linux.MetricsSnapshot{CPUUsagePercent: 42}
	if err := collector.handleReport(context.Background(), "srv-disabled", agentclient.AgentReport{
		SampleAt: time.Unix(120, 0).UTC(),
		Metrics:  &snapshot,
	}); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := metricsDB.QueryRow(`SELECT COUNT(*) FROM metrics_snapshots WHERE server_id='srv-disabled'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("disabled node samples must be skipped, got %d rows", count)
	}
}

// TestAgentReportHandleReportFacilityFailureFallsBackToDefault covers the save
// path failure semantics: a transient facility read failure stores with
// enabled=true and the default 60s interval instead of dropping the sample.
func TestAgentReportHandleReportFacilityFailureFallsBackToDefault(t *testing.T) {
	settingsSvc, metricsSvc, metricsDB := newReportCollectorMetrics(t)
	collector := newAgentReportCollector(&fakeReportServerProvider{}, nil, settingsSvc, metricsSvc, nil, nil)
	collector.SetMetricsFacility(&fakeMetricsFacility{err: context.DeadlineExceeded})
	snapshot := linux.MetricsSnapshot{CPUUsagePercent: 21}
	if err := collector.handleReport(context.Background(), "srv-1", agentclient.AgentReport{
		SampleAt: time.Unix(120, 0).UTC(),
		Metrics:  &snapshot,
	}); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := metricsDB.QueryRow(`SELECT COUNT(*) FROM metrics_snapshots WHERE server_id='srv-1'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("facility read failure must still store the sample, got %d rows", count)
	}
}

// TestAgentReportHandleReportUsesFacilityInterval covers FAC-MET-004: an enabled
// node's samples are aligned against the facility collection interval, not the
// removed runtime setting.
func TestAgentReportHandleReportUsesFacilityInterval(t *testing.T) {
	settingsSvc, metricsSvc, metricsDB := newReportCollectorMetrics(t)
	collector := newAgentReportCollector(&fakeReportServerProvider{}, nil, settingsSvc, metricsSvc, nil, nil)
	collector.SetMetricsFacility(&fakeMetricsFacility{reports: map[string]metricsfacility.ReportConfig{
		"srv-1": {Enabled: true, IntervalSeconds: 120},
	}})
	// 60s 是旧默认间隔、不是设施间隔；启用节点必须按 120s 对齐。
	unaligned := linux.MetricsSnapshot{CPUUsagePercent: 30}
	if err := collector.handleReport(context.Background(), "srv-1", agentclient.AgentReport{
		SampleAt: time.Unix(60, 0).UTC(),
		Metrics:  &unaligned,
	}); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := metricsDB.QueryRow(`SELECT COUNT(*) FROM metrics_snapshots WHERE server_id='srv-1'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("sample aligned to the old interval must be skipped, got %d rows", count)
	}
	aligned := linux.MetricsSnapshot{CPUUsagePercent: 31}
	if err := collector.handleReport(context.Background(), "srv-1", agentclient.AgentReport{
		SampleAt: time.Unix(120, 0).UTC(),
		Metrics:  &aligned,
	}); err != nil {
		t.Fatal(err)
	}
	if err := metricsDB.QueryRow(`SELECT COUNT(*) FROM metrics_snapshots WHERE server_id='srv-1'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("sample aligned to the facility interval must be stored, got %d rows", count)
	}
}

// TestAgentReportSilentTimeoutUsesMetricsFacility covers FAC-MET-004: the stream
// silence timeout is min(facility interval, container interval) with the 10s
// floor, and falls back to the 60s default when the facility is absent.
func TestAgentReportSilentTimeoutUsesMetricsFacility(t *testing.T) {
	cases := []struct {
		name      string
		interval  int
		withFac   bool
		wantDelay time.Duration
	}{
		{"facility interval dominates container", 120, true, 90 * time.Second},
		{"container interval dominates when smaller", 20, true, 60 * time.Second},
		{"floor keeps timeout at 10s", 1, true, 10 * time.Second},
		{"missing facility falls back to 60s", 0, false, 90 * time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			collector := newAgentReportCollector(&fakeReportServerProvider{}, nil, newReportCollectorSettings(t), nil, nil, nil)
			if tc.withFac {
				collector.SetMetricsFacility(&fakeMetricsFacility{interval: tc.interval})
			}
			if got := collector.silentTimeout(); got != tc.wantDelay {
				t.Fatalf("silentTimeout() = %s, want %s", got, tc.wantDelay)
			}
		})
	}
}

func newReportCollectorMetrics(t *testing.T) (*settings.Service, *metrics.Service, *sql.DB) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.LogDatabase = filepath.Join(dir, "log.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.CoordinationDatabase = filepath.Join(dir, "coordination.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	settingsSvc, err := settings.NewService(store.AppDB(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	return settingsSvc, metrics.NewService(store.MetricsDB(), nil), store.MetricsDB()
}
