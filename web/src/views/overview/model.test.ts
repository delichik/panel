import { describe, expect, it } from 'vitest';
import type { ServerMetricsSeries } from '@/api/servers';
import type { ApplicationSummaryDto } from '@/types/applications';
import type { NetworkProbeHistoryPoint, NetworkProbeResult } from '@/types/networkProbe';
import type { ServerDto } from '@/types/servers';
import {
  aggregateProbeHistory,
  applicationRatio,
  buildServerCards,
  cpuHistory,
  diskReadHistory,
  diskPercent,
  diskWriteHistory,
  formatBytes,
  formatLatency,
  formatLoad,
  formatPacketLoss,
  formatPercent,
  formatRate,
  hourlyHistory,
  latestMetricsView,
  loadHistory,
  memoryPercent,
  nodeStatusKind,
  nodeStatusTone,
  onlineRateTone,
  percentTone,
  probeLatencyHistory,
  probeLossHistory,
  probeTargetView,
  relativeTimeLabel,
  emptyApplicationSummary,
  summarizeApplications,
  summarizeCapacity,
  summarizeNodeCards,
  summarizeProbeResults,
  valueTone,
} from './model';

function server(overrides: Partial<ServerDto> = {}): ServerDto {
  return {
    id: 'srv-1',
    name: 'edge',
    host: '10.0.0.1',
    port: 22,
    credentialId: 'cred-1',
    reachable: true,
    proxyMode: 'direct',
    tunnelServerId: '',
    tunnelServerName: '',
    socks5Address: '',
    socks5Username: '',
    proxyInvalid: false,
    proxyError: '',
    os: { supported: true },
    ...overrides,
  } as ServerDto;
}

function hourIso(hoursAgo: number, now: number): string {
  return new Date(now - hoursAgo * 60 * 60 * 1000).toISOString();
}

function series(overrides: Partial<ServerMetricsSeries> = {}): ServerMetricsSeries {
  const time = '2026-11-16T07:00:00Z';
  return {
    range: '1d',
    cpu: [{ time, usagePercent: 42.5 }],
    memory: [{ time, usedBytes: 4 * 1024 ** 3, totalBytes: 8 * 1024 ** 3 }],
    diskCurrent: { time: '2026-11-16T06:55:00Z', usedBytes: 60 * 1024 ** 3, totalBytes: 100 * 1024 ** 3 },
    diskIo: [{ time, readBytesPerSecond: 1536, writeBytesPerSecond: 512 }],
    network: [{ time, rxBytesPerSecond: 1, txBytesPerSecond: 1 }],
    load: [{ time, load1: 0.42, load5: 0.3, load15: 0.2 }],
    latest: {
      time,
      cpuUsagePercent: 42.5,
      memoryUsedBytes: 4 * 1024 ** 3,
      memoryTotalBytes: 8 * 1024 ** 3,
      diskUsedBytes: 60 * 1024 ** 3,
      diskTotalBytes: 100 * 1024 ** 3,
      diskSampledAt: '2026-11-16T06:55:00Z',
      load1: 0.42,
      rxBytesPerSecond: 1536,
      txBytesPerSecond: 512,
    },
    ...overrides,
  };
}

function probe(overrides: Partial<NetworkProbeResult> = {}): NetworkProbeResult {
  return {
    sourceServerId: 'edge',
    targetServerId: 'core',
    targetAddress: '10.0.0.2',
    latencyMs: 40,
    packetLossPercent: 0,
    sent: 3,
    received: 3,
    sampledAt: '2026-11-16T07:00:00Z',
    error: '',
    stale: false,
    history: [],
    ...overrides,
  };
}

function historyPoint(time: string, latencyMs: number, packetLossPercent: number): NetworkProbeHistoryPoint {
  return { time, latencyMs, packetLossPercent };
}

