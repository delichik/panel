export type OverviewCardKind = 'cpu' | 'memory' | 'disk' | 'network' | 'packageUpdates' | 'containerUpdates' | 'placeholder';
export type OverviewCardRange = '1h' | '6h' | '1d' | '7d';
export type OverviewCardNetworkDirection = 'rx' | 'tx' | 'both';

export interface OverviewServerSummary {
  id: string;
  name: string;
  host: string;
  supported: boolean;
  reachable: boolean;
  metricsFresh: boolean;
  packageUpdateCount: number;
  loadAverage?: string;
  lastMetricsAt?: string | null;
  lastPackageRefreshAt?: string | null;
}

export interface OverviewDto {
  servers: OverviewServerSummary[];
}

export interface OverviewCardConfiguration {
  id: string;
  kind: OverviewCardKind;
  width: number;
  height: number;
  range: OverviewCardRange;
  networkDirection: OverviewCardNetworkDirection;
  serverIds: string[];
}

export interface OverviewCardConfigurationSet {
  cards: OverviewCardConfiguration[];
}

export interface OverviewMetricPoint {
  time: string;
  usagePercent?: number;
  usedBytes?: number;
  totalBytes?: number;
  rxBytesPerSecond?: number;
  txBytesPerSecond?: number;
}

export interface OverviewMetricsSeries {
  cpu?: OverviewMetricPoint[];
  memory?: OverviewMetricPoint[];
  disk?: OverviewMetricPoint[];
  network?: OverviewMetricPoint[];
}

export interface OverviewCardData {
  card: OverviewCardConfiguration;
  metricsByServer: Record<string, OverviewMetricsSeries>;
}
