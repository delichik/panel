package panel

import (
	"context"
	"strings"
	"sync"
	"time"

	agentclient "panel/internal/agent/client"
	agentcontract "panel/internal/agent/contract"
	"panel/internal/modules/containers"
	"panel/internal/modules/observability/metrics"
	"panel/internal/modules/runtimeevents"
	server "panel/internal/modules/servers"
	"panel/internal/modules/settings"
	"panel/internal/modules/tasks"
	"panel/internal/platform/linux"
	"panel/internal/platform/logging"

	"go.uber.org/zap"
)

type agentReportCollector struct {
	servers interface {
		List(context.Context) ([]server.Server, error)
	}
	reportStreams interface {
		RecordAgentReportStream(context.Context, string, bool, time.Time, string) error
	}
	client     *agentclient.GRPCClient
	settings   *settings.Service
	metrics    *metrics.Service
	containers *containerization.Service
	images     interface {
		SaveReportedImages(context.Context, string, []agentcontract.DockerImage) error
	}
	packages interface {
		SaveReportedUpdates(context.Context, string, []linux.PackageUpdate) error
	}
	logs    runtimeevents.EventWriter
	cancel  context.CancelFunc
	mu      sync.Mutex
	streams map[string]*agentReportStream
}

type agentReportStream struct {
	serverID      string
	serverName    string
	endpoint      string
	cancel        context.CancelFunc
	startedAt     time.Time
	lastMessageAt time.Time
	connected     bool
}

func newAgentReportCollector(serverSvc interface {
	List(context.Context) ([]server.Server, error)
}, client *agentclient.GRPCClient, settingsSvc *settings.Service, metricsSvc *metrics.Service, containerSvc *containerization.Service, packageSvc interface {
	SaveReportedUpdates(context.Context, string, []linux.PackageUpdate) error
}) *agentReportCollector {
	c := &agentReportCollector{
		servers:    serverSvc,
		client:     client,
		settings:   settingsSvc,
		metrics:    metricsSvc,
		containers: containerSvc,
		packages:   packageSvc,
		streams:    map[string]*agentReportStream{},
	}
	if recorder, ok := serverSvc.(interface {
		RecordAgentReportStream(context.Context, string, bool, time.Time, string) error
	}); ok {
		c.reportStreams = recorder
	}
	return c
}

func (c *agentReportCollector) Start(ctx context.Context) {
	if c == nil {
		return
	}
	runCtx, cancel := context.WithCancel(ctx)
	c.cancel = cancel
	go c.run(runCtx)
}

func (c *agentReportCollector) Stop() {
	if c == nil {
		return
	}
	if c.cancel != nil {
		c.cancel()
	}
	c.mu.Lock()
	for id, entry := range c.streams {
		entry.cancel()
		delete(c.streams, id)
	}
	c.mu.Unlock()
}

