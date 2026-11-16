package rpc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	agentcontract "panel/internal/agent/contract"
	agentdocker "panel/internal/agent/docker"
	agentpb "panel/internal/agent/pb"
	agentprobe "panel/internal/agent/probe"
	"panel/internal/platform/linux"
	"panel/internal/platform/logging"

	"go.uber.org/zap"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	reportCollectionTimeout = 30 * time.Second
	// diskUsageSampleInterval 是磁盘使用率的采样间隔。根文件系统使用率变化
	// 缓慢，若每个指标样本都 statfs，Panel 的磁盘历史序列会被重复值填满；
	// 降为 5 分钟一次后，只有重新采样的样本携带磁盘值。
	diskUsageSampleInterval = 5 * time.Minute
	// defaultProbeInterval 是探测配置缺失 interval 时的兜底周期，与 Panel
	// 下发的 probeIntervalSeconds 一致。
	defaultProbeInterval = 60 * time.Second
	// metricsSpoolCapacity/metricsSpoolMaxAge 限制弱网补报缓冲：只保留最近的
	// 少量指标样本且不超过 10 分钟，避免长时间离线后重放过于陈旧的数据。
	metricsSpoolCapacity = 12
	metricsSpoolMaxAge   = 10 * time.Minute
)

type reportConfig struct {
	serverID          string
	metricsInterval   time.Duration
	containerInterval time.Duration
	// probe 是 Panel 下发的网络探测任务；probeSet 区分“消息未携带 probe”与
	// “显式关闭/清空目标”，只有携带时才整体替换当前配置。
	probe    probeConfig
	probeSet bool
}

// probeConfig 是当前生效的网络探测任务快照。
type probeConfig struct {
	enabled  bool
	interval time.Duration
	targets  []agentprobe.Target
}

// reportCollector is the subset of agentsystem.LocalCollector used by the
// report hub; it exists so tests can inject failing collectors. 每个指标有
// 独立的采样协程，上报循环只读取它们写入的内存缓存。
type reportCollector interface {
	CPUUsage(ctx context.Context) (float64, error)
	MemoryStats(ctx context.Context) (total, used int64, err error)
	DiskUsage(ctx context.Context) (total, used int64, err error)
	DiskIOTotals(ctx context.Context) (readBytes, writeBytes int64, err error)
	NetworkTotals(ctx context.Context) (rx, tx int64, err error)
	SystemStatus(ctx context.Context) (linux.SystemStatus, error)
	PackageUpdates(ctx context.Context) ([]linux.PackageUpdate, error)
	TailscaleStatus(ctx context.Context) (agentcontract.TailscaleStatus, error)
}

type reportHub struct {
	collector reportCollector
	runtime   *agentdocker.LocalRuntime
	metrics   *metricCache

	mu                sync.Mutex
	watchers          map[int]*reportWatcher
	nextWatcherID     int
	running           bool
	wake              chan struct{}
	lastContainerHash string
	collectMu         sync.Mutex
	// probe/probeWake 保存当前探测任务并唤醒探测采样协程；probeRunner 允许
	// 测试注入 ping 替身，nil 表示使用系统 ping。
	probe       probeConfig
	probeWake   chan struct{}
	probeRunner agentprobe.Runner
	// spoolMu/spool 是弱网补报缓冲：stream.Send 失败时收集的指标样本，重连后
	// 按样本时间升序补发并标记 replayed。
	spoolMu sync.Mutex
	spool   []*agentpb.AgentReport
}

type reportWatcher struct {
	id  int
	cfg reportConfig
	ch  chan *agentpb.AgentReport
}

type reportWatcherSnapshot struct {
	id  int
	cfg reportConfig
	ch  chan *agentpb.AgentReport
}

// metricCache 保存各指标采样协程产出的最新值。上报循环只读取这份缓存并
// 在整点统一提交，因此单次采集耗时不会拖慢或跳过上报表的整点调度。
// 每个指标同时记录最近一次成功写入时间：某个指标持续采样失败超过
// metricFreshnessWindow 后，snapshot 不再返回该缓存，避免冻结的旧值随
// 不断前进的整点时间伪装成新样本落库。
//
// 网络部分保存的是自上次提交以来的字节增量（累加器）而非速率：/proc/net/dev
// 是累计计数器，采样协程每个节拍读一次并把相邻读数的差累加进来。上报循环在
// 整点消费累加值并除以读数窗口的实际时长，因此采集间隔是 5 秒时上报的就是
// 这 5 秒产生的流量平均值，而不是某个 1 秒瞬时样本。读数中途的计数器回退
// （网卡重建、回绕）按 0 增量跳过；窗口边界取实际读数时间，连续两次消费
// 之间没有新读数时窗口时长为 0，本次不产出指标。
const metricFreshnessWindow = 15 * time.Second

