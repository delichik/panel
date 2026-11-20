import type { ServerMetricsSeries } from '@/api/servers';
import type { ApplicationSummaryDto } from '@/types/applications';
import type { NetworkProbeHistoryPoint, NetworkProbeResult } from '@/types/networkProbe';
import type { ServerDto } from '@/types/servers';
import { formatPercent as sharedFormatPercent } from '@/utils/metrics';

/** 应用总览：总数 + 启用/停用/部署中/需关注（后三者是启用状态下的细分）。 */
export interface ApplicationSummaryView {
  total: number | null;
  enabled: number;
  disabled: number;
  deploying: number;
  attention: number;
  available: boolean;
}

export function summarizeApplications(items: ApplicationSummaryDto[], total = items.length): ApplicationSummaryView {
  let enabled = 0;
  let disabled = 0;
  let deploying = 0;
  let attention = 0;
  for (const app of items) {
    if (!app.enabled) {
      disabled += 1;
      continue;
    }
    enabled += 1;
    if ((app.deployingInstanceCount ?? 0) > 0) deploying += 1;
    if (app.planningError || app.lastError || (app.abnormalInstanceCount ?? 0) > 0) attention += 1;
  }
  return { total, enabled, disabled, deploying, attention, available: true };
}

export function emptyApplicationSummary(): ApplicationSummaryView {
  return { total: null, enabled: 0, disabled: 0, deploying: 0, attention: 0, available: false };
}

export interface ApplicationRatioView {
  /** 已启用占全部应用的比例（0–100）；不可用或总数为 0 时为 null。 */
  enabledPercent: number | null;
  /** 已停用占比；与 enabledPercent 互斥，合计 100。 */
  disabledPercent: number | null;
}

/**
 * 应用启用/停用占比，仅用于摘要区的分布条。
 *
 * 部署中、需关注都是“已启用”下的子集，因此不进入互斥分布条，避免同一应用被
 * 重复计数；占比只在应用摘要可用且总数 > 0 时给出，失败时不得伪造 0。
 */
export function applicationRatio(summary: ApplicationSummaryView): ApplicationRatioView {
  if (!summary.available || summary.total === null || summary.total <= 0) {
    return { enabledPercent: null, disabledPercent: null };
  }
  const enabledPercent = Math.min(100, Math.max(0, (summary.enabled / summary.total) * 100));
  return { enabledPercent, disabledPercent: 100 - enabledPercent };
}

/** 指标新鲜度窗口：与后端 probe 口径一致（5 分钟）。 */
export const METRICS_FRESH_WINDOW_MS = 5 * 60 * 1000;
/**
 * 桶查询缺少 latest 时的兜底新鲜度窗口：24h/30min 桶的最后一点是桶起点，
 * 最多可能比当前时间早一个桶；缺少 latest 时不能因此把整卡指标误判为过期。
 */
export const BUCKET_FRESHNESS_FALLBACK_MS = 35 * 60 * 1000;
/** 卡片历史条展示最近 24 小时，逐小时一个分段。 */
export const HISTORY_HOURS = 24;

export type MetricTone = 'success' | 'warning' | 'danger';
export type NodeStatusKind = 'online' | 'offline';

/** 历史分段与数值的阈值；CPU/延迟/丢包/负载各一组。 */
export const toneZones = {
  cpu: [{ to: 70, tone: 'success' }, { to: 90, tone: 'warning' }, { to: Infinity, tone: 'danger' }],
  latency: [{ to: 80, tone: 'success' }, { to: 150, tone: 'warning' }, { to: Infinity, tone: 'danger' }],
  packetLoss: [{ to: 2, tone: 'success' }, { to: 5, tone: 'warning' }, { to: Infinity, tone: 'danger' }],
  load: [{ to: 1, tone: 'success' }, { to: 2, tone: 'warning' }, { to: Infinity, tone: 'danger' }],
  diskRead: [{ to: 50 * 1024 ** 2, tone: 'success' }, { to: 200 * 1024 ** 2, tone: 'warning' }, { to: Infinity, tone: 'danger' }],
  diskWrite: [{ to: 50 * 1024 ** 2, tone: 'success' }, { to: 200 * 1024 ** 2, tone: 'warning' }, { to: Infinity, tone: 'danger' }],
} satisfies Record<string, Array<{ to: number; tone: MetricTone }>>;

