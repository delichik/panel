import { ApiClient } from './client';
import { createServersApi } from './servers';

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

describe('serversApi', () => {
  it('normalizes empty list responses for servers and credentials', async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse({ data: null, error: null }))
      .mockResolvedValueOnce(jsonResponse({ data: null, error: null }));
    const api = createServersApi(new ApiClient({ baseUrl: '/api/v1', fetcher }));

    await expect(api.listServers()).resolves.toEqual([]);
    await expect(api.listCredentials()).resolves.toEqual([]);
  });
});