type metricCache struct {
	mu sync.RWMutex

	cpuUsagePercent    float64
	cpuUpdatedAt       time.Time
	hasCPU             bool
	memoryUsedBytes    int64
	memoryTotalBytes   int64
	memoryUpdatedAt    time.Time
	hasMemory          bool
	diskUsedBytes      int64
	diskTotalBytes     int64
	diskUpdatedAt      time.Time
	hasDisk            bool
	diskSampledPending bool
	networkLastRx      int64
	networkLastTx      int64
	networkWindowStart time.Time
	networkAccRx       int64
	networkAccTx       int64
	networkUpdatedAt   time.Time
	hasNetworkRaw      bool
	hasNetwork         bool
	// 磁盘 IO 与网络同构：保存相邻读数之间的扇区差（累加器），上报时按读数
	// 窗口折算速率。diskIOAvailable 只在本轮确实有可消费读数时为真，读不到
	// /proc/diskstats 时保持 false 且不阻塞其它指标。
	diskIOLastRead    int64
	diskIOLastWrite   int64
	diskIOUpdatedAt   time.Time
	diskIOWindowStart time.Time
	diskIOAccRead     int64
	diskIOAccWrite    int64
	hasDiskIORaw      bool
	diskIOAvailable   bool
	status            linux.SystemStatus
	statusUpdatedAt   time.Time
	hasStatus         bool
}

func (c *metricCache) snapshot() (linux.MetricsSnapshot, bool) {
	return c.snapshotAt(time.Now().UTC())
}

func (c *metricCache) snapshotAt(now time.Time) (linux.MetricsSnapshot, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.hasCPU || !c.hasMemory || !c.hasDisk || !c.hasNetwork || !c.hasStatus {
		return linux.MetricsSnapshot{}, false
	}
	staleBefore := now.Add(-metricFreshnessWindow)
	if c.cpuUpdatedAt.Before(staleBefore) || c.memoryUpdatedAt.Before(staleBefore) ||
		c.networkUpdatedAt.Before(staleBefore) || c.statusUpdatedAt.Before(staleBefore) {
		return linux.MetricsSnapshot{}, false
	}
	rxRate, txRate, ok := c.consumeNetworkLocked()
	if !ok {
		return linux.MetricsSnapshot{}, false
	}
	snapshot := linux.MetricsSnapshot{
		CPUUsagePercent:    c.cpuUsagePercent,
		MemoryUsedBytes:    c.memoryUsedBytes,
		MemoryTotalBytes:   c.memoryTotalBytes,
		DiskUsedBytes:      c.diskUsedBytes,
		DiskTotalBytes:     c.diskTotalBytes,
		NetworkRxBytesRate: rxRate,
		NetworkTxBytesRate: txRate,
		Status:             c.status,
	}
	// 磁盘使用率按 5 分钟节拍采样，不参与上面的 15 秒新鲜度门槛：首次成功
	// 采样后一直复用最近值，只有本轮确实重新采样的样本才携带磁盘值。
	snapshot.DiskUsageSampled = c.diskSampledPending
	c.diskSampledPending = false
	// 磁盘 IO 是可选能力，不能加入全量新鲜度门槛，否则平台不支持
	// /proc/diskstats 时所有指标都会停摆；不可用时只报告“未可用”。
	if c.diskIOAvailable && c.diskIOUpdatedAt.After(staleBefore) {
		if readRate, writeRate, ok := c.consumeDiskIOLocked(); ok {
			snapshot.DiskIOAvailable = true
			snapshot.DiskReadBytesRate = readRate
			snapshot.DiskWriteBytesRate = writeRate
		}
	}
	return snapshot, true
}

func (c *metricCache) setCPU(value float64) {
	c.mu.Lock()
	c.cpuUsagePercent = value
	c.cpuUpdatedAt = time.Now().UTC()
	c.hasCPU = true
	c.mu.Unlock()
}

func (c *metricCache) setMemory(total, used int64) {
	c.mu.Lock()
	c.memoryTotalBytes = total
	c.memoryUsedBytes = used
	c.memoryUpdatedAt = time.Now().UTC()
	c.hasMemory = true
	c.mu.Unlock()
}

func (c *metricCache) setDisk(total, used int64) {
	c.mu.Lock()
	c.diskTotalBytes = total
	c.diskUsedBytes = used
	c.diskUpdatedAt = time.Now().UTC()
	c.hasDisk = true
	c.diskSampledPending = true
	c.mu.Unlock()
}

func (c *metricCache) accumulateNetwork(rx, tx int64) {
	c.accumulateNetworkAt(rx, tx, time.Now().UTC())
}

func (c *metricCache) accumulateNetworkAt(rx, tx int64, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.hasNetworkRaw {
		c.networkAccRx += maxInt64(0, rx-c.networkLastRx)
		c.networkAccTx += maxInt64(0, tx-c.networkLastTx)
		c.hasNetwork = true
	} else {
		c.networkWindowStart = now
	}
	c.networkLastRx = rx
	c.networkLastTx = tx
	c.networkUpdatedAt = now
	c.hasNetworkRaw = true
}

// consumeNetworkLocked 把累加器折算成读数窗口内的平均速率并重置窗口。
// 窗口时长取最近与最早读数时间的差（networkUpdatedAt 即最近一次成功读数），
// 因此没有新读数时窗口时长为 0，本次不产出指标，上报循环保持 metrics 为
// nil 让 Panel 沿用旧值。
func (c *metricCache) consumeNetworkLocked() (rxRate, txRate float64, ok bool) {
	if !c.hasNetwork {
		return 0, 0, false
	}
	elapsed := c.networkUpdatedAt.Sub(c.networkWindowStart).Seconds()
	if elapsed <= 0 {
		return 0, 0, false
	}
	rxRate = float64(c.networkAccRx) / elapsed
	txRate = float64(c.networkAccTx) / elapsed
	c.networkWindowStart = c.networkUpdatedAt
	c.networkAccRx = 0
	c.networkAccTx = 0
	return rxRate, txRate, true
}