export type HistoryKind = keyof typeof toneZones;

/**
 * 历史条/当前值的语义色。配置了警告阈值时，以阈值为红色起点，并按默认
 * 绿/黄/红区间比例自动推导黄色起点（阈值=默认红色起点时恰好复现默认区间）；
 * 未配置阈值时沿用固定 `toneZones`。
 */
export function valueTone(kind: HistoryKind, value: number | null | undefined, threshold?: number | null): MetricTone | null {
  if (value === null || value === undefined || !Number.isFinite(value)) return null;
  if (typeof threshold === 'number' && Number.isFinite(threshold) && threshold > 0) {
    const defaultWarning = toneZones[kind][0].to;
    const defaultDanger = toneZones[kind][1].to;
    const warningStart = defaultDanger > 0 ? threshold * (defaultWarning / defaultDanger) : threshold;
    if (value >= threshold) return 'danger';
    if (value >= warningStart) return 'warning';
    return 'success';
  }
  const zone = toneZones[kind].find((item) => value <= item.to);
  return zone?.tone ?? 'danger';
}

/** 内存/磁盘进度条的阈值：<70 绿、<90 黄、其余红。 */
export function percentTone(percent: number | null | undefined): MetricTone | null {
  if (percent === null || percent === undefined || !Number.isFinite(percent)) return null;
  if (percent < 70) return 'success';
  if (percent < 90) return 'warning';
  return 'danger';
}

export function nodeStatusKind(server: ServerDto): NodeStatusKind {
  // 在线状态以面板的可达性为准，与指标新鲜度解耦：没有指标样本的在线节点
  // 仍应显示在线（指标单元格显示 `—`），不能因此被误判为离线/指标过期。
  return server.reachable ? 'online' : 'offline';
}

export function nodeStatusTone(kind: NodeStatusKind): MetricTone {
  return kind === 'online' ? 'success' : 'danger';
}

export interface ServerMetricsView {
  cpuPercent: number | null;
  memoryUsedBytes: number | null;
  memoryTotalBytes: number | null;
  diskUsedBytes: number | null;
  diskTotalBytes: number | null;
  diskSampledAt: string | null;
  load1: number | null;
  rxBytesPerSecond: number | null;
  txBytesPerSecond: number | null;
  diskReadBytesPerSecond: number | null;
  diskWriteBytesPerSecond: number | null;
  sampledAt: string | null;
}

const emptyMetricsView: ServerMetricsView = {
  cpuPercent: null,
  memoryUsedBytes: null,
  memoryTotalBytes: null,
  diskUsedBytes: null,
  diskTotalBytes: null,
  diskSampledAt: null,
  load1: null,
  rxBytesPerSecond: null,
  txBytesPerSecond: null,
  diskReadBytesPerSecond: null,
  diskWriteBytesPerSecond: null,
  sampledAt: null,
};

/** 取当前值：优先后端 bucket 查询返回的 latest 原始样本，退化到序列最后一点。 */
export function latestMetricsView(series?: ServerMetricsSeries | null): ServerMetricsView {
  if (!series) return { ...emptyMetricsView };
  const latest = series.latest;
  const cpu = lastPoint(series.cpu);
  const memory = lastPoint(series.memory);
  const load = lastPoint(series.load);
  const network = lastPoint(series.network);
  const diskIO = lastPoint(series.diskIo);
  const disk = series.diskCurrent ?? null;
  return {
    cpuPercent: latest?.cpuUsagePercent ?? cpu?.usagePercent ?? null,
    memoryUsedBytes: latest?.memoryUsedBytes ?? memory?.usedBytes ?? null,
    memoryTotalBytes: latest?.memoryTotalBytes ?? memory?.totalBytes ?? null,
    diskUsedBytes: latest?.diskUsedBytes ?? disk?.usedBytes ?? null,
    diskTotalBytes: latest?.diskTotalBytes ?? disk?.totalBytes ?? null,
    diskSampledAt: latest?.diskSampledAt ?? disk?.time ?? null,
    load1: latest?.load1 ?? load?.load1 ?? null,
    rxBytesPerSecond: latest?.rxBytesPerSecond ?? network?.rxBytesPerSecond ?? null,
    txBytesPerSecond: latest?.txBytesPerSecond ?? network?.txBytesPerSecond ?? null,
    diskReadBytesPerSecond: diskIO?.readBytesPerSecond ?? null,
    diskWriteBytesPerSecond: diskIO?.writeBytesPerSecond ?? null,
    sampledAt: latest?.time ?? cpu?.time ?? memory?.time ?? load?.time ?? null,
  };
}

