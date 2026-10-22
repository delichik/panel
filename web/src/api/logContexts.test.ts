import { afterEach, describe, expect, it, vi } from 'vitest';
import { logContextsApi } from './logContexts';

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe('logContexts API', () => {
  it('lists contexts through the log-contexts namespace', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ data: { items: [], hasMore: false } }), { headers: { 'Content-Type': 'application/json' } }));
    vi.stubGlobal('fetch', fetchMock);
    await logContextsApi.list({ cursor: '10', operationId: 'op/x', hadError: true });
    expect(fetchMock.mock.calls[0]?.[0]).toBe('/api/v1/log-contexts?cursor=10&operationId=op%2Fx&hadError=true');
  });

  it('reads lines for a stream with level filter', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ data: { items: [], hasMore: false } }), { headers: { 'Content-Type': 'application/json' } }));
    vi.stubGlobal('fetch', fetchMock);
    await logContextsApi.lines('stream/id', { level: 'warning,error', limit: 50 });
    expect(fetchMock.mock.calls[0]?.[0]).toBe('/api/v1/log-contexts/stream%2Fid/lines?level=warning%2Cerror&limit=50');
  });
});
