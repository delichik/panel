import { apiClient } from './client';
import type { MetricsRange, MetricsSeriesDto, OverviewDto } from '@/types/api';

export const overviewApi = {
  getOverview() {
    return apiClient.get<OverviewDto>('/overview');
  },
  getMetrics(serverId: string, range: MetricsRange) {
    return apiClient.get<MetricsSeriesDto>(`/servers/${serverId}/metrics?range=${range}`);
  },
};