function lastPoint<T extends { time: string }>(points: T[] | undefined): T | null {
  if (!points?.length) return null;
  return points[points.length - 1] ?? null;
}

export interface HistoryPoint {
  time: string;
  value: number | null;
}

/**
 * 把任意时间点的数值序列聚合成最近 24 个整点小时桶（最旧→最新）。
 * 缺失小时为 null，便于渲染灰段；不做跨小时插值。
 */
export function hourlyHistory(points: HistoryPoint[] | undefined, now = Date.now()): Array<number | null> {
  const buckets: number[][] = Array.from({ length: HISTORY_HOURS }, () => []);
  const hourMs = 60 * 60 * 1000;
  const currentHour = Math.floor(now / hourMs) * hourMs;
  for (const point of points ?? []) {
    if (point.value === null || !Number.isFinite(point.value)) continue;
    const parsed = Date.parse(point.time);
    if (Number.isNaN(parsed)) continue;
    const hour = Math.floor(parsed / hourMs) * hourMs;
    const index = HISTORY_HOURS - 1 - Math.round((currentHour - hour) / hourMs);
    if (index < 0 || index >= HISTORY_HOURS) continue;
    buckets[index]!.push(point.value);
  }
  return buckets.map((values) => (values.length ? values.reduce((sum, value) => sum + value, 0) / values.length : null));
}

export function cpuHistory(series?: ServerMetricsSeries | null, now = Date.now()): Array<number | null> {
  return hourlyHistory(series?.cpu?.map((point) => ({ time: point.time, value: point.usagePercent })), now);
}

export function loadHistory(series?: ServerMetricsSeries | null, now = Date.now()): Array<number | null> {
  return hourlyHistory(series?.load?.map((point) => ({ time: point.time, value: point.load1 })), now);
}

export function diskReadHistory(series?: ServerMetricsSeries | null, now = Date.now()): Array<number | null> {
  return hourlyHistory(series?.diskIo?.map((point) => ({ time: point.time, value: point.readBytesPerSecond })), now);
}

export function diskWriteHistory(series?: ServerMetricsSeries | null, now = Date.now()): Array<number | null> {
  return hourlyHistory(series?.diskIo?.map((point) => ({ time: point.time, value: point.writeBytesPerSecond })), now);
}

export function probeLatencyHistory(history: NetworkProbeHistoryPoint[] | undefined, now = Date.now()): Array<number | null> {
  return hourlyHistory(history?.map((point) => ({ time: point.time, value: point.latencyMs > 0 ? point.latencyMs : null })), now);
}

export function probeLossHistory(history: NetworkProbeHistoryPoint[] | undefined, now = Date.now()): Array<number | null> {
  return hourlyHistory(history?.map((point) => ({ time: point.time, value: point.packetLossPercent })), now);
}

/** 探测源自身视角：把各目标的历史按小时合并（延迟取成功目标均值，丢包取均值）。 */
export function aggregateProbeHistory(
  results: NetworkProbeResult[],
  field: 'latency' | 'loss',
  now = Date.now(),
): Array<number | null> {
  const series = results.map((result) => (
    field === 'latency' ? probeLatencyHistory(result.history, now) : probeLossHistory(result.history, now)
  ));
  return Array.from({ length: HISTORY_HOURS }, (_, index) => {
    const values = series.map((item) => item[index]).filter((value): value is number => value !== null && Number.isFinite(value));
    return values.length ? values.reduce((sum, value) => sum + value, 0) / values.length : null;
  });
}