func (c *metricCache) accumulateDiskIO(readBytes, writeBytes int64) {
	c.accumulateDiskIOAt(readBytes, writeBytes, time.Now().UTC())
}

// accumulateDiskIOAt 与 accumulateNetworkAt 同语义：/proc/diskstats 是累计
// 计数器，这里累加相邻读数的差；计数器回退（设备重建/热插拔）按 0 增量跳过。
func (c *metricCache) accumulateDiskIOAt(readBytes, writeBytes int64, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.hasDiskIORaw {
		c.diskIOAccRead += maxInt64(0, readBytes-c.diskIOLastRead)
		c.diskIOAccWrite += maxInt64(0, writeBytes-c.diskIOLastWrite)
		c.diskIOAvailable = true
	} else {
		c.diskIOWindowStart = now
	}
	c.diskIOLastRead = readBytes
	c.diskIOLastWrite = writeBytes
	c.diskIOUpdatedAt = now
	c.hasDiskIORaw = true
}

// clearDiskIO 在 /proc/diskstats 读取失败时清空 IO 组：保持 DiskIOAvailable
// 为 false 并丢弃旧累加器，下一轮成功读数重新建立基线，避免把损坏的差值
// 当成速率上报，也避免 IO 采样失败拖停其它指标。
func (c *metricCache) clearDiskIO() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.diskIOLastRead = 0
	c.diskIOLastWrite = 0
	c.diskIOUpdatedAt = time.Time{}
	c.diskIOWindowStart = time.Time{}
	c.diskIOAccRead = 0
	c.diskIOAccWrite = 0
	c.hasDiskIORaw = false
	c.diskIOAvailable = false
}

// consumeDiskIOLocked 把磁盘 IO 累加器折算成读数窗口内的平均速率并重置窗口。
func (c *metricCache) consumeDiskIOLocked() (readRate, writeRate float64, ok bool) {
	if !c.diskIOAvailable {
		return 0, 0, false
	}
	elapsed := c.diskIOUpdatedAt.Sub(c.diskIOWindowStart).Seconds()
	if elapsed <= 0 {
		return 0, 0, false
	}
	readRate = float64(c.diskIOAccRead) / elapsed
	writeRate = float64(c.diskIOAccWrite) / elapsed
	c.diskIOWindowStart = c.diskIOUpdatedAt
	c.diskIOAccRead = 0
	c.diskIOAccWrite = 0
	return readRate, writeRate, true
}

func (c *metricCache) setStatus(status linux.SystemStatus) {
	c.mu.Lock()
	c.status = status
	c.statusUpdatedAt = time.Now().UTC()
	c.hasStatus = true
	c.mu.Unlock()
}

func newReportHub(collector reportCollector, runtime *agentdocker.LocalRuntime) *reportHub {
	return &reportHub{
		collector: collector,
		runtime:   runtime,
		metrics:   &metricCache{},
		watchers:  map[int]*reportWatcher{},
		wake:      make(chan struct{}, 1),
		probeWake: make(chan struct{}, 1),
	}
}

func (h *Handler) Report(stream agentpb.AgentReportService_ReportServer) error {
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	watcher := h.reports.add(normalizeReportConfig(first))
	defer h.reports.remove(watcher.id)

	// 新连接建立后先补发上次弱网期间缓冲的指标样本；补发失败时把剩余样本
	// 放回缓冲，等下一次重连继续。
	if err := h.reports.replaySpool(stream); err != nil {
		return err
	}

	errCh := make(chan error, 1)
	go func() {
		for {
			msg, err := stream.Recv()
			if err != nil {
				errCh <- err
				return
			}
			h.reports.update(watcher.id, normalizeReportConfig(msg))
		}
	}()
	for {
		select {
		case <-stream.Context().Done():
			return stream.Context().Err()
		case err := <-errCh:
			return err
		case report := <-watcher.ch:
			if err := stream.Send(report); err != nil {
				// 先把 watcher 摘掉，避免补报收集期间又有新报告写入队列。
				h.reports.remove(watcher.id)
				h.reports.spoolUnsent(watcher, report)
				return err
			}
		}
	}
}

// replaySpool 按样本时间升序补发缓冲的指标报告并标记 replayed，让 Panel
// 跳过整点对齐校验、按 (server_id, sample_at) 幂等落库。
func (h *reportHub) replaySpool(stream agentpb.AgentReportService_ReportServer) error {
	items := h.takeSpool()
	for index, report := range items {
		report.Replayed = true
		if err := stream.Send(report); err != nil {
			h.restoreSpool(items[index:])
			return err
		}
	}
	return nil
}

