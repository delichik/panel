package panel

import (
	"context"
	"strings"
	"sync"
	"time"

	agentclient "panel/internal/agent/client"
	agentcontract "panel/internal/agent/contract"
	"panel/internal/modules/containers"
	"panel/internal/modules/networkprobe"
	"panel/internal/modules/observability/metrics"
	"panel/internal/modules/observability/metricsfacility"
	"panel/internal/modules/runtimeevents"
	server "panel/internal/modules/servers"
	"panel/internal/modules/settings"
	"panel/internal/modules/tasks"
	"panel/internal/platform/linux"
	"panel/internal/platform/logging"
	"panel/internal/platform/maintenance"

	"go.uber.org/zap"
)

// defaultMetricsIntervalSeconds 是未注入指标设施（测试/降级）时的采集间隔回退值。
const defaultMetricsIntervalSeconds = 60

// networkProbeReporter 是网络探测设施在 Agent 上报链路中需要的最小接口。
type networkProbeReporter interface {
	ReportConfig(ctx context.Context, serverID string) (networkprobe.ProbeReport, error)
	SaveReported(ctx context.Context, sourceServerID string, sampleAt time.Time, results []networkprobe.ReportedResult) error
}

// metricsFacilityReporter 是指标设施在 Agent 上报链路中需要的最小接口。
type metricsFacilityReporter interface {
	ReportConfig(ctx context.Context, serverID string) (metricsfacility.ReportConfig, error)
	IntervalSeconds() int
}

// sshAccessReporter 是安全库 SSH 访问记录在 Agent 上报链路中的最小接口。
// 回填游标由安全库持久化，Panel 重启或 Agent 重连后继续从上次位置补。
type sshAccessReporter interface {
	BackfillSince(ctx context.Context, serverID string) (time.Time, error)
	ApplyLive(ctx context.Context, serverID string, snapshot agentcontract.SSHSnapshot) error
	ApplyHistory(ctx context.Context, serverID string, history agentcontract.SSHSessionHistory) error
	ApplyFailures(ctx context.Context, serverID string, failures agentcontract.SSHFailures) error
}