export interface ProbeTargetView {
  targetServerId: string;
  latencyMs: number | null;
  packetLossPercent: number | null;
  failed: boolean;
  stale: boolean;
  sampledAt: string | null;
  history: NetworkProbeHistoryPoint[];
}

export function probeTargetView(result: NetworkProbeResult): ProbeTargetView {
  // 后端 error 字段 omitempty：成功结果不会带该字段，不能直接 trim()。
  const failed = Boolean(result.error?.trim()) || result.received <= 0;
  return {
    targetServerId: result.targetServerId,
    latencyMs: failed || result.latencyMs <= 0 ? null : result.latencyMs,
    packetLossPercent: failed ? 100 : result.packetLossPercent,
    failed,
    stale: Boolean(result.stale),
    sampledAt: result.sampledAt || null,
    history: result.history ?? [],
  };
}

export interface ProbeAggregateView {
  latencyMs: number | null;
  packetLossPercent: number | null;
  targetCount: number;
  failedTargetCount: number;
  sampledAt: string | null;
  stale: boolean;
}

export function summarizeProbeResults(results: NetworkProbeResult[] = []): ProbeAggregateView {
  const views = results.map(probeTargetView);
  const successful = views.filter((item) => !item.failed && item.latencyMs !== null);
  const latencyMs = successful.length
    ? successful.reduce((sum, item) => sum + (item.latencyMs ?? 0), 0) / successful.length
    : null;
  const sent = results.reduce((sum, item) => sum + Math.max(0, item.sent), 0);
  const received = results.reduce((sum, item) => sum + Math.max(0, item.received), 0);
  const packetLossPercent = sent > 0 ? ((sent - received) / sent) * 100 : null;
  const sampledAt = latestIso(results.map((item) => item.sampledAt));
  return {
    latencyMs,
    packetLossPercent,
    targetCount: results.length,
    failedTargetCount: views.filter((item) => item.failed).length,
    sampledAt,
    stale: results.length === 0 || views.every((item) => item.stale),
  };
}

function latestIso(values: string[]): string | null {
  let latest: string | null = null;
  let latestMs = Number.NEGATIVE_INFINITY;
  for (const value of values) {
    const parsed = Date.parse(value);
    if (Number.isNaN(parsed)) continue;
    if (parsed > latestMs) {
      latestMs = parsed;
      latest = value;
    }
  }
  return latest;
}

export interface ServerCardView {
  server: ServerDto;
  status: NodeStatusKind;
  statusTone: MetricTone;
  metrics: ServerMetricsView;
  metricsError: boolean;
  metricsFresh: boolean;
  isProbeSource: boolean;
  probeAvailable: boolean;
  probeLatencyMs: number | null;
  probePacketLossPercent: number | null;
  probeStale: boolean;
  cpuHistory: Array<number | null>;
  loadHistory: Array<number | null>;
  diskReadHistory: Array<number | null>;
  diskWriteHistory: Array<number | null>;
  latencyHistory: Array<number | null>;
  lossHistory: Array<number | null>;
  packageUpdates: number | null;
  imageUpdates: number | null;
}

export interface BuildCardsInput {
  servers: ServerDto[];
  metricsById: Record<string, ServerMetricsSeries | undefined>;
  metricsErrorIds: ReadonlySet<string>;
  probeSourceId: string;
  probeResults: NetworkProbeResult[];
  packageUpdatesById: Record<string, number | null>;
  imageUpdatesById: Record<string, number | null>;
  now?: number;
}

