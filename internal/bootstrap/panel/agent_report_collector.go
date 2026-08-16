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
	// streaming 表示当前是否正阻塞在 StreamReports 调用内（连接已建立或正在
	// 建立）；false 表示处于两次尝试之间的退避等待。auditSilentStreams 只回收
	// streaming 的静默连接，避免把正在退避重试的条目取消掉而重置退避。
	streaming bool
	// delivered 表示本次连接（自上次尝试起）是否收到过至少一条上报；用于
	// 重连退避判断"上一次连接是通的"，跨连接不保留。
	delivered bool
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
		images:     containerSvc,
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
		// 处于退避重试间的条目（streaming=false）由 runServerStream 自己负责
		// 重连节奏；回收它们只会把退避清零，让断线 Agent 永远按 5s 轮询。
		if !entry.streaming {
			continue
		}
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
		entry.delivered = true
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
	var wait time.Duration
	for ctx.Err() == nil {
		// 每次尝试开始时清零 delivered：退避重置只应依据"本次连接"是否收到
		// 过上报，跨连接保留 lastMessageAt 会让健康过一段时间的断线 Agent
		// 永远按 5s 重连（退避永不翻倍）。
		c.mu.Lock()
		entry.streaming = true
		entry.delivered = false
		c.mu.Unlock()
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
		c.mu.Lock()
		entry.streaming = false
		delivered := entry.delivered
		c.mu.Unlock()
		// 本次连接曾经收到过上报说明链路本身是通的，断流只是瞬时故障，重连
		// 退避必须重置回 5s；只有连续失败才逐次翻倍，否则一次偶发断流会让
		// 后续每次重连越等越久（5s→…→5min 封顶）。
		logging.L().Warn("agent report stream failed", zap.String("server_id", srv.ID), zap.Error(err))
		c.markDisconnected(entry, err.Error())
		wait, backoff = reconnectBackoff(delivered, backoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

// reconnectBackoff 计算断流后的重连等待与下一次累计值。本次连接曾收到上报
// （hadMessages=true）说明链路本身是通的，等待重置回初始 5s，下一次连续
// 失败从 10s 起继续翻倍；未收到上报时按当前累计值等待并继续翻倍（封顶
// 5 分钟）。
func reconnectBackoff(hadMessages bool, current time.Duration) (wait, next time.Duration) {
	if hadMessages {
		return 5 * time.Second, 10 * time.Second
	}
	if current < 5*time.Second {
		current = 5 * time.Second
	}
	wait = current
	next = wait * 2
	if next > 5*time.Minute {
		next = 5 * time.Minute
	}
	return wait, next
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
	if report.Metrics != nil {
		if sampleAligned(report.SampleAt, rt.MetricsCollectionIntervalSeconds) {
			if err := c.metrics.SaveReported(ctx, serverID, report.SampleAt, *report.Metrics); err != nil {
				// 与包/镜像推送路径一致：落库失败只记录日志，不中断上报流。
				logging.L().Warn("agent metrics report save failed", zap.String("server_id", serverID), zap.Error(err))
			}
		} else {
			// agent 按发送时的间隔计算整点、Panel 按收到时的间隔校验；
			// 设置项在上报在途时变更会让样本落在新间隔之外，这里记录日志便于排查，
			// 但同样不中断上报流。
			logging.L().Debug("agent metrics report skipped: sample not aligned to collection interval",
				zap.String("server_id", serverID),
				zap.Time("sample_at", report.SampleAt),
				zap.Int("interval_seconds", rt.MetricsCollectionIntervalSeconds))
		}
	}
	// 容器分支：仅当上报明确携带容器快照（非 nil）时才替换观察集合；
	// 未携带快照（nil）时保留既有观察，由 SaveReportedContainers 内部保障。
	if report.HasContainers && report.Containers != nil && (sampleAligned(report.SampleAt, rt.ContainerReportIntervalSeconds) || report.Reason == "container_change") {
		if err := c.containers.SaveReportedContainers(ctx, serverID, report.SampleAt, report.Containers); err != nil {
			// 与指标分支一致：容器观察落库失败只记录日志，不中断上报流，
			// 否则一次瞬时错误会让指标采集随整条流一起断掉并触发重连退避。
			logging.L().Warn("agent container report save failed", zap.String("server_id", serverID), zap.Error(err))
		} else {
			_, _, err := c.containers.TriggerApplicationReconcile(context.Background(), tasks.PeriodicTrigger{
				Type: "agent_report", TriggerResourceType: "server", TriggerResourceID: serverID,
				Payload: containerization.ApplicationReconcileTrigger{ServerIDs: []string{serverID}, Reason: firstNonEmpty(report.Reason, "agent_report")},
			})
			if err != nil {
				logging.L().Warn("agent report reconcile trigger failed", zap.String("server_id", serverID), zap.Error(err))
			}
		}
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
