import { describe, expect, it } from 'vitest';
import { cardHasData, mergeCardData, mergeMetricPoints, overviewRisks, summarizeOverview, trimCardDataToRange } from './model';
import type { OverviewCardData, OverviewDto } from '@/types/overview';

const overview: OverviewDto = {
  servers: [
    { id: 'srv-1', name: 'edge', host: '10.0.0.1', supported: true, reachable: true, metricsFresh: true, packageUpdateCount: 0 },
    { id: 'srv-2', name: 'core', host: '10.0.0.2', supported: true, reachable: false, metricsFresh: false, packageUpdateCount: 7 },
  ],
};

describe('overview model', () => {
  it('summarizes health, metric freshness, and package pressure', () => {
    expect(summarizeOverview(overview)).toEqual({ total: 2, reachable: 1, supported: 2, fresh: 1, updates: 7 });
  });

  it('builds a risk queue with navigation targets', () => {
    expect(overviewRisks(overview.servers)).toEqual([
      { id: 'srv-2:reach', tone: 'danger', title: 'core', description: 'overviewPage.riskUnreachable', to: '/servers?server=srv-2' },
      { id: 'srv-2:packages', tone: 'info', title: 'core', description: 'overviewPage.riskPackages', to: '/resources/packages' },
    ]);
  });

  it('appends strictly newer points when merging auto-refresh deltas', () => {
    const existing: OverviewCardData = {
      card: { id: 'card-cpu', kind: 'cpu', width: 3, height: 2, range: '1h', networkDirection: 'both', serverIds: ['srv-1'] },
      metricsByServer: {
        'srv-1': { cpu: [{ time: '2026-08-01T08:00:00.000Z', usagePercent: 10 }] },
        'srv-2': { cpu: [{ time: '2026-08-01T08:00:00.000Z', usagePercent: 20 }] },
      },
    };
    const delta: OverviewCardData = {
      card: existing.card,
      metricsByServer: {
        'srv-1': { cpu: [{ time: '2026-08-01T08:00:05.000Z', usagePercent: 12 }] },
      },
    };
    const merged = mergeCardData(existing, delta);
    expect(merged.metricsByServer['srv-1'].cpu?.map((point) => point.time)).toEqual(['2026-08-01T08:00:00.000Z', '2026-08-01T08:00:05.000Z']);
    expect(merged.metricsByServer['srv-1'].cpu?.[1].usagePercent).toBe(12);
    expect(merged.metricsByServer['srv-2'].cpu).toHaveLength(1);
  });

  it('keeps existing series untouched when a delta carries no points', () => {
    const existing: OverviewCardData = {
      card: { id: 'card-network', kind: 'network', width: 6, height: 2, range: '1h', networkDirection: 'both', serverIds: [] },
      metricsByServer: {
        'srv-1': { network: [{ time: '2026-08-01T08:00:00.000Z', rxBytesPerSecond: 100, txBytesPerSecond: 50 }] },
      },
    };
    const delta: OverviewCardData = {
      card: existing.card,
      metricsByServer: { 'srv-1': { network: [] } },
    };
    const merged = mergeCardData(existing, delta);
    expect(merged.metricsByServer['srv-1'].network).toHaveLength(1);
  });

  it('drops points outside the card range when trimming', () => {
    const now = new Date('2026-08-01T08:00:00.000Z');
    const data: OverviewCardData = {
      card: { id: 'card-cpu', kind: 'cpu', width: 3, height: 2, range: '1h', networkDirection: 'both', serverIds: [] },
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
      card: { id: 'card-cpu', kind: 'cpu', width: 3, height: 2, range: '1h', networkDirection: 'both', serverIds: [] },
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
});