export function buildServerCards(input: BuildCardsInput): ServerCardView[] {
  const now = input.now ?? Date.now();
  const targets = new Map<string, ProbeTargetView>();
  for (const result of input.probeResults) {
    targets.set(result.targetServerId, probeTargetView(result));
  }
  return input.servers.map((server) => {
    const series = input.metricsById[server.id];
    const metrics = latestMetricsView(series);
    const freshnessMs = series?.latest ? METRICS_FRESH_WINDOW_MS : BUCKET_FRESHNESS_FALLBACK_MS;
    const metricsFresh = (() => {
      if (!metrics.sampledAt) return false;
      const parsed = Date.parse(metrics.sampledAt);
      return !Number.isNaN(parsed) && now - parsed <= freshnessMs;
    })();
    const status = nodeStatusKind(server);
    const isProbeSource = server.id === input.probeSourceId;
    const target = isProbeSource ? undefined : targets.get(server.id);
    const hasProbe = Boolean(target);
    return {
      server,
      status,
      statusTone: nodeStatusTone(status),
      metrics,
      metricsError: input.metricsErrorIds.has(server.id),
      metricsFresh,
      isProbeSource,
      probeAvailable: hasProbe,
      probeLatencyMs: target?.latencyMs ?? null,
      probePacketLossPercent: target?.packetLossPercent ?? null,
      probeStale: Boolean(target?.stale),
      cpuHistory: cpuHistory(series, now),
      loadHistory: loadHistory(series, now),
      diskReadHistory: diskReadHistory(series, now),
      diskWriteHistory: diskWriteHistory(series, now),
      latencyHistory: probeLatencyHistory(target?.history, now),
      lossHistory: probeLossHistory(target?.history, now),
      packageUpdates: input.packageUpdatesById[server.id] ?? null,
      imageUpdates: input.imageUpdatesById[server.id] ?? null,
    };
  });
}

export interface NodeSummaryView {
  total: number;
  online: number;
  offline: number;
  fresh: number;
  unsupported: number;
}

export function summarizeNodeCards(cards: ServerCardView[]): NodeSummaryView {
  let online = 0;
  let offline = 0;
  let fresh = 0;
  let unsupported = 0;
  for (const card of cards) {
    if (card.status === 'online') online += 1;
    else offline += 1;
    if (card.metricsFresh) fresh += 1;
    if (card.server.os?.supported === false) unsupported += 1;
  }
  return { total: cards.length, online, offline, fresh, unsupported };
}

/** 节点摘要环的语义色调：没有离线节点为 success，出现离线为 warning。 */
export function onlineRateTone(summary: NodeSummaryView): MetricTone {
  return summary.offline > 0 ? 'warning' : 'success';
}

export interface CapacityView {
  /** 至少有一种指标样本的在线节点数（离线节点不参与聚合）。 */
  sampleNodes: number;
  cpuPercent: number | null;
  memoryUsedBytes: number | null;
  memoryTotalBytes: number | null;
  memoryPercent: number | null;
  diskUsedBytes: number | null;
  diskTotalBytes: number | null;
  diskPercent: number | null;
}

const emptyCapacityView: CapacityView = {
  sampleNodes: 0,
  cpuPercent: null,
  memoryUsedBytes: null,
  memoryTotalBytes: null,
  memoryPercent: null,
  diskUsedBytes: null,
  diskTotalBytes: null,
  diskPercent: null,
};

/**
 * 在线节点容量摘要：只聚合在线且有有效样本的节点。
 *
 * CPU 取有样本节点的算术平均；内存/磁盘取有 used+total 节点的合计后求比例。
 * 离线节点、无样本节点不进入任何分子分母；完全无样本时所有值为 null，页面
 * 显示 `—` 与空环，不得用 0% 冒充“磁盘为空”。
 */
