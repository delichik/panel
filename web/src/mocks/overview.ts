import type { OverviewCardConfiguration, OverviewCardConfigurationSet, OverviewCardData, OverviewDto, OverviewMetricsSeries } from '@/types/overview';
import type { ServerDto } from '@/types/servers';

function nowIso(): string {
  return new Date().toISOString();
}

export function overviewFromServers(servers: ServerDto[]): OverviewDto {
  return {
    servers: servers.map((server, index) => ({
      id: server.id,
      name: server.name,
      host: server.host,
      supported: server.os?.supported !== false,
      reachable: server.reachable,
      metricsFresh: server.reachable && index !== 2,
      packageUpdateCount: Number(server.traits?.['mock.package_updates'] ?? (index === 1 ? 14 : index === 3 ? 3 : 0)),
      loadAverage: server.loadAverage ?? (index === 1 ? '3.80 3.62 3.44' : '0.42 0.38 0.36'),
      lastMetricsAt: index === 2 ? null : nowIso(),
      lastPackageRefreshAt: nowIso(),
    })),
  };
}

export let overviewCards: OverviewCardConfiguration[] = [
  card('card-cpu', 'cpu', '1h', 3, 2),
  card('card-memory', 'memory', '1h', 3, 2),
  card('card-disk', 'disk', '6h', 2, 2),
  card('card-network', 'network', '1h', 6, 2),
  card('card-packages', 'packageUpdates', '1d', 2, 1),
  card('card-containers', 'containerUpdates', '1d', 2, 1),
];

export function getOverviewCards(): OverviewCardConfigurationSet {
  return { cards: overviewCards.map((item) => ({ ...item, serverIds: [...item.serverIds] })) };
}

export function setOverviewCards(input: OverviewCardConfigurationSet): OverviewCardConfigurationSet {
  overviewCards = input.cards.map((item) => ({ ...item, serverIds: [...item.serverIds] }));
  return getOverviewCards();
}

export function getOverviewCardData(cardId: string, servers: ServerDto[], since?: string): OverviewCardData | null {
  const found = overviewCards.find((item) => item.id === cardId);
  if (!found) return null;
  const kind = found.kind;
  if (kind !== 'cpu' && kind !== 'memory' && kind !== 'disk' && kind !== 'network') {
    return { card: { ...found, serverIds: [...found.serverIds] }, metricsByServer: {}, bucketSeconds: 0 };
  }
  const selected = new Set(found.serverIds);
  const targetServers = servers.filter((server) => selected.size === 0 || selected.has(server.id));
  const bucketSeconds = mockBucketSeconds(found.range);
  const metricsByServer = Object.fromEntries(targetServers.map((server, index) => [server.id, mockCardSeries(kind, index, bucketSeconds, since)]));
  return { card: { ...found, serverIds: [...found.serverIds] }, metricsByServer, bucketSeconds };
}

function mockBucketSeconds(range: OverviewCardConfiguration['range']): number {
  const rangeSeconds: Record<OverviewCardConfiguration['range'], number> = {
    '1h': 60 * 60,
    '6h': 6 * 60 * 60,
    '1d': 24 * 60 * 60,
    '7d': 7 * 24 * 60 * 60,
  };
  return Math.max(10, Math.round(rangeSeconds[range] / 120));
}

// 与后端一致：只返回卡片对应的序列，since 先向下对齐到桶起点。
function mockCardSeries(
  kind: 'cpu' | 'memory' | 'disk' | 'network',
  seed: number,
  bucketSeconds: number,
  since: string | undefined,
): OverviewMetricsSeries {
  const end = Math.floor(Date.now() / 1000 / bucketSeconds) * bucketSeconds;
  const sinceSeconds = since ? Math.floor(Date.parse(since) / 1000 / bucketSeconds) * bucketSeconds : null;
  const points = Array.from({ length: 120 }, (_, index) => ({
    time: new Date((end - (119 - index) * bucketSeconds) * 1000).toISOString(),
    index: index % 24,
  })).filter((point) => sinceSeconds === null || Date.parse(point.time) >= sinceSeconds * 1000);
  if (kind === 'cpu') return { cpu: points.map((point) => ({ time: point.time, usagePercent: 18 + seed * 12 + point.index * 2 })) };
  if (kind === 'memory') return { memory: points.map((point) => ({ time: point.time, usedBytes: (2 + seed + point.index / 10) * 1024 ** 3, totalBytes: 8 * 1024 ** 3 })) };
  if (kind === 'disk') return { disk: points.map((point) => ({ time: point.time, usedBytes: (35 + seed * 8 + point.index) * 1024 ** 3, totalBytes: 100 * 1024 ** 3 })) };
  return {
    network: points.map((point) => ({
      time: point.time,
      rxBytesPerSecond: (seed + 1) * 1024 * (12 + point.index),
      txBytesPerSecond: (seed + 1) * 1024 * (7 + point.index),
    })),
  };
}

function card(id: string, kind: OverviewCardConfiguration['kind'], range: OverviewCardConfiguration['range'], width: number, height: number): OverviewCardConfiguration {
  return { id, kind, width, height, range, networkDirection: 'both', serverIds: [] };
}
