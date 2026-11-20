/** 指标设施：决定哪些节点采集指标、采集节拍、保留天数与清理计划。 */
export type MetricsFacilityMode = 'all' | 'selected';
export type MetricsCleanupSchedule = 'hourly' | 'daily' | 'weekly';

/** 指标图表警告阈值；缺省表示该指标不启用阈值。 */
export interface MetricsWarningThresholds {
  cpuUsagePercent?: number;
  memoryUsagePercent?: number;
  diskReadBytesPerSecond?: number;
  diskWriteBytesPerSecond?: number;
  networkRxBytesPerSecond?: number;
  networkTxBytesPerSecond?: number;
}

export interface MetricsFacilityConfig {
  mode: MetricsFacilityMode;
  serverIds: string[];
  retentionDays: number;
  collectionIntervalSeconds: number;
  cleanupSchedule: MetricsCleanupSchedule;
  thresholds: MetricsWarningThresholds;
  version: number;
  updatedAt: string;
}

export interface MetricsFacilitySaveInput {
  mode: MetricsFacilityMode;
  serverIds: string[];
  retentionDays: number;
  collectionIntervalSeconds: number;
  cleanupSchedule: MetricsCleanupSchedule;
  thresholds: MetricsWarningThresholds;
}
