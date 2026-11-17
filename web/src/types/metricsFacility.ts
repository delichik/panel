/** 指标设施：决定哪些节点采集指标、采集节拍、保留天数与清理计划。 */
export type MetricsFacilityMode = 'all' | 'selected';
export type MetricsCleanupSchedule = 'hourly' | 'daily' | 'weekly';

export interface MetricsFacilityConfig {
  mode: MetricsFacilityMode;
  serverIds: string[];
  retentionDays: number;
  collectionIntervalSeconds: number;
  cleanupSchedule: MetricsCleanupSchedule;
  version: number;
  updatedAt: string;
}

export interface MetricsFacilitySaveInput {
  mode: MetricsFacilityMode;
  serverIds: string[];
  retentionDays: number;
  collectionIntervalSeconds: number;
  cleanupSchedule: MetricsCleanupSchedule;
}
