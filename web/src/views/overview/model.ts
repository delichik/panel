import type { OverviewCardConfiguration, OverviewCardData, OverviewDto, OverviewServerSummary } from '@/types/overview';

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
