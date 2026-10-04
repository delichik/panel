import { afterEach, describe, expect, it, vi } from 'vitest';
import { activityApi } from './activity';

describe('activity API', () => {
  afterEach(() => vi.unstubAllGlobals());
  it('preserves opaque cursors, stable snapshot, false filters and escaped resource IDs', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ data: { items: [], hasMore: false, snapshotSeq: 300, headSeq: 302, projectedThroughSeq: 300 } }), { headers: { 'Content-Type': 'application/json' } }));
    vi.stubGlobal('fetch', fetchMock);
    const response = await activityApi.events({ cursor: 'a+b/c=', snapshotSeq: 300, resourceId: 'node edge', attention: false, level: 'warning,error' });
    expect(fetchMock.mock.calls[0]?.[0]).toBe('/api/v1/activity/events?cursor=a%2Bb%2Fc%3D&snapshotSeq=300&resourceId=node+edge&attention=false&level=warning%2Cerror');
    expect(response).toMatchObject({ hasMore: false, snapshotSeq: 300, headSeq: 302 });
  });
  it('reads event context through the same activity namespace', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ data: { items: [], hasMore: false } }), { headers: { 'Content-Type': 'application/json' } }));
    vi.stubGlobal('fetch', fetchMock);
    await activityApi.context('event/id', { scope: 'execution', before: 20, after: 20 });
    expect(fetchMock.mock.calls[0]?.[0]).toBe('/api/v1/activity/events/event%2Fid/context?scope=execution&before=20&after=20');
  });
});
