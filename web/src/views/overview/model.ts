import type { OverviewCardConfiguration, OverviewCardData, OverviewDto, OverviewMetricsSeries, OverviewCardRange, OverviewServerSummary } from '@/types/overview';

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

/**
 * 多服务器指标按时间戳对齐后聚合（同一时刻取均值），避免按数组下标错位聚合。
 */
export function aggregateMetricValues(
  pointsByServer: Array<Array<{ time: string }>>,
  valueOf: (point: { time: string }) => number,
): { times: string[]; values: number[] } {
  const byTime = new Map<string, number[]>();
  pointsByServer.forEach((points) => points.forEach((point) => {
    const list = byTime.get(point.time);
    if (list) list.push(valueOf(point));
    else byTime.set(point.time, [valueOf(point)]);
  }));
  const times = [...byTime.keys()].sort((a, b) => Date.parse(a) - Date.parse(b));
  return {
    times,
    values: times.map((time) => {
      const list = byTime.get(time) ?? [];
      return list.length ? list.reduce((sum, value) => sum + value, 0) / list.length : 0;
    }),
  };
}

export function mergeMetricPoints<T extends { time: string }>(existing: T[] | undefined, incoming: T[] | undefined): T[] {
  if (!incoming?.length) return existing ? [...existing] : [];
  if (!existing?.length) return [...incoming];
  return [...existing, ...incoming];
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
  return { card: delta.card, metricsByServer };
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