describe('overview model', () => {
  it('maps values onto the shared tone thresholds', () => {
    expect(valueTone('cpu', 70)).toBe('success');
    expect(valueTone('cpu', 70.1)).toBe('warning');
    expect(valueTone('load', 2.5)).toBe('danger');
    expect(valueTone('latency', null)).toBeNull();
    expect(percentTone(null)).toBeNull();
    expect(percentTone(41)).toBe('success');
    expect(percentTone(80)).toBe('warning');
    expect(percentTone(96)).toBe('danger');
  });

  it('derives node status from reachability only, independent of metric freshness', () => {
    const now = Date.parse('2026-11-16T07:10:00Z');
    expect(nodeStatusKind(server({ reachable: false }))).toBe('offline');
    expect(nodeStatusKind(server())).toBe('online');
    // 指标缺失/过期不改变在线状态；指标新鲜度单独用于摘要的新鲜指标计数。
    const cards = buildServerCards({
      servers: [server()],
      metricsById: {},
      metricsErrorIds: new Set(['srv-1']),
      probeSourceId: '',
      probeResults: [],
      packageUpdatesById: {},
      imageUpdatesById: {},
      now,
    });
    expect(cards[0]!.status).toBe('online');
    expect(cards[0]!.metricsFresh).toBe(false);
    expect(nodeStatusTone('online')).toBe('success');
    expect(nodeStatusTone('offline')).toBe('danger');
  });

  it('prefers the latest raw sample for current values and keeps the last valid disk sample', () => {
    const view = latestMetricsView(series());
    expect(view.cpuPercent).toBe(42.5);
    expect(memoryPercent(view)).toBe(50);
    expect(diskPercent(view)).toBe(60);
    expect(view.diskSampledAt).toBe('2026-11-16T06:55:00Z');
    expect(view.load1).toBe(0.42);
    expect(view.rxBytesPerSecond).toBe(1536);
    expect(view.txBytesPerSecond).toBe(512);
    expect(view.diskReadBytesPerSecond).toBe(1536);
    expect(view.diskWriteBytesPerSecond).toBe(512);
    expect(view.sampledAt).toBe('2026-11-16T07:00:00Z');

    const fallback = latestMetricsView(series({ latest: undefined, diskCurrent: null }));
    expect(fallback.diskUsedBytes).toBeNull();
    expect(diskPercent(fallback)).toBeNull();
    expect(fallback.sampledAt).toBe('2026-11-16T07:00:00Z');
    expect(latestMetricsView(null).sampledAt).toBeNull();
  });

  it('aggregates arbitrary points into 24 hourly history buckets, keeping gaps', () => {
    const now = Date.parse('2026-11-16T12:30:00Z');
    const values = hourlyHistory([
      { time: hourIso(25, now), value: 90 },
      { time: hourIso(2, now), value: 10 },
      { time: hourIso(2, now), value: 20 },
      { time: hourIso(1, now), value: null },
    ], now);
    expect(values).toHaveLength(24);
    expect(values[21]).toBe(15);
    expect(values[22]).toBeNull();
    expect(values[0]).toBeNull();
  });

  it('builds CPU, load and disk IO history from bucketed metric series', () => {
    const now = Date.parse('2026-11-16T12:30:00Z');
    const h = series({
      cpu: [{ time: hourIso(2, now), usagePercent: 30 }, { time: hourIso(1, now), usagePercent: 50 }],
      load: [{ time: hourIso(1, now), load1: 1.5, load5: 0, load15: 0 }],
      diskIo: [{ time: hourIso(1, now), readBytesPerSecond: 2048, writeBytesPerSecond: 1024 }],
    });
    const cpu = cpuHistory(h, now);
    const load = loadHistory(h, now);
    expect(cpu[21]).toBe(30);
    expect(cpu[22]).toBe(50);
    expect(load[22]).toBe(1.5);
    expect(cpu.filter((value) => value !== null)).toHaveLength(2);
    expect(diskReadHistory(h, now)[22]).toBe(2048);
    expect(diskWriteHistory(h, now)[22]).toBe(1024);
  });

  it('derives per-target and per-source latency/loss history from probe history', () => {
    const now = Date.parse('2026-11-16T12:30:00Z');
    const history = [
      historyPoint(hourIso(2, now), 40, 0),
      historyPoint(hourIso(1, now), 0, 100),
    ];
    expect(probeLatencyHistory(history, now)[21]).toBe(40);
    expect(probeLatencyHistory(history, now)[22]).toBeNull();
    expect(probeLossHistory(history, now)[22]).toBe(100);

    const aggregatedLatency = aggregateProbeHistory([
      probe({ targetServerId: 'a', history }),
      probe({ targetServerId: 'b', history }),
    ], 'latency', now);
    expect(aggregatedLatency[21]).toBe(40);
    expect(aggregatedLatency[22]).toBeNull();
  });

  it('aggregates one probe source by successful latency and weighted packet loss', () => {
    const summary = summarizeProbeResults([
      probe({ targetServerId: 'a', latencyMs: 40, sent: 3, received: 3 }),
      probe({ targetServerId: 'b', latencyMs: 80, sent: 3, received: 2 }),
      probe({ targetServerId: 'c', latencyMs: 0, sent: 3, received: 0, error: 'timeout' }),
    ]);
    expect(summary.latencyMs).toBe(60);
    expect(summary.packetLossPercent).toBeCloseTo((9 - 5) / 9 * 100, 5);
    expect(summary.targetCount).toBe(3);
    expect(summary.failedTargetCount).toBe(1);
    expect(summary.stale).toBe(false);

    expect(summarizeProbeResults([])).toMatchObject({ latencyMs: null, packetLossPercent: null, targetCount: 0, stale: true });
    expect(summarizeProbeResults([probe({ stale: true })]).stale).toBe(true);
  });

  it('treats a probe result with an omitted error field as successful', () => {
    const result = probe({ error: undefined, received: 3 });
    const view = probeTargetView(result);
    expect(view.failed).toBe(false);
    expect(view.latencyMs).toBe(40);
    expect(view.packetLossPercent).toBe(0);

    // 回归：error 被 JSON omitempty 省略时，概览卡片构建不能抛错清空整页 metric。
    const cards = buildServerCards({
      servers: [server({ id: 'edge' }), server({ id: 'core', name: 'core', host: '10.0.0.2' })],
      metricsById: { edge: series(), core: series() },
      metricsErrorIds: new Set(),
      probeSourceId: 'edge',
      probeResults: [result],
      packageUpdatesById: {},
      imageUpdatesById: {},
      now: Date.parse('2026-11-16T07:01:00Z'),
    });
    expect(cards[0]!.metrics.cpuPercent).toBe(42.5);
    expect(cards[1]!.probeAvailable).toBe(true);
  });

  it('builds cards with source aggregate history and target history', () => {
    const now = Date.parse('2026-11-16T07:01:00Z');
    const history = [historyPoint(hourIso(1, now), 55, 1.5)];
    const cards = buildServerCards({
      servers: [server({ id: 'edge' }), server({ id: 'core', name: 'core', host: '10.0.0.2' })],
      metricsById: { edge: series(), core: series({ latest: undefined, cpu: [], memory: [], diskCurrent: null, load: [] }) },
      metricsErrorIds: new Set(['core']),
      probeSourceId: 'edge',
      probeResults: [
        probe({ sourceServerId: 'edge', targetServerId: 'core', latencyMs: 55, packetLossPercent: 1.5, history }),
      ],
      packageUpdatesById: { edge: 3, core: null },
      imageUpdatesById: { edge: 1, core: null },
      now,
    });
    expect(cards).toHaveLength(2);
    const edge = cards[0]!;
    expect(edge.status).toBe('online');
    expect(edge.isProbeSource).toBe(true);
    expect(edge.probeAvailable).toBe(false);
    expect(edge.probeLatencyMs).toBeNull();
    expect(edge.packageUpdates).toBe(3);
    expect(edge.imageUpdates).toBe(1);
    expect(edge.latencyHistory[22]).toBeNull();
    expect(edge.lossHistory[22]).toBeNull();

    const core = cards[1]!;
    expect(core.isProbeSource).toBe(false);
    expect(core.probeAvailable).toBe(true);
    expect(core.probeLatencyMs).toBe(55);
    expect(core.probePacketLossPercent).toBe(1.5);
    expect(core.metricsError).toBe(true);
    expect(core.status).toBe('online');
    expect(core.latencyHistory[22]).toBe(55);
  });

  it('summarizes node cards and formats values for the cards', () => {
    const now = Date.parse('2026-11-16T07:01:00Z');
    const cards = buildServerCards({
      servers: [
        server({ id: 'a' }),
        server({ id: 'b', reachable: false }),
        server({ id: 'c', os: { supported: false } }),
      ],
      metricsById: { a: series() },
      metricsErrorIds: new Set(),
      probeSourceId: 'a',
      probeResults: [],
      packageUpdatesById: {},
      imageUpdatesById: {},
      now,
    });
    expect(summarizeNodeCards(cards)).toEqual({ total: 3, online: 2, offline: 1, fresh: 1, unsupported: 1 });
    expect(formatPercent(42.56, 1)).toBe('42.6%');
    expect(formatPercent(null)).toBe('—');
    expect(formatLatency(144.6)).toBe('145 ms');
    expect(formatPacketLoss(1.7)).toBe('1.7 %');
    expect(formatLoad(0.42)).toBe('0.42');
    expect(formatBytes(4 * 1024 ** 3)).toBe('4.0 GB');
    expect(formatBytes(null)).toBe('—');
    expect(formatRate(1536)).toBe('1.5 KB/s');
    expect(formatRate(null)).toBe('—');
  });

  it('formats relative times as i18n keys so the page can localize them', () => {
    const now = Date.parse('2026-11-16T07:10:00Z');
    expect(relativeTimeLabel('2026-11-16T07:09:40Z', now)).toEqual({ key: 'overviewPage.relativeJustNow' });
    expect(relativeTimeLabel('2026-11-16T07:05:00Z', now)).toEqual({ key: 'overviewPage.relativeMinutes', params: { count: 5 } });
    expect(relativeTimeLabel('2026-11-16T04:10:00Z', now)).toEqual({ key: 'overviewPage.relativeHours', params: { count: 3 } });
    expect(relativeTimeLabel('2026-11-13T07:10:00Z', now)).toEqual({ key: 'overviewPage.relativeDays', params: { count: 3 } });
    expect(relativeTimeLabel(null, now)).toBeNull();
  });

  it('summarizes application count and states for the overview card', () => {
    const apps = [
      { enabled: true, deployingInstanceCount: 0, abnormalInstanceCount: 0 },
      { enabled: false, deployingInstanceCount: 0, abnormalInstanceCount: 0 },
      { enabled: true, deployingInstanceCount: 1, abnormalInstanceCount: 0 },
      { enabled: true, deployingInstanceCount: 0, abnormalInstanceCount: 2 },
      { enabled: true, deployingInstanceCount: 0, abnormalInstanceCount: 0, lastError: 'deploy failed' },
    ] as ApplicationSummaryDto[];
    expect(summarizeApplications(apps, 5)).toEqual({
      total: 5,
      enabled: 4,
      disabled: 1,
      deploying: 1,
      attention: 2,
      available: true,
    });
    expect(summarizeApplications([], 0)).toEqual({ total: 0, enabled: 0, disabled: 0, deploying: 0, attention: 0, available: true });
    expect(emptyApplicationSummary().available).toBe(false);
  });

  it('derives the online-rate ring tone from offline nodes only', () => {
    expect(onlineRateTone({ total: 3, online: 3, offline: 0, fresh: 3, unsupported: 0 })).toBe('success');
    expect(onlineRateTone({ total: 3, online: 2, offline: 1, fresh: 3, unsupported: 1 })).toBe('warning');
  });

  it('derives an exclusive enabled/disabled application ratio for the summary bar', () => {
    expect(applicationRatio({ total: 4, enabled: 3, disabled: 1, deploying: 1, attention: 1, available: true }))
      .toEqual({ enabledPercent: 75, disabledPercent: 25 });
    expect(applicationRatio(emptyApplicationSummary())).toEqual({ enabledPercent: null, disabledPercent: null });
    expect(applicationRatio({ total: 0, enabled: 0, disabled: 0, deploying: 0, attention: 0, available: true }))
      .toEqual({ enabledPercent: null, disabledPercent: null });
  });

  it('summarizes capacity from online nodes with valid samples only', () => {
    const now = Date.parse('2026-11-16T07:10:00Z');
    const second = series();
    second.latest = {
      ...second.latest!,
      cpuUsagePercent: 50,
      memoryUsedBytes: 2 * 1024 ** 3,
      memoryTotalBytes: 4 * 1024 ** 3,
      diskUsedBytes: 20 * 1024 ** 3,
      diskTotalBytes: 100 * 1024 ** 3,
    };
    const cards = buildServerCards({
      servers: [server({ id: 'a' }), server({ id: 'b' }), server({ id: 'offline', reachable: false })],
      metricsById: { a: series(), b: second, offline: series() },
      metricsErrorIds: new Set(),
      probeSourceId: '',
      probeResults: [],
      packageUpdatesById: {},
      imageUpdatesById: {},
      now,
    });
    const capacity = summarizeCapacity(cards);
    expect(capacity.sampleNodes).toBe(2);
    expect(capacity.cpuPercent).toBeCloseTo(46.25, 5);
    expect(capacity.memoryUsedBytes).toBe(6 * 1024 ** 3);
    expect(capacity.memoryTotalBytes).toBe(12 * 1024 ** 3);
    expect(capacity.memoryPercent).toBe(50);
    expect(capacity.diskUsedBytes).toBe(80 * 1024 ** 3);
    expect(capacity.diskTotalBytes).toBe(200 * 1024 ** 3);
    expect(capacity.diskPercent).toBe(40);

    // 无样本不得伪造 0%：所有值保持 null，页面显示 — 与空环。
    const empty = buildServerCards({
      servers: [server()],
      metricsById: {},
      metricsErrorIds: new Set(),
      probeSourceId: '',
      probeResults: [],
      packageUpdatesById: {},
      imageUpdatesById: {},
      now,
    });
    expect(summarizeCapacity(empty)).toEqual({
      sampleNodes: 0,
      cpuPercent: null,
      memoryUsedBytes: null,
      memoryTotalBytes: null,
      memoryPercent: null,
      diskUsedBytes: null,
      diskTotalBytes: null,
      diskPercent: null,
    });
  });
});
