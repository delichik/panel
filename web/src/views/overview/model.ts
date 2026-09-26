import type { OverviewCardConfiguration, OverviewCardData, OverviewDto, OverviewMetricPoint, OverviewMetricsSeries, OverviewCardRange, OverviewServerSummary } from '@/types/overview';

export interface OverviewRisk {
  id: string;
  tone: 'warning' | 'danger' | 'info';
  title: string;
  description: string;
  to: string;
}

export function summarizeOverview(data: OverviewDto) {
  const total = data.servers.length;
  const reachable = data.servers.filter((item) => item.reachable).length;
  const supported = data.servers.filter((item) => item.supported).length;
  const fresh = data.servers.filter((item) => item.metricsFresh).length;
  const updates = data.servers.reduce((sum, item) => sum + (item.packageUpdateCount || 0), 0);
  return { total, reachable, supported, fresh, updates };
}

export function overviewRisks(servers: OverviewServerSummary[]): OverviewRisk[] {
  const risks: OverviewRisk[] = [];
  for (const server of servers) {
    if (!server.reachable) {
      risks.push({ id: `${server.id}:reach`, tone: 'danger', title: server.name, description: 'overviewPage.riskUnreachable', to: `/servers?server=${server.id}` });
    } else if (!server.supported) {
      risks.push({ id: `${server.id}:os`, tone: 'warning', title: server.name, description: 'overviewPage.riskUnsupported', to: `/servers?server=${server.id}` });
    } else if (!server.metricsFresh) {
      risks.push({ id: `${server.id}:metrics`, tone: 'warning', title: server.name, description: 'overviewPage.riskMetricsStale', to: `/servers?server=${server.id}` });
    }
    if (server.packageUpdateCount > 0) {
      risks.push({ id: `${server.id}:packages`, tone: 'info', title: server.name, description: 'overviewPage.riskPackages', to: '/resources/packages' });
    }
  }
  return risks.slice(0, 8);
}

export function cardHasData(card: OverviewCardConfiguration, data?: OverviewCardData) {
  if (card.kind === 'placeholder') return false;
  if (card.kind === 'packageUpdates' || card.kind === 'containerUpdates') return true;
  return Object.keys(data?.metricsByServer ?? {}).length > 0;
}

export function defaultOverviewCards(): OverviewCardConfiguration[] {
  return [
    createOverviewCard('cpu', '1h', 3, 2),
    createOverviewCard('memory', '1h', 3, 2),
    createOverviewCard('disk', '6h', 2, 2),
    createOverviewCard('network', '1h', 6, 2),
    createOverviewCard('packageUpdates', '1d', 2, 1),
    createOverviewCard('containerUpdates', '1d', 2, 1),
  ];
}

export function createOverviewCard(
  kind: OverviewCardConfiguration['kind'],
  range: OverviewCardConfiguration['range'],
  width = 3,
  height = 2,
): OverviewCardConfiguration {
  return {
    id: `card-${Date.now()}-${Math.random().toString(36).slice(2, 7)}`,
    kind,
    width,
    height,
    range,
    networkDirection: 'both',
    serverIds: [],
  };
}

export interface OverviewCardChartSeries {
  id: string;
  name: string;
  values: Array<number | null>;
}

export interface OverviewCardView {
  labels: string[];
  series: OverviewCardChartSeries[];
  latestValue: number | null;
  peakValue: number | null;
}

/**
 * 一次性把卡片原始指标派生成图表与摘要视图。每个服务器先建 time -> value
 * 映射，再按统一时间轴取值，整体复杂度为 O(总点数 + 服务器数 x 时间点数)，
 * 避免在渲染期做 O(n²) 的逐点查找。摘要值按时间取跨服务器均值，与旧聚合语义一致。
 */
export function deriveCardView(
  card: OverviewCardConfiguration,
  data: OverviewCardData | undefined,
  serverName: (serverId: string) => string,
): OverviewCardView {
  const entries = Object.entries(data?.metricsByServer ?? {})
    .map(([serverId, series]) => ({ serverId, points: metricPoints(card, series) }))
    .filter((entry) => entry.points.length > 0);
  const timeSet = new Set<string>();
  const perServer = entries.map(({ serverId, points }) => {
    const values = new Map<string, number>();
    for (const point of points) {
      values.set(point.time, metricValueOf(card, point));
      timeSet.add(point.time);
    }
    return { serverId, values };
  });
  const labels = [...timeSet].sort((a, b) => Date.parse(a) - Date.parse(b));
  const series = perServer.map(({ serverId, values }) => ({
    id: serverId,
    name: serverName(serverId),
    values: labels.map((time) => values.get(time) ?? null),
  }));
  const sums = labels.map(() => 0);
  const counts = labels.map(() => 0);
  for (const { values } of perServer) {
    labels.forEach((time, index) => {
      const value = values.get(time);
      if (value === undefined) return;
      sums[index] += value;
      counts[index] += 1;
    });
  }
  let latestValue: number | null = null;
  let peakValue: number | null = null;
  labels.forEach((_, index) => {
    if (!counts[index]) return;
    const average = sums[index] / counts[index];
    latestValue = average;
    if (peakValue === null || average > peakValue) peakValue = average;
  });
  return { labels, series, latestValue, peakValue };
}

