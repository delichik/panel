import { describe, expect, it } from 'vitest';
import { cardHasData, overviewRisks, summarizeOverview } from './model';
import type { OverviewDto } from '@/types/overview';

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

  it('distinguishes empty metric cards from message cards', () => {
    expect(cardHasData({ id: 'cpu', kind: 'cpu', width: 3, height: 2, range: '1h', networkDirection: 'both', serverIds: [] }, undefined)).toBe(false);
    expect(cardHasData({ id: 'pkg', kind: 'packageUpdates', width: 3, height: 2, range: '1d', networkDirection: 'both', serverIds: [] }, undefined)).toBe(true);
  });
});