type agentReportCollector struct {
	writers maintenance.Gate
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
	probes networkProbeReporter
	// sshAccess 落库 SSH 会话/失败尝试；nil 表示未启用（测试或降级）。
	sshAccess sshAccessReporter
	// metricsFacility 决定每台服务器是否采集指标以及采集间隔；未注入时按
	// enabled=true、默认 60s 处理（仅测试/降级）。
	metricsFacility metricsFacilityReporter
	logs            runtimeevents.EventWriter
	cancel          context.CancelFunc
	mu              sync.Mutex
	streams         map[string]*agentReportStream
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

// SetSSHAccess 注入 SSH 访问记录仓储。未注入时上报中的 SSH 字段被忽略，
// 既不影响指标/容器链路，也保持既有测试的零值可用性。
func (c *agentReportCollector) SetSSHAccess(reporter sshAccessReporter) {
	if c == nil {
		return
	}
	c.sshAccess = reporter
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
	ctx = logging.With(ctx, logging.Context{StreamID: logging.NewStreamID(), Module: "agent-report"})
	servers, err := c.servers.List(ctx)
	if err != nil {
		logging.From(ctx).Warn("agent report sync failed", zap.Error(err))
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
		logging.From(logging.Background("agent-report")).Warn("agent report stream timed out", zap.String("server_id", entry.serverID), zap.Duration("timeout", timeout))
		c.recordReportStream(context.Background(), entry.serverID, false, entry.lastMessageAt, msg)
		if item.wasConnected {
			c.logStreamStatus(entry, false, msg)
		}
	}
}

func (c *agentReportCollector) silentTimeout() time.Duration {
	// 指标采集间隔来自指标设施；未注入或设施读取失败（IntervalSeconds 内部
	// 已回退默认 60）时使用默认值。容器上报间隔仍取 runtime settings。
	interval := defaultMetricsIntervalSeconds
	if c != nil && c.metricsFacility != nil {
		if configured := c.metricsFacility.IntervalSeconds(); configured > 0 {
			interval = configured
		}
	}
	if c != nil {
		if container := c.settings.Runtime().ContainerReportIntervalSeconds; container > 0 && container < interval {
			interval = container
		}
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
	done, ok := c.writers.Enter()
	if !ok {
		return
	}
	defer done()
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
	done, ok := c.writers.Enter()
	if !ok {
		return
	}
	defer done()
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

// SetNetworkProbe 注入网络探测设施；未注入时上报链路不探测也不落库探测结果。
func (c *agentReportCollector) SetNetworkProbe(probes networkProbeReporter) {
	if c == nil {
		return
	}
	c.probes = probes
}

// SetMetricsFacility 注入指标设施；未注入时按 enabled=true、默认 60s 处理
// （仅测试/降级）。设施配置按服务器决定是否采集指标与采集间隔。
func (c *agentReportCollector) SetMetricsFacility(facility metricsFacilityReporter) {
	if c == nil {
		return
	}
	c.metricsFacility = facility
}

// metricsTargeting 解析指定服务器当前的指标采集目标：是否启用以及采集间隔。
//
// failOpen 决定设施读取失败的语义：保存路径（handleReport）传 true，瞬时读库
// 错误时按 enabled=true、默认 60s 继续落库，避免丢指标；下发路径（report
// stream 配置闭包）传 false，读不到配置就下发 disabled，让 Agent 停止采集而不是
// 用过时间隔继续上报。两条路径都只记录日志，不中断上报流。
func (c *agentReportCollector) metricsTargeting(ctx context.Context, serverID string, failOpen bool) (bool, int) {
	if c == nil || c.metricsFacility == nil {
		return true, defaultMetricsIntervalSeconds
	}
	report, err := c.metricsFacility.ReportConfig(ctx, serverID)
	if err != nil {
		logging.From(ctx).Warn("agent metrics facility config lookup failed",
			zap.String("server_id", serverID),
			zap.Bool("fail_open", failOpen),
			zap.Error(err))
		if failOpen {
			return true, defaultMetricsIntervalSeconds
		}
		return false, 0
	}
	if !report.Enabled {
		return false, 0
	}
	interval := report.IntervalSeconds
	if interval < 1 {
		// 防御性：启用却拿到非法间隔时回落默认，保证整点对齐校验不会 panic/全跳过。
		interval = defaultMetricsIntervalSeconds
	}
	return true, interval
}

// reportStreamConfig 构造下发给单台服务器的 Agent 上报配置。指标采集间隔按该
// 服务器的指标设施归属计算：禁用节点下发 0（Agent 停止采集），启用节点下发设施
// 配置的间隔；容器上报间隔仍取 runtime settings，不受指标设施影响。
func (c *agentReportCollector) reportStreamConfig(ctx context.Context, srv server.Server) agentclient.ReportConfig {
	metricsEnabled, metricsInterval := c.metricsTargeting(ctx, srv.ID, false)
	if !metricsEnabled {
		metricsInterval = 0
	}
	return agentclient.ReportConfig{
		ServerID:                  srv.ID,
		MetricsIntervalSeconds:    metricsInterval,
		ContainersIntervalSeconds: c.settings.Runtime().ContainerReportIntervalSeconds,
		Probe:                     c.reportProbeConfig(ctx, srv.ID),
		SshSessionsSince:          c.sshBackfillSince(ctx, srv.ID),
	}
}

// sshBackfillSince 读取安全库游标；失败时返回 0，让 Agent 回退到 now-7d，
// 不让一次游标查询失败阻断整个上报流。
func (c *agentReportCollector) sshBackfillSince(ctx context.Context, serverID string) int64 {
	if c.sshAccess == nil {
		return 0
	}
	since, err := c.sshAccess.BackfillSince(ctx, serverID)
	if err != nil {
		logging.From(ctx).Warn("agent SSH backfill cursor read failed", zap.String("server_id", serverID), zap.Error(err))
		return 0
	}
	if since.IsZero() {
		return 0
	}
	return since.Unix()
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
		ResourceID: entry.serverID, ResourceName: entry.serverName,
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
		logCtx := ctx
		if _, ok := logging.Lookup(ctx); !ok {
			logCtx = logging.Background("agent-report")
		} else {
			logCtx = logging.With(ctx, logging.Context{Module: "agent-report"})
		}
		logging.From(logCtx).Warn("failed to record agent report stream status", zap.String("server_id", serverID), zap.Error(err))
	}
}

func (c *agentReportCollector) runServerStream(ctx context.Context, srv server.Server, entry *agentReportStream) {
	ctx = logging.With(ctx, logging.Context{StreamID: logging.NewStreamID(), Module: "agent-report"})
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
			return c.reportStreamConfig(ctx, srv)
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
		logging.From(ctx).Warn("agent report stream failed", zap.String("server_id", srv.ID), zap.Error(err))
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
		logging.From(ctx).Warn("agent image report save failed", zap.String("server_id", serverID), zap.Error(err))
	}
	return nil
}

func (c *agentReportCollector) savePackageUpdates(ctx context.Context, serverID string, updates []linux.PackageUpdate) error {
	if c.packages == nil {
		return nil
	}
	if err := c.packages.SaveReportedUpdates(ctx, serverID, updates); err != nil {
		logging.From(ctx).Warn("agent package report save failed", zap.String("server_id", serverID), zap.Error(err))
	}
	return nil
}

// reportProbeConfig 把网络探测设施下发的探测配置转换为 Agent 协议结构。
// 查询失败只记录日志并返回 disabled，不能因设施读库失败中断上报流。
func (c *agentReportCollector) reportProbeConfig(ctx context.Context, serverID string) agentclient.ProbeReportConfig {
	if c == nil || c.probes == nil {
		return agentclient.ProbeReportConfig{}
	}
	report, err := c.probes.ReportConfig(ctx, serverID)
	if err != nil {
		logging.From(ctx).Warn("agent probe config lookup failed", zap.String("server_id", serverID), zap.Error(err))
		return agentclient.ProbeReportConfig{}
	}
	out := agentclient.ProbeReportConfig{
		Enabled:            report.Enabled,
		IntervalSeconds:    report.IntervalSeconds,
		PingCount:          report.PingCount,
		PingTimeoutSeconds: report.PingTimeoutSeconds,
	}
	if len(report.Targets) == 0 {
		return out
	}
	out.Targets = make([]agentclient.ProbeTarget, 0, len(report.Targets))
	for _, target := range report.Targets {
		out.Targets = append(out.Targets, agentclient.ProbeTarget{
			ServerID: target.ServerID,
			Address:  target.Address,
			Name:     target.Name,
		})
	}
	return out
}

// saveProbeResults 保存一次探测上报。失败只记录日志，不中断上报流。
func (c *agentReportCollector) saveProbeResults(ctx context.Context, serverID string, sampleAt time.Time, results []agentclient.ProbeResult) {
	if c == nil || c.probes == nil {
		return
	}
	converted := make([]networkprobe.ReportedResult, 0, len(results))
	for _, result := range results {
		converted = append(converted, networkprobe.ReportedResult{
			TargetServerID:    result.TargetServerID,
			TargetAddress:     result.TargetAddress,
			LatencyMS:         result.LatencyMS,
			PacketLossPercent: result.PacketLossPercent,
			Sent:              result.Sent,
			Received:          result.Received,
			Error:             result.Error,
		})
	}
	if err := c.probes.SaveReported(ctx, serverID, sampleAt, converted); err != nil {
		logging.From(ctx).Warn("agent network probe report save failed", zap.String("server_id", serverID), zap.Error(err))
	}
}

func (c *agentReportCollector) handleReport(ctx context.Context, serverID string, report agentclient.AgentReport) error {
	done, ok := c.writers.Enter()
	if !ok {
		return nil
	}
	defer done()
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
		// 指标采集范围由指标设施按服务器决定：禁用节点即使旧 Agent 仍在上报，
		// 也必须跳过落库（历史数据保留），否则“停止采集”的配置不会生效。
		metricsEnabled, metricsInterval := c.metricsTargeting(ctx, serverID, true)
		switch {
		case !metricsEnabled:
			logging.From(ctx).Debug("agent metrics report skipped: server is not a metrics collection node",
				zap.String("server_id", serverID))
		case report.Replayed || sampleAligned(report.SampleAt, metricsInterval):
			// 补报重放样本可能落在当前采集间隔之外，必须跳过整点对齐校验；
			// metrics.Service.Save 已按 (server_id,time) 去重保证重放幂等。
			// 磁盘使用率改为 5 分钟采样：未刷新磁盘值的样本把磁盘字段清零，
			// 避免把旧值重复写成新的历史点。nil 表示旧 Agent，保持原值。
			snapshot := *report.Metrics
			if report.MetricsDiskUsageSampled != nil && !*report.MetricsDiskUsageSampled {
				snapshot.DiskUsedBytes = 0
				snapshot.DiskTotalBytes = 0
			}
			if err := c.metrics.SaveReported(ctx, serverID, report.SampleAt, snapshot); err != nil {
				// 与包/镜像推送路径一致：落库失败只记录日志，不中断上报流。
				logging.From(ctx).Warn("agent metrics report save failed", zap.String("server_id", serverID), zap.Error(err))
			}
		default:
			// agent 按发送时的间隔计算整点、Panel 按收到时的设施间隔校验；
			// 设施间隔在上报在途时变更会让样本落在新间隔之外，这里记录日志便于排查，
			// 但同样不中断上报流。
			logging.From(ctx).Debug("agent metrics report skipped: sample not aligned to collection interval",
				zap.String("server_id", serverID),
				zap.Time("sample_at", report.SampleAt),
				zap.Int("interval_seconds", metricsInterval))
		}
	}
	// 容器分支：仅当上报明确携带容器快照（非 nil）时才替换观察集合；
	// 未携带快照（nil）时保留既有观察，由 SaveReportedContainers 内部保障。
	if report.HasContainers && report.Containers != nil && (sampleAligned(report.SampleAt, rt.ContainerReportIntervalSeconds) || report.Reason == "container_change") {
		if err := c.containers.SaveReportedContainers(ctx, serverID, report.SampleAt, report.Containers); err != nil {
			// 与指标分支一致：容器观察落库失败只记录日志，不中断上报流，
			// 否则一次瞬时错误会让指标采集随整条流一起断掉并触发重连退避。
			logging.From(ctx).Warn("agent container report save failed", zap.String("server_id", serverID), zap.Error(err))
		} else {
			_, _, err := c.containers.TriggerApplicationReconcile(context.Background(), tasks.PeriodicTrigger{
				Type: "agent_report", TriggerResourceType: "server", TriggerResourceID: serverID,
				Payload: containerization.ApplicationReconcileTrigger{ServerIDs: []string{serverID}, Reason: firstNonEmpty(report.Reason, "agent_report")},
			})
			if err != nil {
				logging.From(ctx).Warn("agent report reconcile trigger failed", zap.String("server_id", serverID), zap.Error(err))
			}
		}
	}
	// 探测分支：只有 Agent 明确携带探测结果时才写入；旧 Agent 不上报，因此已有
	// 结果不会被空报告清空。
	if report.HasProbes {
		c.saveProbeResults(ctx, serverID, report.SampleAt, report.Probes)
	}
	if c.sshAccess != nil {
		if report.SSHSessions != nil {
			if err := c.sshAccess.ApplyLive(ctx, serverID, *report.SSHSessions); err != nil {
				logging.From(ctx).Warn("agent SSH session report save failed", zap.String("server_id", serverID), zap.Error(err))
			}
		}
		if report.SSHHistory != nil {
			if err := c.sshAccess.ApplyHistory(ctx, serverID, *report.SSHHistory); err != nil {
				logging.From(ctx).Warn("agent SSH history report save failed", zap.String("server_id", serverID), zap.Error(err))
			}
		}
		if report.SSHFailures != nil {
			if err := c.sshAccess.ApplyFailures(ctx, serverID, *report.SSHFailures); err != nil {
				logging.From(ctx).Warn("agent SSH failure report save failed", zap.String("server_id", serverID), zap.Error(err))
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
