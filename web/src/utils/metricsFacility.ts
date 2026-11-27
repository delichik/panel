import type { MetricsCleanupSchedule, MetricsFacilityMode, MetricsWarningThresholds } from '@/types/metricsFacility';

/**
 * 指标设施契约：默认值、模式/清理计划枚举与数字校验在页面、Mock 和测试间共用，
 * 避免界面提示与 Mock 校验各自漂移（契约见 tmp/metrics-facility-spec.md §2）。
 */
export const metricsFacilityDefaults = {
  mode: 'all' as MetricsFacilityMode,
  serverIds: [] as string[],
  retentionDays: 7,
  collectionIntervalSeconds: 60,
  cleanupSchedule: 'daily' as MetricsCleanupSchedule,
  thresholds: {} as MetricsWarningThresholds,
};

export const metricsRetentionMin = 1;
export const metricsCollectionIntervalMin = 1;

export function isMetricsFacilityMode(value: unknown): value is MetricsFacilityMode {
  return value === 'all' || value === 'selected';
}

export function isMetricsCleanupSchedule(value: unknown): value is MetricsCleanupSchedule {
  return value === 'hourly' || value === 'daily' || value === 'weekly';
}

/** 保留天数必须是 >= 1 的整数；字符串、NaN、小数一律非法。 */
export function isValidMetricsRetention(value: unknown): value is number {
  return typeof value === 'number' && Number.isInteger(value) && value >= metricsRetentionMin;
}

/** 采集间隔秒数必须是 >= 1 的整数；字符串、NaN、小数一律非法。 */
export function isValidMetricsCollectionInterval(value: unknown): value is number {
  return typeof value === 'number' && Number.isInteger(value) && value >= metricsCollectionIntervalMin;
}

/** 表单原始输入（字符串）转正整数；空串、小数与负数返回 null。 */
export function parseMetricsPositiveInteger(raw: string): number | null {
  const trimmed = raw.trim();
  if (!trimmed) return null;
  const value = Number(trimmed);
  return Number.isInteger(value) && value >= 1 ? value : null;
}