// spoolUnsent 在 stream.Send 失败时收集本次失败报告以及该 watcher 待发队列
// 中尚未发送的报告，只保留携带指标样本的条目，放入有界内存缓冲。
func (h *reportHub) spoolUnsent(watcher *reportWatcher, failed *agentpb.AgentReport) {
	collected := make([]*agentpb.AgentReport, 0, metricsSpoolCapacity)
	if failed != nil {
		collected = append(collected, failed)
	}
drain:
	for {
		select {
		case report := <-watcher.ch:
			if report != nil {
				collected = append(collected, report)
			}
		default:
			break drain
		}
	}
	now := time.Now().UTC()
	kept := make([]*agentpb.AgentReport, 0, len(collected))
	for _, report := range collected {
		if report == nil || report.GetMetrics() == nil {
			continue
		}
		if !reportSampleFresh(report, now) {
			continue
		}
		// 补报只重放指标样本：同一报告里可能夹带容器/Tailscale 等观测，
		// 重放陈旧观测会覆盖 Panel 更新的状态，因此这里裁剪成纯指标报告。
		kept = append(kept, &agentpb.AgentReport{
			SampleAt: report.SampleAt,
			Reason:   report.Reason,
			Metrics:  report.Metrics,
		})
	}
	if len(kept) == 0 {
		return
	}
	h.spoolMu.Lock()
	defer h.spoolMu.Unlock()
	h.spool = append(h.spool, kept...)
	if len(h.spool) > metricsSpoolCapacity {
		h.spool = h.spool[len(h.spool)-metricsSpoolCapacity:]
	}
}

// takeSpool 取走并清空缓冲，返回按样本时间升序的报告；超龄样本直接丢弃。
func (h *reportHub) takeSpool() []*agentpb.AgentReport {
	h.spoolMu.Lock()
	defer h.spoolMu.Unlock()
	if len(h.spool) == 0 {
		return nil
	}
	now := time.Now().UTC()
	items := make([]*agentpb.AgentReport, 0, len(h.spool))
	for _, report := range h.spool {
		if report == nil || !reportSampleFresh(report, now) {
			continue
		}
		items = append(items, report)
	}
	h.spool = nil
	sort.SliceStable(items, func(i, j int) bool {
		return reportSampleAt(items[i]).Before(reportSampleAt(items[j]))
	})
	return items
}

// restoreSpool 把补发失败的剩余样本放回缓冲；它们比补发期间新失败的样本
// 更旧，因此放在队首并保持原有升序。
func (h *reportHub) restoreSpool(items []*agentpb.AgentReport) {
	if len(items) == 0 {
		return
	}
	h.spoolMu.Lock()
	defer h.spoolMu.Unlock()
	merged := make([]*agentpb.AgentReport, 0, len(items)+len(h.spool))
	merged = append(merged, items...)
	merged = append(merged, h.spool...)
	if len(merged) > metricsSpoolCapacity {
		merged = merged[len(merged)-metricsSpoolCapacity:]
	}
	h.spool = merged
}

func reportSampleFresh(report *agentpb.AgentReport, now time.Time) bool {
	return now.Sub(reportSampleAt(report)) <= metricsSpoolMaxAge
}

func reportSampleAt(report *agentpb.AgentReport) time.Time {
	if report == nil || report.GetSampleAt() == nil {
		return time.Time{}
	}
	return report.GetSampleAt().AsTime()
}

func (h *reportHub) add(cfg reportConfig) *reportWatcher {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.nextWatcherID++
	watcher := &reportWatcher{id: h.nextWatcherID, cfg: cfg, ch: make(chan *agentpb.AgentReport, 16)}
	h.watchers[watcher.id] = watcher
	// 新连接的首个控制消息决定探测任务：旧 Panel 不携带 probe 时这里清空，
	// 避免上一个连接遗留的探测配置继续工作。
	h.probe = cfg.probe
	if cfg.probeSet {
		h.notifyProbeLocked()
	}
	if !h.running {
		h.running = true
		go h.run()
	}
	h.notifyLocked()
	return watcher
}

func (h *reportHub) update(id int, cfg reportConfig) {
	h.mu.Lock()
	defer h.mu.Unlock()
	watcher, ok := h.watchers[id]
	if !ok {
		return
	}
	if cfg.serverID != "" {
		watcher.cfg.serverID = cfg.serverID
	}
	if cfg.metricsInterval > 0 {
		watcher.cfg.metricsInterval = cfg.metricsInterval
	}
	if cfg.containerInterval > 0 {
		watcher.cfg.containerInterval = cfg.containerInterval
	}
	// 探测配置整体替换：允许 Panel 显式关闭探测或清空目标列表。
	if cfg.probeSet {
		watcher.cfg.probe = cfg.probe
		watcher.cfg.probeSet = true
		h.probe = cfg.probe
		h.notifyProbeLocked()
	}
	h.notifyLocked()
}

func (h *reportHub) remove(id int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.watchers, id)
	h.notifyLocked()
}

func (h *reportHub) isRunning() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.running
}

func (h *reportHub) notifyLocked() {
	select {
	case h.wake <- struct{}{}:
	default:
	}
}

// notifyProbeLocked 唤醒探测采样协程立即重新读取配置（不阻塞调用方）。
func (h *reportHub) notifyProbeLocked() {
	select {
	case h.probeWake <- struct{}{}:
	default:
	}
}

func (h *reportHub) probeConfigSnapshot() probeConfig {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.probe
}

