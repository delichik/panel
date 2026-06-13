import { ApiClient, apiClient } from './client';
import type { OverviewCardConfigurationDto, OverviewCardDataDto, OverviewDto } from '@/types/api';

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
    getCardData(cardId: string) {
      return client.get<OverviewCardDataDto>(`/overview/cards/${encodeURIComponent(cardId)}/data`);
    },
  };
}

export const overviewApi = createOverviewApi(apiClient);
