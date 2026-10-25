import { beforeEach, describe, expect, it, vi } from 'vitest';
import { logsApi } from './logs';

const fetchMock = vi.fn();
vi.stubGlobal('fetch', fetchMock);

function jsonResponse(data: unknown = { items: [], hasMore: false }) {
  return new Response(JSON.stringify({ data }), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  });
}

beforeEach(() => {
  fetchMock.mockReset();
  fetchMock.mockResolvedValue(jsonResponse());
});

describe('logs API', () => {
  it('requires eventId on the logs namespace', async () => {
    await logsApi.list({ eventId: 'evt-1', level: 'error' });
    expect(fetchMock.mock.calls[0]?.[0]).toBe('/api/v1/logs?eventId=evt-1&level=error');
  });
});
