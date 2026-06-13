import { ApiClient, apiClient } from './client';
import type { MetricsRange, MetricsSeriesDto, OverviewCardConfigurationDto, OverviewDto } from '@/types/api';

export function createOverviewApi(client: ApiClient) {
  return {
    getOverview() {
      return client.get<OverviewDto>('/overview');
    },
    getCards() {
      return client.get<OverviewCardConfigurationDto>('/overview/cards');
    },
    updateCards(configuration: OverviewCardConfigurationDto) {
      return client.put<OverviewCardConfigurationDto>('/overview/cards', configuration);
    },
    getMetrics(serverId: string, range: MetricsRange) {
      return client.get<MetricsSeriesDto>(`/servers/${serverId}/metrics?range=${range}`);
    },
  };
}

export const overviewApi = createOverviewApi(apiClient);
