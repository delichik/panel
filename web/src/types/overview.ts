import type { NetworkProbeCardData } from './networkProbe';

export type OverviewCardKind = 'cpu' | 'memory' | 'disk' | 'diskIO' | 'network' | 'networkProbe' | 'packageUpdates' | 'containerUpdates' | 'placeholder';
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
  readBytesPerSecond?: number;
  writeBytesPerSecond?: number;
}

export interface OverviewMetricsSeries {
  cpu?: OverviewMetricPoint[];
  memory?: OverviewMetricPoint[];
  diskIo?: OverviewMetricPoint[];
  network?: OverviewMetricPoint[];
  /** 最近一次有效磁盘采样；不参与时间序列、增量刷新时整值替换。 */
  diskCurrent?: OverviewMetricPoint | null;
}

export interface OverviewCardData {
  card: OverviewCardConfiguration;
  metricsByServer: Record<string, OverviewMetricsSeries>;
  /** 服务端降采样桶大小；增量刷新用它判断桶网格是否变化。 */
  bucketSeconds: number;
  /** networkProbe 卡片的节点与最新结果。 */
  probes?: NetworkProbeCardData;
}