func (h *reportHub) run() {
	eventCtx, stopEvents := context.WithCancel(context.Background())
	defer stopEvents()
	go h.watchContainerEvents(eventCtx)
	go h.watchImageEvents(eventCtx)
	go h.watchPackageEvents(eventCtx)
	go h.runMetricsSamplers(eventCtx)
	go h.runProbeSampler(eventCtx)
	for {
		watchers := h.snapshot()
		if len(watchers) == 0 {
			if h.tryStop() {
				return
			}
			continue
		}
		dueAt, ok := nextReportDue(time.Now().UTC(), watchers)
		if !ok {
			h.waitForChange()
			continue
		}
		wait := dueAt.Sub(time.Now().UTC())
		if wait < 0 {
			wait = 0
		}
		if !h.waitUntilDue(wait) {
			continue
		}
		h.collectAndBroadcast(dueAt, false, "")
	}
}

// runMetricsSamplers 为每个指标启动独立的采样协程，采样结果只写入内存缓存；
// 上报循环是唯一的提交方，在整点从缓存读取后统一上报。
func (h *reportHub) runMetricsSamplers(ctx context.Context) {
	var wg sync.WaitGroup
	wg.Add(6)
	go func() { defer wg.Done(); h.sampleCPU(ctx) }()
	go func() { defer wg.Done(); h.sampleMemory(ctx) }()
	go func() { defer wg.Done(); h.sampleDisk(ctx) }()
	go func() { defer wg.Done(); h.sampleDiskIO(ctx) }()
	go func() { defer wg.Done(); h.sampleNetwork(ctx) }()
	go func() { defer wg.Done(); h.sampleStatus(ctx) }()
	wg.Wait()
}

func (h *reportHub) sampleCPU(ctx context.Context) {
	ctx = logging.With(ctx, logging.Context{StreamID: logging.NewStreamID(), Module: "agent"})
	for {
		value, err := h.collector.CPUUsage(ctx)
		if err == nil {
			h.metrics.setCPU(value)
		} else if ctx.Err() == nil {
			logging.From(ctx).Warn("agent CPU sampling failed", zap.Error(err))
		}
		if !waitSamplerTick(ctx) {
			return
		}
	}
}

func (h *reportHub) sampleMemory(ctx context.Context) {
	ctx = logging.With(ctx, logging.Context{StreamID: logging.NewStreamID(), Module: "agent"})
	for {
		total, used, err := h.collector.MemoryStats(ctx)
		if err == nil {
			h.metrics.setMemory(total, used)
		} else if ctx.Err() == nil {
			logging.From(ctx).Warn("agent memory sampling failed", zap.Error(err))
		}
		if !waitSamplerTick(ctx) {
			return
		}
	}
}

func (h *reportHub) sampleDisk(ctx context.Context) {
	ctx = logging.With(ctx, logging.Context{StreamID: logging.NewStreamID(), Module: "agent"})
	for {
		total, used, err := h.collector.DiskUsage(ctx)
		if err == nil {
			h.metrics.setDisk(total, used)
		} else if ctx.Err() == nil {
			logging.From(ctx).Warn("agent disk sampling failed", zap.Error(err))
		}
		if !waitSamplerInterval(ctx, diskUsageSampleInterval) {
			return
		}
	}
}

// sampleDiskIO 每秒读取一次 /proc/diskstats 累计扇区数并累加差值。读取失败
// （平台不支持或文件不可读）时清空 IO 组并保持未可用，不阻塞其它指标。
func (h *reportHub) sampleDiskIO(ctx context.Context) {
	ctx = logging.With(ctx, logging.Context{StreamID: logging.NewStreamID(), Module: "agent"})
	var lastWarn time.Time
	for {
		readBytes, writeBytes, err := h.collector.DiskIOTotals(ctx)
		if err == nil {
			h.metrics.accumulateDiskIO(readBytes, writeBytes)
			lastWarn = time.Time{}
		} else {
			h.metrics.clearDiskIO()
			if ctx.Err() == nil && (lastWarn.IsZero() || time.Since(lastWarn) >= 5*time.Minute) {
				lastWarn = time.Now()
				logging.From(ctx).Warn("agent disk IO sampling unavailable", zap.Error(err))
			}
		}
		if !waitSamplerTick(ctx) {
			return
		}
	}
}

func (h *reportHub) sampleNetwork(ctx context.Context) {
	ctx = logging.With(ctx, logging.Context{StreamID: logging.NewStreamID(), Module: "agent"})
	for {
		rx, tx, err := h.collector.NetworkTotals(ctx)
		if err == nil {
			h.metrics.accumulateNetwork(rx, tx)
		} else if ctx.Err() == nil {
			logging.From(ctx).Warn("agent network sampling failed", zap.Error(err))
		}
		if !waitSamplerTick(ctx) {
			return
		}
	}
}

func (h *reportHub) sampleStatus(ctx context.Context) {
	ctx = logging.With(ctx, logging.Context{StreamID: logging.NewStreamID(), Module: "agent"})
	for {
		status, err := h.collector.SystemStatus(ctx)
		if err == nil {
			h.metrics.setStatus(status)
		} else if ctx.Err() == nil {
			logging.From(ctx).Warn("agent status sampling failed", zap.Error(err))
		}
		if !waitSamplerTick(ctx) {
			return
		}
	}
}

// waitSamplerTick 是采样协程的固定节拍：一次采集完成后等待 1 秒再进行下一次。
func waitSamplerTick(ctx context.Context) bool {
	return waitSamplerInterval(ctx, time.Second)
}

func waitSamplerInterval(ctx context.Context, interval time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(interval):
		return true
	}
}

