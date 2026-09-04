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
  const selected = new Set(found.serverIds);
  const targetServers = servers.filter((server) => selected.size === 0 || selected.has(server.id));
  const metricsByServer = Object.fromEntries(targetServers.map((server, index) => {
    const full = series(index);
    return [server.id, {
      cpu: after(full.cpu ?? [], since),
      memory: after(full.memory ?? [], since),
      disk: after(full.disk ?? [], since),
      network: after(full.network ?? [], since),
    }];
  }));
  return { card: { ...found, serverIds: [...found.serverIds] }, metricsByServer };
}

function after<T extends { time: string }>(points: T[], since: string | undefined): T[] {
  if (!since) return points;
  return points.filter((point) => point.time > since);
}

function card(id: string, kind: OverviewCardConfiguration['kind'], range: OverviewCardConfiguration['range'], width: number, height: number): OverviewCardConfiguration {
  return { id, kind, width, height, range, networkDirection: 'both', serverIds: [] };
}

function series(seed: number): OverviewMetricsSeries {
  const end = Date.now();
  const points = Array.from({ length: 360 }, (_, index) => ({
    time: new Date(end - (359 - index) * 5 * 1000).toISOString(),
    index: index % 24,
  }));
  return {
    cpu: points.map((point) => ({ time: point.time, usagePercent: 18 + seed * 12 + point.index * 2 })),
    memory: points.map((point) => ({ time: point.time, usedBytes: (2 + seed + point.index / 10) * 1024 ** 3, totalBytes: 8 * 1024 ** 3 })),
    disk: points.map((point) => ({ time: point.time, usedBytes: (35 + seed * 8 + point.index) * 1024 ** 3, totalBytes: 100 * 1024 ** 3 })),
    network: points.map((point) => ({
      time: point.time,
      rxBytesPerSecond: (seed + 1) * 1024 * (12 + point.index),
      txBytesPerSecond: (seed + 1) * 1024 * (7 + point.index),
    })),
  };
}