func (c *agentReportCollector) run(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		c.sync(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (c *agentReportCollector) sync(ctx context.Context) {
	servers, err := c.servers.List(ctx)
	if err != nil {
		logging.L().Warn("agent report sync failed", zap.Error(err))
		return
	}
	wanted := map[string]server.Server{}
	for _, srv := range servers {
		if !reportAgentReady(srv) {
			continue
		}
		wanted[srv.ID] = srv
		c.ensureStream(ctx, srv)
	}
	c.mu.Lock()
	for id, entry := range c.streams {
		if _, ok := wanted[id]; !ok {
			entry.cancel()
			delete(c.streams, id)
		}
	}
	c.mu.Unlock()
	c.auditSilentStreams()
}

func (c *agentReportCollector) ensureStream(ctx context.Context, srv server.Server) {
	endpoint := strings.TrimSpace(srv.Traits[agentcontract.TraitURL])
	c.mu.Lock()
	if current := c.streams[srv.ID]; current != nil {
		if current.endpoint == endpoint {
			c.mu.Unlock()
			return
		}
		current.cancel()
		delete(c.streams, srv.ID)
	}
	streamCtx, cancel := context.WithCancel(ctx)
	entry := &agentReportStream{serverID: srv.ID, serverName: strings.TrimSpace(srv.Name), endpoint: endpoint, cancel: cancel, startedAt: time.Now().UTC()}
	c.streams[srv.ID] = entry
	c.mu.Unlock()
	if c.client == nil {
		return
	}
	go func() {
		defer c.deleteEntryIfCurrent(entry)
		c.runServerStream(streamCtx, srv, entry)
	}()
}

func (c *agentReportCollector) auditSilentStreams() {
	now := time.Now().UTC()
	timeout := c.silentTimeout()
	type streamDisconnect struct {
		entry        *agentReportStream
		wasConnected bool
	}
	var disconnected []streamDisconnect
	c.mu.Lock()
	for id, entry := range c.streams {
		last := entry.lastMessageAt
		if last.IsZero() {
			last = entry.startedAt
		}
		if now.Sub(last) <= timeout {
			continue
		}
		entry.cancel()
		wasConnected := entry.connected
		entry.connected = false
		delete(c.streams, id)
		disconnected = append(disconnected, streamDisconnect{entry: entry, wasConnected: wasConnected})
	}
	c.mu.Unlock()
	for _, item := range disconnected {
		entry := item.entry
		msg := "agent report stream timed out after " + timeout.String()
		logging.L().Warn("agent report stream timed out", zap.String("server_id", entry.serverID), zap.Duration("timeout", timeout))
		c.recordReportStream(context.Background(), entry.serverID, false, entry.lastMessageAt, msg)
		if item.wasConnected {
			c.logStreamStatus(entry, false, msg)
		}
	}
}

func (c *agentReportCollector) silentTimeout() time.Duration {
	rt := c.settings.Runtime()
	interval := rt.MetricsCollectionIntervalSeconds
	if rt.ContainerReportIntervalSeconds > 0 && (interval <= 0 || rt.ContainerReportIntervalSeconds < interval) {
		interval = rt.ContainerReportIntervalSeconds
	}
	if interval < 1 {
		interval = 1
	}
	timeout := time.Duration(interval*3) * time.Second
	if timeout < 10*time.Second {
		return 10 * time.Second
	}
	return timeout
}

func (c *agentReportCollector) deleteEntryIfCurrent(entry *agentReportStream) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if current := c.streams[entry.serverID]; current == entry {
		delete(c.streams, entry.serverID)
	}
}

func (c *agentReportCollector) markConnected(entry *agentReportStream, at time.Time) {
	if at.IsZero() {
		at = time.Now().UTC()
	}
	at = at.UTC().Truncate(time.Second)
	wasConnected := false
	c.mu.Lock()
	if current := c.streams[entry.serverID]; current == entry {
		wasConnected = entry.connected
		entry.connected = true
		entry.lastMessageAt = at
	}
	c.mu.Unlock()
	c.recordReportStream(context.Background(), entry.serverID, true, at, "")
	if !wasConnected {
		c.logStreamStatus(entry, true, "")
	}
}

func (c *agentReportCollector) markDisconnected(entry *agentReportStream, msg string) {
	wasConnected := false
	last := entry.lastMessageAt
	c.mu.Lock()
	current := c.streams[entry.serverID]
	if current == entry {
		wasConnected = entry.connected
		entry.connected = false
	}
	c.mu.Unlock()
	if current != entry {
		return
	}
	c.recordReportStream(context.Background(), entry.serverID, false, last, msg)
	if wasConnected {
		c.logStreamStatus(entry, false, msg)
	}
}

// SetSystemLogs 安装系统日志写入器；Agent 连接/断开状态转换会写入系统日志。
func (c *agentReportCollector) SetSystemLogs(logs runtimeevents.EventWriter) {
	c.logs = logs
}

func (c *agentReportCollector) logStreamStatus(entry *agentReportStream, connected bool, msg string) {
	if c.logs == nil || entry == nil {
		return
	}
	eventType := runtimeevents.EventAgentConnected
	severity := runtimeevents.SeverityInfo
	summary := "Agent report stream connected: " + firstNonEmpty(entry.serverName, entry.serverID)
	if !connected {
		eventType = runtimeevents.EventAgentDisconnected
		severity = runtimeevents.SeverityWarning
		summary = "Agent report stream disconnected: " + firstNonEmpty(entry.serverName, entry.serverID)
		if strings.TrimSpace(msg) != "" {
			summary += ": " + strings.TrimSpace(msg)
		}
	}
	c.logs.Log(context.Background(), runtimeevents.WriteEventInput{
		EventType:    eventType,
		Category:     runtimeevents.CategorySystem,
		Severity:     severity,
		Source:       "agent-report",
		SourceModule: "agent",
		Summary:      summary,
		OccurredAt:   time.Now().UTC(),
	})
}

func (c *agentReportCollector) recordReportStream(ctx context.Context, serverID string, connected bool, lastMessageAt time.Time, msg string) {
	if c.reportStreams == nil {
		return
	}
	if err := c.reportStreams.RecordAgentReportStream(ctx, serverID, connected, lastMessageAt, msg); err != nil {
		logging.L().Warn("failed to record agent report stream status", zap.String("server_id", serverID), zap.Error(err))
	}
}

func (c *agentReportCollector) runServerStream(ctx context.Context, srv server.Server, entry *agentReportStream) {
	backoff := 5 * time.Second
	for ctx.Err() == nil {
		err := c.client.StreamReports(ctx, entry.endpoint, func() agentclient.ReportConfig {
			rt := c.settings.Runtime()
			return agentclient.ReportConfig{
				ServerID:                  srv.ID,
				MetricsIntervalSeconds:    rt.MetricsCollectionIntervalSeconds,
				ContainersIntervalSeconds: rt.ContainerReportIntervalSeconds,
			}
		}, func(ctx context.Context, report agentclient.AgentReport) error {
			c.markConnected(entry, report.SampleAt)
			return c.handleReport(ctx, srv.ID, report)
		})
		if ctx.Err() != nil {
			return
		}
		logging.L().Warn("agent report stream failed", zap.String("server_id", srv.ID), zap.Error(err))
		c.markDisconnected(entry, err.Error())
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < 5*time.Minute {
			backoff *= 2
		}
	}
}

func (c *agentReportCollector) saveReportedImages(ctx context.Context, serverID string, images []agentcontract.DockerImage) error {
	if c.images == nil {
		return nil
	}
	if err := c.images.SaveReportedImages(ctx, serverID, images); err != nil {
		logging.L().Warn("agent image report save failed", zap.String("server_id", serverID), zap.Error(err))
	}
	return nil
}

func (c *agentReportCollector) savePackageUpdates(ctx context.Context, serverID string, updates []linux.PackageUpdate) error {
	if c.packages == nil {
		return nil
	}
	if err := c.packages.SaveReportedUpdates(ctx, serverID, updates); err != nil {
		logging.L().Warn("agent package report save failed", zap.String("server_id", serverID), zap.Error(err))
	}
	return nil
}

func (c *agentReportCollector) handleReport(ctx context.Context, serverID string, report agentclient.AgentReport) error {
	if report.SampleAt.IsZero() {
		report.SampleAt = time.Now().UTC().Truncate(time.Second)
	}
	if len(report.PackageUpdates) > 0 {
		c.savePackageUpdates(ctx, serverID, report.PackageUpdates)
	}
	if len(report.Images) > 0 {
		c.saveReportedImages(ctx, serverID, report.Images)
	}
	rt := c.settings.Runtime()
	if report.Metrics != nil && sampleAligned(report.SampleAt, rt.MetricsCollectionIntervalSeconds) {
		if err := c.metrics.SaveReported(ctx, serverID, report.SampleAt, *report.Metrics); err != nil {
			return err
		}
	}
	if report.HasContainers && (sampleAligned(report.SampleAt, rt.ContainerReportIntervalSeconds) || report.Reason == "container_change") {
		if err := c.containers.SaveReportedContainers(ctx, serverID, report.SampleAt, report.Containers); err != nil {
			return err
		}
		_, _, err := c.containers.TriggerApplicationReconcile(context.Background(), tasks.PeriodicTrigger{
			Type: "agent_report", TriggerResourceType: "server", TriggerResourceID: serverID,
			Payload: containerization.ApplicationReconcileTrigger{ServerIDs: []string{serverID}, Reason: firstNonEmpty(report.Reason, "agent_report")},
		})
		return err
	}
	return nil
}

func reportAgentReady(srv server.Server) bool {
	return srv.Traits[agentcontract.TraitEnabled] == "true" &&
		strings.TrimSpace(srv.Traits[agentcontract.TraitURL]) != "" &&
		srv.Traits[agentcontract.TraitStatus] == agentcontract.StatusCompatible
}

func sampleAligned(t time.Time, intervalSeconds int) bool {
	if intervalSeconds < 1 {
		intervalSeconds = 1
	}
	return t.UTC().Unix()%int64(intervalSeconds) == 0
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
