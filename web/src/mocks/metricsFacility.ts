import type { MetricsFacilityConfig, MetricsFacilitySaveInput } from '@/types/metricsFacility';
import {
  isMetricsCleanupSchedule,
  isMetricsFacilityMode,
  isValidMetricsCollectionInterval,
  isValidMetricsRetention,
  metricsFacilityDefaults,
} from '@/utils/metricsFacility';
import { mockServers } from './servers';

/** 指标设施配置校验错误：browser mock 捕获后按契约错误码返回。 */
export class MockMetricsFacilityError extends Error {
  readonly code: string;
  readonly status: number;

  constructor(code: string, message: string, status = 422) {
    super(message);
    this.name = 'MockMetricsFacilityError';
    this.code = code;
    this.status = status;
  }
}

// 未保存过的设施与后端 GET 一致：默认值 + version 0 + 空 updatedAt。
let config: MetricsFacilityConfig = {
  ...metricsFacilityDefaults,
  serverIds: [],
  version: 0,
  updatedAt: '',
};

function cloneConfig(): MetricsFacilityConfig {
  return { ...config, serverIds: [...config.serverIds] };
}

export function getMetricsFacilityConfig(): MetricsFacilityConfig {
  return cloneConfig();
}

export function saveMetricsFacilityConfig(input: MetricsFacilitySaveInput): MetricsFacilityConfig {
  // 校验顺序与后端 metricsfacility.Service.Save 保持一致。
  if (!isMetricsFacilityMode(input.mode)) {
    throw new MockMetricsFacilityError('metrics_facility_mode_invalid', 'Metrics mode must be "all" or "selected".');
  }
  if (!isValidMetricsRetention(input.retentionDays)) {
    throw new MockMetricsFacilityError('metrics_facility_retention_invalid', 'Retention days must be at least 1.');
  }
  if (!isValidMetricsCollectionInterval(input.collectionIntervalSeconds)) {
    throw new MockMetricsFacilityError('metrics_facility_interval_invalid', 'Collection interval must be at least 1 second.');
  }
  if (!isMetricsCleanupSchedule(input.cleanupSchedule)) {
    throw new MockMetricsFacilityError('metrics_facility_cleanup_schedule_invalid', 'Cleanup schedule must be hourly, daily or weekly.');
  }
  let serverIds: string[] = [];
  if (input.mode === 'selected') {
    const known = new Set(mockServers.map((server) => server.id));
    serverIds = [...new Set((input.serverIds ?? []).map((id) => id.trim()).filter(Boolean))].sort();
    if (serverIds.length === 0) {
      throw new MockMetricsFacilityError('metrics_facility_servers_required', 'Select at least one metrics node.');
    }
    const missing = serverIds.find((id) => !known.has(id));
    if (missing) {
      throw new MockMetricsFacilityError('metrics_facility_server_not_found', `Server ${missing} was not found.`);
    }
  }
  config = {
    mode: input.mode,
    // all 模式忽略 serverIds，与后端归一化一致。
    serverIds,
    retentionDays: input.retentionDays,
    collectionIntervalSeconds: input.collectionIntervalSeconds,
    cleanupSchedule: input.cleanupSchedule,
    version: config.version + 1,
    updatedAt: new Date().toISOString(),
  };
  return cloneConfig();
}
