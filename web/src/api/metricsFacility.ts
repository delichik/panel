import { apiClient, type ApiRequestOptions } from './client';
import type { MetricsFacilityConfig, MetricsFacilitySaveInput } from '@/types/metricsFacility';

export const metricsFacilityApi = {
  config(options?: ApiRequestOptions) {
    return apiClient.get<MetricsFacilityConfig>('/facility-apps/metrics', options);
  },
  saveConfig(input: MetricsFacilitySaveInput) {
    return apiClient.put<MetricsFacilityConfig>('/facility-apps/metrics', input);
  },
};
