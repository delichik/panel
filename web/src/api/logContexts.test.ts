import { beforeEach, describe, expect, it, vi } from 'vitest';
import { logContextsApi } from './logContexts';

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

describe('logContexts API alias', () => {
  it('lists through the events namespace', async () => {
    await logContextsApi.list({ cursor: '10', operationId: 'op/x', hadError: true });
    expect(fetchMock.mock.calls[0]?.[0]).toBe('/api/v1/events?cursor=10&operationId=op%2Fx&hadError=true');
  });

  it('loads lines for a legacy stream id parameter', async () => {
    await logContextsApi.lines('stream/id', { level: 'warning,error', limit: 50 });
    expect(fetchMock.mock.calls[0]?.[0]).toBe('/api/v1/events/stream%2Fid/logs?level=warning%2Cerror&limit=50');
  });
});