export function summarizeCapacity(cards: ServerCardView[]): CapacityView {
  let cpuSum = 0;
  let cpuCount = 0;
  let memoryUsed = 0;
  let memoryTotal = 0;
  let memoryCount = 0;
  let diskUsed = 0;
  let diskTotal = 0;
  let diskCount = 0;
  const sampleIds = new Set<string>();
  for (const card of cards) {
    if (card.status !== 'online') continue;
    const metrics = card.metrics;
    if (metrics.cpuPercent !== null && Number.isFinite(metrics.cpuPercent)) {
      cpuSum += metrics.cpuPercent;
      cpuCount += 1;
      sampleIds.add(card.server.id);
    }
    if (
      metrics.memoryUsedBytes !== null
      && metrics.memoryTotalBytes !== null
      && Number.isFinite(metrics.memoryUsedBytes)
      && Number.isFinite(metrics.memoryTotalBytes)
      && metrics.memoryTotalBytes > 0
    ) {
      memoryUsed += metrics.memoryUsedBytes;
      memoryTotal += metrics.memoryTotalBytes;
      memoryCount += 1;
      sampleIds.add(card.server.id);
    }
    if (
      metrics.diskUsedBytes !== null
      && metrics.diskTotalBytes !== null
      && Number.isFinite(metrics.diskUsedBytes)
      && Number.isFinite(metrics.diskTotalBytes)
      && metrics.diskTotalBytes > 0
    ) {
      diskUsed += metrics.diskUsedBytes;
      diskTotal += metrics.diskTotalBytes;
      diskCount += 1;
      sampleIds.add(card.server.id);
    }
  }
  if (sampleIds.size === 0) return emptyCapacityView;
  return {
    sampleNodes: sampleIds.size,
    cpuPercent: cpuCount > 0 ? cpuSum / cpuCount : null,
    memoryUsedBytes: memoryCount > 0 ? memoryUsed : null,
    memoryTotalBytes: memoryCount > 0 ? memoryTotal : null,
    memoryPercent: memoryTotal > 0 ? clampPercent((memoryUsed / memoryTotal) * 100) : null,
    diskUsedBytes: diskCount > 0 ? diskUsed : null,
    diskTotalBytes: diskCount > 0 ? diskTotal : null,
    diskPercent: diskTotal > 0 ? clampPercent((diskUsed / diskTotal) * 100) : null,
  };
}

function clampPercent(value: number): number {
  return Math.min(100, Math.max(0, value));
}

export interface RelativeTimeLabel {
  key: string;
  params?: Record<string, number>;
}

/** 相对时间文案 key（实际翻译由页面通过 t() 完成）。 */
export function relativeTimeLabel(value: string | null | undefined, now = Date.now()): RelativeTimeLabel | null {
  if (!value) return null;
  const parsed = Date.parse(value);
  if (Number.isNaN(parsed)) return null;
  const diff = Math.max(0, now - parsed);
  if (diff < 60 * 1000) return { key: 'overviewPage.relativeJustNow' };
  if (diff < 60 * 60 * 1000) return { key: 'overviewPage.relativeMinutes', params: { count: Math.floor(diff / 60000) } };
  if (diff < 24 * 60 * 60 * 1000) return { key: 'overviewPage.relativeHours', params: { count: Math.floor(diff / 3600000) } };
  return { key: 'overviewPage.relativeDays', params: { count: Math.floor(diff / 86400000) } };
}

export function memoryPercent(view: ServerMetricsView): number | null {
  if (!view.memoryUsedBytes || !view.memoryTotalBytes || view.memoryTotalBytes <= 0) return null;
  return Math.min(100, Math.max(0, (view.memoryUsedBytes / view.memoryTotalBytes) * 100));
}

export function diskPercent(view: ServerMetricsView): number | null {
  if (!view.diskUsedBytes || !view.diskTotalBytes || view.diskTotalBytes <= 0) return null;
  return Math.min(100, Math.max(0, (view.diskUsedBytes / view.diskTotalBytes) * 100));
}

export function formatPercent(value: number | null | undefined, digits = 0): string {
  return sharedFormatPercent(value, digits);
}

export function formatLatency(value: number | null | undefined): string {
  if (value === null || value === undefined || !Number.isFinite(value)) return '—';
  return `${Math.round(value)} ms`;
}

export function formatPacketLoss(value: number | null | undefined): string {
  if (value === null || value === undefined || !Number.isFinite(value)) return '—';
  return `${value.toFixed(1)} %`;
}

export function formatLoad(value: number | null | undefined): string {
  if (value === null || value === undefined || !Number.isFinite(value)) return '—';
  return value.toFixed(2);
}

export { formatBytes, formatRate } from '@/utils/metrics';