export function metricPoints(card: OverviewCardConfiguration, series: OverviewMetricsSeries): OverviewMetricPoint[] {
  if (card.kind === 'cpu') return series.cpu ?? [];
  if (card.kind === 'memory') return series.memory ?? [];
  if (card.kind === 'disk') return series.disk ?? [];
  if (card.kind === 'network') return series.network ?? [];
  return [];
}

export function metricValueOf(card: OverviewCardConfiguration, point: OverviewMetricPoint) {
  if (card.kind === 'cpu') return point.usagePercent ?? 0;
  if (card.kind === 'memory' || card.kind === 'disk') return percentUsed(point);
  if (card.kind === 'network') {
    if (card.networkDirection === 'rx') return point.rxBytesPerSecond ?? 0;
    if (card.networkDirection === 'tx') return point.txBytesPerSecond ?? 0;
    return (point.rxBytesPerSecond ?? 0) + (point.txBytesPerSecond ?? 0);
  }
  return 0;
}

function percentUsed(point: OverviewMetricPoint) {
  return point.totalBytes ? ((point.usedBytes ?? 0) / point.totalBytes) * 100 : 0;
}

/**
 * 增量响应返回从 since 所在桶开始的完整桶，因此按时间戳做后缀替换：保留
 * 早于首个新桶的旧点，再用重算后的桶替换尾部，避免把未满桶的均值追加两次。
 */
export function mergeMetricPoints<T extends { time: string }>(existing: T[] | undefined, incoming: T[] | undefined): T[] {
  if (!incoming?.length) return existing ? [...existing] : [];
  if (!existing?.length) return [...incoming];
  const cutoff = incoming[0].time;
  return [...existing.filter((point) => point.time < cutoff), ...incoming];
}

export function mergeMetricSeries(existing: OverviewMetricsSeries | undefined, incoming: OverviewMetricsSeries | undefined): OverviewMetricsSeries {
  return {
    cpu: mergeMetricPoints(existing?.cpu, incoming?.cpu),
    memory: mergeMetricPoints(existing?.memory, incoming?.memory),
    disk: mergeMetricPoints(existing?.disk, incoming?.disk),
    network: mergeMetricPoints(existing?.network, incoming?.network),
  };
}

export function mergeCardData(existing: OverviewCardData | undefined, delta: OverviewCardData): OverviewCardData {
  const serverIds = new Set([...Object.keys(existing?.metricsByServer ?? {}), ...Object.keys(delta.metricsByServer)]);
  const metricsByServer: Record<string, OverviewMetricsSeries> = {};
  for (const serverId of serverIds) {
    metricsByServer[serverId] = mergeMetricSeries(existing?.metricsByServer[serverId], delta.metricsByServer[serverId]);
  }
  return { card: delta.card, metricsByServer, bucketSeconds: delta.bucketSeconds };
}

const RANGE_DURATIONS_MS: Record<OverviewCardRange, number> = {
  '1h': 60 * 60 * 1000,
  '6h': 6 * 60 * 60 * 1000,
  '1d': 24 * 60 * 60 * 1000,
  '7d': 7 * 24 * 60 * 60 * 1000,
};

export function trimMetricPoints<T extends { time: string }>(points: T[] | undefined, sinceMs: number): T[] {
  if (!points?.length) return [];
  return points.filter((point) => Date.parse(point.time) >= sinceMs);
}

export function trimMetricSeries(series: OverviewMetricsSeries | undefined, sinceMs: number): OverviewMetricsSeries {
  return {
    cpu: trimMetricPoints(series?.cpu, sinceMs),
    memory: trimMetricPoints(series?.memory, sinceMs),
    disk: trimMetricPoints(series?.disk, sinceMs),
    network: trimMetricPoints(series?.network, sinceMs),
  };
}

export function trimCardDataToRange(data: OverviewCardData, now: Date = new Date()): OverviewCardData {
  const durationMs = RANGE_DURATIONS_MS[data.card.range];
  if (!durationMs) return data;
  const sinceMs = now.getTime() - durationMs;
  const metricsByServer: Record<string, OverviewMetricsSeries> = {};
  for (const [serverId, series] of Object.entries(data.metricsByServer)) {
    metricsByServer[serverId] = trimMetricSeries(series, sinceMs);
  }
  return { ...data, metricsByServer };
}