// tryStop atomically marks the hub stopped only when no watchers remain. It
// closes the race where the last watcher is removed while a new watcher is
// added: the new watcher either finds the hub still running or starts a new
// run loop, so it can never be left without a scheduler.
func (h *reportHub) tryStop() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.watchers) > 0 {
		return false
	}
	h.running = false
	return true
}

func (h *reportHub) waitForChange() {
	<-h.wake
}

func (h *reportHub) waitUntilDue(wait time.Duration) bool {
	if wait <= 0 {
		select {
		case <-h.wake:
		default:
		}
		return true
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-h.wake:
		return false
	}
}

func (h *reportHub) snapshot() []reportWatcherSnapshot {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]reportWatcherSnapshot, 0, len(h.watchers))
	for _, watcher := range h.watchers {
		out = append(out, reportWatcherSnapshot{id: watcher.id, cfg: watcher.cfg, ch: watcher.ch})
	}
	return out
}

func (h *reportHub) watchContainerEvents(ctx context.Context) {
	ctx = logging.With(ctx, logging.Context{StreamID: logging.NewStreamID(), Module: "agent"})
	if h.runtime == nil {
		return
	}
	backoff := time.Second
	for ctx.Err() == nil {
		events, errs := h.runtime.ContainerEvents(ctx)
		for events != nil || errs != nil {
			select {
			case <-ctx.Done():
				return
			case _, ok := <-events:
				if !ok {
					events = nil
					continue
				}
				h.collectAndBroadcast(time.Now().UTC().Truncate(time.Second), true, "container_change")
			case err, ok := <-errs:
				if !ok {
					errs = nil
					continue
				}
				if err != nil && ctx.Err() == nil {
					logging.From(ctx).Warn("agent report container event watch failed", zap.Error(err))
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

const (
	imagePushMinInterval   = 30 * time.Second
	packagePushMinInterval = 10 * time.Minute
)

func (h *reportHub) watchImageEvents(ctx context.Context) {
	ctx = logging.With(ctx, logging.Context{StreamID: logging.NewStreamID(), Module: "agent"})
	if h.runtime == nil {
		return
	}
	backoff := time.Second
	var lastPush time.Time
	for ctx.Err() == nil {
		events, errs := h.runtime.ImageEvents(ctx)
		for events != nil || errs != nil {
			select {
			case <-ctx.Done():
				return
			case _, ok := <-events:
				if !ok {
					events = nil
					continue
				}
				if time.Since(lastPush) < imagePushMinInterval {
					continue
				}
				lastPush = time.Now()
				h.pushImages(ctx, "image_change")
			case err, ok := <-errs:
				if !ok {
					errs = nil
					continue
				}
				if err != nil && ctx.Err() == nil {
					logging.From(ctx).Warn("agent report image event watch failed", zap.Error(err))
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

const packageStatusPath = "/var/lib/dpkg/status"

func (h *reportHub) watchPackageEvents(ctx context.Context) {
	var lastMod time.Time
	var lastPush time.Time
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		info, err := os.Stat(packageStatusPath)
		if err != nil {
			continue
		}
		mod := info.ModTime().UTC().Truncate(time.Second)
		if lastMod.IsZero() {
			lastMod = mod
			continue
		}
		if !mod.After(lastMod) {
			continue
		}
		lastMod = mod
		if time.Since(lastPush) < packagePushMinInterval {
			continue
		}
		lastPush = time.Now()
		h.pushPackageUpdates(ctx)
	}
}

func (h *reportHub) pushImages(ctx context.Context, reason string) {
	if _, ok := logging.Lookup(ctx); ok {
		ctx = logging.With(ctx, logging.Context{Module: "agent"})
	} else {
		ctx = logging.With(ctx, logging.Context{StreamID: logging.NewStreamID(), Module: "agent"})
	}
	pushCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	items, err := h.runtime.Images(pushCtx)
	if err != nil {
		logging.From(ctx).Warn("agent report image push failed", zap.Error(err))
		return
	}
	out := make([]*agentpb.DockerImage, 0, len(items))
	for _, item := range items {
		out = append(out, pbDockerImage(item))
	}
	h.broadcastReport(&agentpb.AgentReport{
		SampleAt: timestamppb.New(time.Now().UTC().Truncate(time.Second)),
		Reason:   reason,
		Images:   &agentpb.DockerImagesResponse{Items: out},
	})
}

func (h *reportHub) pushPackageUpdates(ctx context.Context) {
	if _, ok := logging.Lookup(ctx); ok {
		ctx = logging.With(ctx, logging.Context{Module: "agent"})
	} else {
		ctx = logging.With(ctx, logging.Context{StreamID: logging.NewStreamID(), Module: "agent"})
	}
	pushCtx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	items, err := h.collector.PackageUpdates(pushCtx)
	if err != nil {
		logging.From(ctx).Warn("agent report package push failed", zap.Error(err))
		return
	}
	h.broadcastReport(&agentpb.AgentReport{
		SampleAt:       timestamppb.New(time.Now().UTC().Truncate(time.Second)),
		Reason:         "package_change",
		PackageUpdates: &agentpb.PackageUpdatesResponse{Items: pbPackageUpdates(items)},
	})
}

func (h *reportHub) broadcastReport(report *agentpb.AgentReport) {
	if report == nil {
		return
	}
	watchers := h.snapshot()
	for _, watcher := range watchers {
		if watcher.cfg.serverID == "" {
			continue
		}
		sendReport(watcher.ch, report)
	}
}

// runProbeSampler 按当前探测配置独立采样：配置启用且目标非空时立即执行一轮，
// 之后每 interval 执行一轮；配置变化会打断等待并按新配置重新开始。
func (h *reportHub) runProbeSampler(ctx context.Context) {
	ctx = logging.With(ctx, logging.Context{StreamID: logging.NewStreamID(), Module: "agent"})
	for {
		cfg := h.probeConfigSnapshot()
		if !cfg.enabled || len(cfg.targets) == 0 {
			select {
			case <-ctx.Done():
				return
			case <-h.probeWake:
			}
			continue
		}
		results := agentprobe.Run(ctx, cfg.targets, h.probeRunner)
		if ctx.Err() != nil {
			return
		}
		h.broadcastProbeResults(results)
		interval := cfg.interval
		if interval <= 0 {
			interval = defaultProbeInterval
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-h.probeWake:
			timer.Stop()
		case <-timer.C:
		}
	}
}

// broadcastProbeResults 只把探测结果发给启用了探测的 watcher。探测失败只逐
// 目标带 Error 返回，既不影响指标上报，也不清空上次结果。
func (h *reportHub) broadcastProbeResults(results []agentprobe.Result) {
	if len(results) == 0 {
		return
	}
	items := make([]*agentpb.AgentProbeResult, 0, len(results))
	for _, result := range results {
		items = append(items, &agentpb.AgentProbeResult{
			TargetServerId:    result.TargetServerID,
			TargetAddress:     result.TargetAddress,
			LatencyMs:         result.LatencyMS,
			PacketLossPercent: result.PacketLossPercent,
			Sent:              int32(result.Sent),
			Received:          int32(result.Received),
			Error:             result.Error,
		})
	}
	report := &agentpb.AgentReport{
		SampleAt: timestamppb.New(time.Now().UTC().Truncate(time.Second)),
		Reason:   "network_probe",
		Probes:   &agentpb.AgentProbeReport{Items: items},
	}
	for _, watcher := range h.snapshot() {
		if !watcher.cfg.probe.enabled {
			continue
		}
		sendReport(watcher.ch, report)
	}
}

func (h *reportHub) collectAndBroadcast(sampleAt time.Time, forceContainers bool, forcedReason string) {
	h.collectMu.Lock()
	defer h.collectMu.Unlock()
	watchers := h.snapshot()
	targets := make([]reportWatcherSnapshot, 0, len(watchers))
	var collectMetrics, collectContainers bool
	serverID := ""
	for _, watcher := range watchers {
		if watcher.cfg.serverID == "" {
			continue
		}
		metricsDue := !forceContainers && reportIntervalDue(sampleAt, watcher.cfg.metricsInterval)
		containersDue := reportIntervalDue(sampleAt, watcher.cfg.containerInterval)
		if forceContainers {
			containersDue = watcher.cfg.containerInterval > 0
		}
		if !metricsDue && !containersDue {
			continue
		}
		if serverID == "" {
			serverID = watcher.cfg.serverID
		}
		collectMetrics = collectMetrics || metricsDue
		collectContainers = collectContainers || containersDue
		targets = append(targets, watcher)
	}
	if len(targets) == 0 || serverID == "" {
		return
	}

	ctx, cancel := context.WithTimeout(logging.Background("agent"), reportCollectionTimeout)
	defer cancel()
	var metrics *agentpb.MetricsSnapshotResponse
	if collectMetrics {
		if snap, ok := h.metrics.snapshot(); ok {
			metrics = pbSnapshot(snap)
		}
		// 采样缓存尚未齐全（刚启动或采集持续失败）时保持 nil，
		// Panel 不会用空快照覆盖旧数据。
	}

	var containers *agentpb.DockerContainersResponse
	reason := "scheduled"
	if forcedReason != "" {
		reason = forcedReason
	}
	if collectContainers {
		items, hash, err := h.reportContainers(ctx)
		if err == nil {
			containers = &agentpb.DockerContainersResponse{Items: items}
			h.mu.Lock()
			if forcedReason == "" && h.lastContainerHash != "" && hash != h.lastContainerHash {
				reason = "container_change"
			}
			h.lastContainerHash = hash
			h.mu.Unlock()
		} else {
			// Keep the field nil so the panel never clears the last known
			// container snapshot on a transient collection failure.
			logging.From(ctx).Warn("agent report container collection failed", zap.Error(err))
		}
	}

	// Tailscale 状态变化很慢，跟随容器上报节拍采集即可；采集失败时保持 nil，
	// Panel 不会用空结果覆盖已有观测（与容器快照同一约定）。
	var tailscale *agentpb.TailscaleStatusResponse
	if collectContainers {
		if status, err := h.collector.TailscaleStatus(ctx); err == nil {
			tailscale = pbTailscaleStatus(status)
		} else {
			logging.From(ctx).Warn("agent report tailscale collection failed", zap.Error(err))
		}
	}

	for _, target := range targets {
		report := &agentpb.AgentReport{SampleAt: timestamppb.New(sampleAt), Reason: reason}
		if !forceContainers && reportIntervalDue(sampleAt, target.cfg.metricsInterval) {
			report.Metrics = metrics
		}
		if (forceContainers && target.cfg.containerInterval > 0) || (!forceContainers && reportIntervalDue(sampleAt, target.cfg.containerInterval)) {
			report.Containers = containers
			report.Tailscale = tailscale
		}
		if report.Metrics == nil && report.Containers == nil {
			continue
		}
		sendReport(target.ch, report)
	}
}

func sendReport(ch chan *agentpb.AgentReport, report *agentpb.AgentReport) {
	select {
	case ch <- report:
	default:
		select {
		case <-ch:
		default:
		}
		ch <- report
	}
}

func normalizeReportConfig(msg *agentpb.AgentReportControl) reportConfig {
	if msg == nil {
		return reportConfig{}
	}
	out := reportConfig{serverID: msg.ServerId}
	if msg.Config != nil {
		out.metricsInterval = time.Duration(msg.Config.MetricsIntervalSeconds) * time.Second
		out.containerInterval = time.Duration(msg.Config.ContainersIntervalSeconds) * time.Second
	}
	if out.metricsInterval < time.Second {
		out.metricsInterval = 0
	}
	if out.containerInterval < time.Second {
		out.containerInterval = 0
	}
	// Probe 非 nil 时整体替换探测配置：Panel 显式下发 enabled=false 或空
	// targets 时必须清空本地任务，而不是沿用旧目标。
	if msg.Probe != nil {
		out.probeSet = true
		out.probe.enabled = msg.Probe.Enabled
		out.probe.interval = time.Duration(msg.Probe.IntervalSeconds) * time.Second
		if out.probe.interval <= 0 {
			out.probe.interval = defaultProbeInterval
		}
		out.probe.targets = make([]agentprobe.Target, 0, len(msg.Probe.Targets))
		for _, target := range msg.Probe.Targets {
			if target == nil {
				continue
			}
			address := strings.TrimSpace(target.Address)
			if address == "" {
				continue
			}
			out.probe.targets = append(out.probe.targets, agentprobe.Target{
				ServerID: target.ServerId,
				Address:  address,
				Name:     target.Name,
			})
		}
	}
	return out
}

func (h *reportHub) reportContainers(ctx context.Context) ([]*agentpb.DockerContainer, string, error) {
	if h.runtime == nil {
		return nil, "", errors.New("runtime is not configured")
	}
	items, err := h.runtime.Containers(ctx)
	if err != nil {
		return nil, "", err
	}
	out := make([]*agentpb.DockerContainer, 0, len(items))
	hash := sha256.New()
	for _, item := range items {
		out = append(out, pbDockerContainerSlim(item))
		hashDockerContainer(hash, item)
	}
	return out, hex.EncodeToString(hash.Sum(nil)), nil
}

// hashDockerContainer folds the container fields that matter for change
// detection into a streaming hash, avoiding a full JSON marshal of the list.
// Managed-file drift labels are excluded so a drift transition does not
// masquerade as a container change.
func hashDockerContainer(h io.Writer, c agentcontract.DockerContainer) {
	fmt.Fprintf(h, "%s\x00%s\x00%s\x00%s\x00", c.ID, c.Image, c.State, c.Status)
	for _, name := range c.Names {
		fmt.Fprintf(h, "%s\x00", name)
	}
	h.Write([]byte{0xff})
	for _, port := range c.Ports {
		fmt.Fprintf(h, "%s\x00%d\x00%d\x00%s\x00", port.IP, port.PrivatePort, port.PublicPort, port.Type)
	}
	h.Write([]byte{0xfe})
	if len(c.Labels) > 0 {
		keys := make([]string, 0, len(c.Labels))
		for key := range c.Labels {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if strings.HasPrefix(key, "panel.application.managed_files.") {
				continue
			}
			fmt.Fprintf(h, "%s=%s\x00", key, c.Labels[key])
		}
	}
	h.Write([]byte{0xfd})
}

func nextReportDue(now time.Time, watchers []reportWatcherSnapshot) (time.Time, bool) {
	var out time.Time
	for _, watcher := range watchers {
		if watcher.cfg.serverID == "" {
			continue
		}
		if watcher.cfg.metricsInterval > 0 {
			out = minOptionalTime(out, nextAligned(now, watcher.cfg.metricsInterval))
		}
		if watcher.cfg.containerInterval > 0 {
			out = minOptionalTime(out, nextAligned(now, watcher.cfg.containerInterval))
		}
	}
	if out.IsZero() {
		return time.Time{}, false
	}
	return out, true
}

func reportIntervalDue(sampleAt time.Time, interval time.Duration) bool {
	seconds := int64(interval / time.Second)
	return seconds > 0 && sampleAt.UTC().Unix()%seconds == 0
}

func nextAligned(now time.Time, interval time.Duration) time.Time {
	now = now.UTC()
	seconds := int64(interval / time.Second)
	if seconds <= 0 {
		seconds = 1
	}
	unix := now.Unix()
	next := ((unix / seconds) + 1) * seconds
	if now.Nanosecond() == 0 && unix%seconds == 0 {
		next = unix
	}
	return time.Unix(next, 0).UTC()
}

func minOptionalTime(a, b time.Time) time.Time {
	if a.IsZero() || b.Before(a) {
		return b
	}
	return a
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
