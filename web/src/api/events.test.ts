import { beforeEach, describe, expect, it, vi } from 'vitest';
import { eventsApi } from './events';

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

describe('events API', () => {
  it('lists events through the events namespace', async () => {
    await eventsApi.list({ cursor: '10', operationId: 'op/x', hadError: true });
    expect(fetchMock.mock.calls[0]?.[0]).toBe('/api/v1/events?cursor=10&operationId=op%2Fx&hadError=true');
  });

  it('loads event logs with level filters', async () => {
    await eventsApi.logs('event/id', { level: 'warning,error', limit: 50 });
    expect(fetchMock.mock.calls[0]?.[0]).toBe('/api/v1/events/event%2Fid/logs?level=warning%2Cerror&limit=50');
  });
});
