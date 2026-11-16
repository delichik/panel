import { describe, expect, it } from 'vitest';
import { cardHasData, deriveCardView, deriveDiskCardView, deriveNetworkProbeCardView, mergeCardData, mergeMetricPoints, metricPoints, metricValueOf, overviewRisks, summarizeOverview, trimCardDataToRange } from './model';
import type { OverviewCardConfiguration, OverviewCardData, OverviewDto } from '@/types/overview';
import type { NetworkProbeCardData } from '@/types/networkProbe';

const overview: OverviewDto = {
  servers: [
    { id: 'srv-1', name: 'edge', host: '10.0.0.1', supported: true, reachable: true, metricsFresh: true, packageUpdateCount: 0 },
    { id: 'srv-2', name: 'core', host: '10.0.0.2', supported: true, reachable: false, metricsFresh: false, packageUpdateCount: 7 },
  ],
};

function cpuCard(serverIds: string[] = []): OverviewCardConfiguration {
  return { id: 'card-cpu', kind: 'cpu', width: 3, height: 2, range: '1h', networkDirection: 'both', serverIds };
}

describe('overview model', () => {
  it('summarizes health, metric freshness, and package pressure', () => {
    expect(summarizeOverview(overview)).toEqual({ total: 2, reachable: 1, supported: 2, fresh: 1, updates: 7 });
  });

  it('builds a risk queue with navigation targets', () => {
    expect(overviewRisks(overview.servers)).toEqual([
      { id: 'srv-2:reach', tone: 'danger', title: 'core', description: 'overviewPage.riskUnreachable', to: '/servers?server=srv-2' },
      { id: 'srv-2:packages', tone: 'info', title: 'core', description: 'overviewPage.riskPackages', to: '/servers?server=srv-2&tab=packages' },
    ]);
  });

  it('appends strictly newer points when merging auto-refresh deltas', () => {
    const existing: OverviewCardData = {
      card: cpuCard(['srv-1']),
      bucketSeconds: 10,
      metricsByServer: {
        'srv-1': { cpu: [{ time: '2026-08-01T08:00:00.000Z', usagePercent: 10 }] },
        'srv-2': { cpu: [{ time: '2026-08-01T08:00:00.000Z', usagePercent: 20 }] },
      },
    };
    const delta: OverviewCardData = {
      card: existing.card,
      bucketSeconds: 10,
      metricsByServer: {
        'srv-1': { cpu: [{ time: '2026-08-01T08:00:05.000Z', usagePercent: 12 }] },
      },
    };
    const merged = mergeCardData(existing, delta);
    expect(merged.metricsByServer['srv-1'].cpu?.map((point) => point.time)).toEqual(['2026-08-01T08:00:00.000Z', '2026-08-01T08:00:05.000Z']);
    expect(merged.metricsByServer['srv-1'].cpu?.[1].usagePercent).toBe(12);
    expect(merged.metricsByServer['srv-2'].cpu).toHaveLength(1);
  });

  it('replaces trailing buckets when the delta recomputes the open bucket', () => {
    const existing: OverviewCardData = {
      card: cpuCard(),
      bucketSeconds: 300,
      metricsByServer: {
        'srv-1': {
          cpu: [
            { time: '2026-08-01T08:00:00.000Z', usagePercent: 10 },
            { time: '2026-08-01T08:05:00.000Z', usagePercent: 20 },
          ],
        },
      },
    };
    const delta: OverviewCardData = {
      card: existing.card,
      bucketSeconds: 300,
      metricsByServer: {
        'srv-1': {
          cpu: [
            { time: '2026-08-01T08:05:00.000Z', usagePercent: 25 },
            { time: '2026-08-01T08:10:00.000Z', usagePercent: 30 },
          ],
        },
      },
    };
    const merged = mergeCardData(existing, delta);
    expect(merged.metricsByServer['srv-1'].cpu?.map((point) => [point.time, point.usagePercent])).toEqual([
      ['2026-08-01T08:00:00.000Z', 10],
      ['2026-08-01T08:05:00.000Z', 25],
      ['2026-08-01T08:10:00.000Z', 30],
    ]);
  });

  it('keeps existing series untouched when a delta carries no points', () => {
    const existing: OverviewCardData = {
      card: { id: 'card-network', kind: 'network', width: 6, height: 2, range: '1h', networkDirection: 'both', serverIds: [] },
      bucketSeconds: 30,
      metricsByServer: {
        'srv-1': { network: [{ time: '2026-08-01T08:00:00.000Z', rxBytesPerSecond: 100, txBytesPerSecond: 50 }] },
      },
    };
    const delta: OverviewCardData = {
      card: existing.card,
      bucketSeconds: 30,
      metricsByServer: { 'srv-1': { network: [] } },
    };
    const merged = mergeCardData(existing, delta);
    expect(merged.metricsByServer['srv-1'].network).toHaveLength(1);
  });

  it('drops points outside the card range when trimming', () => {
    const now = new Date('2026-08-01T08:00:00.000Z');
    const data: OverviewCardData = {
      card: cpuCard(),
      bucketSeconds: 10,
      metricsByServer: {
        'srv-1': {
          cpu: [
            { time: '2026-08-01T06:30:00.000Z', usagePercent: 1 },
            { time: '2026-08-01T07:00:00.000Z', usagePercent: 2 },
            { time: '2026-08-01T07:30:00.000Z', usagePercent: 3 },
          ],
        },
      },
    };
    const trimmed = trimCardDataToRange(data, now);
    expect(trimmed.metricsByServer['srv-1'].cpu?.map((point) => point.time)).toEqual(['2026-08-01T07:00:00.000Z', '2026-08-01T07:30:00.000Z']);
  });

  it('appends new points while removing an equal amount of expired points', () => {
    const now = new Date('2026-08-01T08:00:05.000Z');
    const existing: OverviewCardData = {
      card: cpuCard(),
      bucketSeconds: 10,
      metricsByServer: {
        'srv-1': {
          cpu: [
            { time: '2026-08-01T07:00:00.000Z', usagePercent: 1 },
            { time: '2026-08-01T08:00:00.000Z', usagePercent: 2 },
          ],
        },
      },
    };
    const delta: OverviewCardData = {
      card: existing.card,
      bucketSeconds: 10,
      metricsByServer: {
        'srv-1': { cpu: [{ time: '2026-08-01T08:00:05.000Z', usagePercent: 3 }] },
      },
    };
    const merged = trimCardDataToRange(mergeCardData(existing, delta), now);
    expect(merged.metricsByServer['srv-1'].cpu?.map((point) => point.time)).toEqual(['2026-08-01T08:00:00.000Z', '2026-08-01T08:00:05.000Z']);
  });

  it('merges point arrays without mutating the input arrays', () => {
    const existing = [{ time: '2026-08-01T08:00:00.000Z', usagePercent: 10 }];
    const incoming = [{ time: '2026-08-01T08:00:05.000Z', usagePercent: 12 }];
    const merged = mergeMetricPoints(existing, incoming);
    expect(merged).toHaveLength(2);
    expect(existing).toHaveLength(1);
    expect(incoming).toHaveLength(1);
  });

  it('distinguishes empty metric cards from message cards', () => {
    expect(cardHasData({ id: 'cpu', kind: 'cpu', width: 3, height: 2, range: '1h', networkDirection: 'both', serverIds: [] }, undefined)).toBe(false);
    expect(cardHasData({ id: 'pkg', kind: 'packageUpdates', width: 3, height: 2, range: '1d', networkDirection: 'both', serverIds: [] }, undefined)).toBe(true);
  });

  it('aligns multiple servers on a shared timeline and aggregates summary values', () => {
    const data: OverviewCardData = {
      card: cpuCard(),
      bucketSeconds: 300,
      metricsByServer: {
        'srv-1': {
          cpu: [
            { time: '2026-08-01T08:00:00.000Z', usagePercent: 10 },
            { time: '2026-08-01T08:05:00.000Z', usagePercent: 20 },
          ],
        },
        'srv-2': {
          cpu: [
            { time: '2026-08-01T08:05:00.000Z', usagePercent: 40 },
            { time: '2026-08-01T08:10:00.000Z', usagePercent: 60 },
          ],
        },
      },
    };
    const view = deriveCardView(data.card, data, (serverId) => `name-${serverId}`);
    expect(view.labels).toEqual(['2026-08-01T08:00:00.000Z', '2026-08-01T08:05:00.000Z', '2026-08-01T08:10:00.000Z']);
    expect(view.series).toEqual([
      { id: 'srv-1', name: 'name-srv-1', values: [10, 20, null] },
      { id: 'srv-2', name: 'name-srv-2', values: [null, 40, 60] },
    ]);
    expect(view.latestValue).toBe(60);
    expect(view.peakValue).toBe(60);
  });

  it('derives network values per direction and returns empty views without data', () => {
    const card: OverviewCardConfiguration = { id: 'card-net', kind: 'network', width: 6, height: 2, range: '1h', networkDirection: 'rx', serverIds: [] };
    const data: OverviewCardData = {
      card,
      bucketSeconds: 10,
      metricsByServer: {
        'srv-1': { network: [{ time: '2026-08-01T08:00:00.000Z', rxBytesPerSecond: 100, txBytesPerSecond: 50 }] },
      },
    };
    const view = deriveCardView(card, data, (serverId) => serverId);
    expect(view.series[0].values).toEqual([100]);
    expect(deriveCardView(card, undefined, (serverId) => serverId)).toEqual({ labels: [], series: [], latestValue: null, peakValue: null });
  });

  it('derives large card data without quadratic lookups', () => {
    const card = cpuCard();
    const metricsByServer: OverviewCardData['metricsByServer'] = {};
    for (let server = 0; server < 30; server += 1) {
      metricsByServer[`srv-${server}`] = {
        cpu: Array.from({ length: 1200 }, (_, index) => ({
          time: new Date(Date.UTC(2026, 7, 1, 0, 0, index * 5)).toISOString(),
          usagePercent: (server + index) % 100,
        })),
      };
    }
    const view = deriveCardView(card, { card, bucketSeconds: 5, metricsByServer }, (serverId) => serverId);
    expect(view.labels).toHaveLength(1200);
    expect(view.series).toHaveLength(30);
    expect(view.series.every((series) => series.values.length === 1200)).toBe(true);
    expect(view.latestValue).not.toBeNull();
  });

  it('derives disk IO series as a combined read+write rate and never charts disk usage', () => {
    const diskIoCard: OverviewCardConfiguration = { id: 'card-disk-io', kind: 'diskIO', width: 6, height: 2, range: '1h', networkDirection: 'both', serverIds: [] };
    const data: OverviewCardData = {
      card: diskIoCard,
      bucketSeconds: 10,
      metricsByServer: {
        'srv-1': {
          diskIo: [{ time: '2026-08-01T08:00:00.000Z', readBytesPerSecond: 300, writeBytesPerSecond: 200 }],
        },
      },
    };
    expect(metricPoints(diskIoCard, data.metricsByServer['srv-1'])).toHaveLength(1);
    expect(metricValueOf(diskIoCard, data.metricsByServer['srv-1'].diskIo![0])).toBe(500);
    expect(deriveCardView(diskIoCard, data, (serverId) => serverId).series[0].values).toEqual([500]);

    // 磁盘卡没有时序数据，只有当前值快照。
    const diskCard: OverviewCardConfiguration = { ...diskIoCard, id: 'card-disk', kind: 'disk' };
    expect(metricPoints(diskCard, { diskCurrent: { time: '2026-08-01T08:00:00.000Z', usedBytes: 50, totalBytes: 100 } })).toEqual([]);
  });

  it('replaces diskCurrent on deltas instead of appending and keeps it out of range trimming', () => {
    const card: OverviewCardConfiguration = { id: 'card-disk', kind: 'disk', width: 2, height: 2, range: '1h', networkDirection: 'both', serverIds: [] };
    const existing: OverviewCardData = {
      card,
      bucketSeconds: 0,
      metricsByServer: {
        'srv-1': { diskCurrent: { time: '2026-08-01T06:00:00.000Z', usedBytes: 40, totalBytes: 100 } },
      },
    };
    const delta: OverviewCardData = {
      card,
      bucketSeconds: 0,
      metricsByServer: {
        'srv-1': { diskCurrent: { time: '2026-08-01T08:00:00.000Z', usedBytes: 55, totalBytes: 100 } },
      },
    };
    const merged = mergeCardData(existing, delta);
    expect(merged.metricsByServer['srv-1'].diskCurrent).toEqual({ time: '2026-08-01T08:00:00.000Z', usedBytes: 55, totalBytes: 100 });
    // 旧样本早于卡片范围，但仍作为「最近一次有效值」保留。
    const trimmed = trimCardDataToRange(merged, new Date('2026-08-01T09:00:00.000Z'));
    expect(trimmed.metricsByServer['srv-1'].diskCurrent?.usedBytes).toBe(55);
  });

  it('keeps an existing diskCurrent when a delta omits the field', () => {
    const card: OverviewCardConfiguration = { id: 'card-disk', kind: 'disk', width: 2, height: 2, range: '1h', networkDirection: 'both', serverIds: [] };
    const existing: OverviewCardData = {
      card,
      bucketSeconds: 0,
      metricsByServer: { 'srv-1': { diskCurrent: { time: '2026-08-01T08:00:00.000Z', usedBytes: 40, totalBytes: 100 } } },
    };
    const delta: OverviewCardData = { card, bucketSeconds: 0, metricsByServer: { 'srv-1': {} } };
    expect(mergeCardData(existing, delta).metricsByServer['srv-1'].diskCurrent?.usedBytes).toBe(40);
  });

  it('summarizes the latest disk usage across servers for the value-only card', () => {
    const card: OverviewCardConfiguration = { id: 'card-disk', kind: 'disk', width: 2, height: 2, range: '1h', networkDirection: 'both', serverIds: [] };
    const data: OverviewCardData = {
      card,
      bucketSeconds: 0,
      metricsByServer: {
        'srv-1': { diskCurrent: { time: '2026-08-01T08:05:00.000Z', usedBytes: 50, totalBytes: 100 } },
        'srv-2': { diskCurrent: { time: '2026-08-01T08:10:00.000Z', usedBytes: 30, totalBytes: 100 } },
        'srv-3': { diskCurrent: null },
      },
    };
    const view = deriveDiskCardView(data);
    expect(view.serverCount).toBe(2);
    expect(view.percent).toBe(40);
    expect(view.usedBytes).toBe(80);
    expect(view.totalBytes).toBe(200);
    expect(view.sampledAt).toBe('2026-08-01T08:10:00.000Z');
    expect(deriveDiskCardView(undefined)).toEqual({ percent: null, usedBytes: null, totalBytes: null, sampledAt: '', serverCount: 0 });
  });

  it('summarizes network probe nodes, average latency, max loss, and worst results', () => {
    const probes: NetworkProbeCardData = {
      nodes: [
        { serverId: 'srv-1', name: 'edge', address: '10.0.0.1' },
        { serverId: 'srv-2', name: 'core', address: '10.0.0.2' },
      ],
      results: [
        { sourceServerId: 'srv-1', targetServerId: 'srv-2', targetAddress: '10.0.0.2', latencyMs: 10, packetLossPercent: 0, sent: 3, received: 3, sampledAt: '2026-11-16T05:00:00.000Z', error: '', stale: false },
        { sourceServerId: 'srv-2', targetServerId: 'srv-1', targetAddress: '10.0.0.1', latencyMs: 30, packetLossPercent: 33, sent: 3, received: 2, sampledAt: '2026-11-16T05:00:00.000Z', error: '', stale: false },
        { sourceServerId: 'srv-1', targetServerId: 'srv-3', targetAddress: '10.0.0.3', latencyMs: 0, packetLossPercent: 100, sent: 3, received: 0, sampledAt: '2026-11-16T05:00:00.000Z', error: 'no route to host', stale: false },
      ],
      updatedAt: '2026-11-16T05:00:00.000Z',
    };
    const view = deriveNetworkProbeCardView(probes);
    expect(view.nodeCount).toBe(2);
    expect(view.resultCount).toBe(3);
    expect(view.averageLatencyMs).toBe(20);
    expect(view.maxPacketLossPercent).toBe(100);
    // 失败（srv-3）> 高丢包目标（srv-1，33%）> 正常目标（srv-2）。
    expect(view.worst.map((result) => result.targetServerId)).toEqual(['srv-3', 'srv-1', 'srv-2']);
    expect(view.updatedAt).toBe('2026-11-16T05:00:00.000Z');
    expect(deriveNetworkProbeCardView(undefined)).toMatchObject({ nodeCount: 0, resultCount: 0, averageLatencyMs: null, maxPacketLossPercent: null, worst: [] });
  });

  it('keeps probe snapshots through delta merges and range trimming', () => {
    const card: OverviewCardConfiguration = { id: 'card-probe', kind: 'networkProbe', width: 3, height: 2, range: '1h', networkDirection: 'both', serverIds: [] };
    const probes: NetworkProbeCardData = { nodes: [{ serverId: 'srv-1', name: 'edge', address: '10.0.0.1' }], results: [], updatedAt: '2026-11-16T05:00:00.000Z' };
    const existing: OverviewCardData = { card, bucketSeconds: 0, metricsByServer: {}, probes };
    const delta: OverviewCardData = { card, bucketSeconds: 0, metricsByServer: {} };
    expect(mergeCardData(existing, delta).probes).toEqual(probes);
    expect(trimCardDataToRange(existing, new Date('2026-11-16T06:00:00.000Z')).probes).toEqual(probes);
    expect(cardHasData(card, existing)).toBe(true);
    expect(cardHasData(card, { card, bucketSeconds: 0, metricsByServer: {} })).toBe(false);
  });
});
