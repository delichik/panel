import { ApiClient } from './client';
import { createOverviewApi } from './overview';
import type { OverviewCardConfigurationDto, OverviewCardDataDto } from '@/types/api';

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

describe('overviewApi', () => {
  it('loads and replaces the database-backed card layout', async () => {
    const configuration: OverviewCardConfigurationDto = {
      cards: [{
        id: 'card-1',
        kind: 'cpu',
        width: 3,
        height: 2,
        range: '1h',
        networkDirection: 'both',
        serverIds: [],
      }],
    };
    const fetcher = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse({ data: configuration, error: null }))
      .mockResolvedValueOnce(jsonResponse({ data: configuration, error: null }));
    const api = createOverviewApi(new ApiClient({ baseUrl: '/api/v1', fetcher }));

    await expect(api.getCards()).resolves.toEqual(configuration);
    await expect(api.updateCards(configuration)).resolves.toEqual(configuration);

    expect(fetcher).toHaveBeenNthCalledWith(1, '/api/v1/overview/cards', expect.objectContaining({ method: 'GET' }));
    expect(fetcher).toHaveBeenNthCalledWith(2, '/api/v1/overview/cards', expect.objectContaining({
      method: 'PUT',
      body: JSON.stringify(configuration),
    }));
  });

  it('loads all data for one overview card by card id', async () => {
    const data: OverviewCardDataDto = {
      card: {
        id: 'card-1',
        kind: 'cpu',
        width: 3,
        height: 2,
        range: '1h',
        networkDirection: 'both',
        serverIds: [],
      },
      metricsByServer: {
        'srv-1': {
          range: '1h',
          cpu: [{ time: '2026-06-13T00:00:00Z', usagePercent: 42 }],
          memory: [],
          disk: [],
          network: [],
        },
      },
    };
    const fetcher = vi.fn().mockResolvedValueOnce(jsonResponse({ data, error: null }));
    const api = createOverviewApi(new ApiClient({ baseUrl: '/api/v1', fetcher }));

    await expect(api.getCardData('card 1')).resolves.toEqual(data);

    expect(fetcher).toHaveBeenCalledWith('/api/v1/overview/cards/card%201/data', expect.objectContaining({ method: 'GET' }));
  });
});
