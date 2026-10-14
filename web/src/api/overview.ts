import { apiGet, apiPut } from './client';
import type { OverviewCardConfigurationSet, OverviewCardData, OverviewDto } from '@/types/overview';

export const overviewApi = {
  getOverview() {
    return apiGet<OverviewDto>('/overview');
  },
  getCards() {
    return apiGet<OverviewCardConfigurationSet>('/overview/cards');
  },
  updateCards(input: OverviewCardConfigurationSet) {
    return apiPut<OverviewCardConfigurationSet>('/overview/cards', input);
  },
  getCardData(cardId: string, since?: string) {
    const query = since ? `?since=${encodeURIComponent(since)}` : '';
    return apiGet<OverviewCardData>(`/overview/cards/${encodeURIComponent(cardId)}/data${query}`);
  },
};
