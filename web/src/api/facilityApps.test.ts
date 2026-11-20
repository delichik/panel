import { afterEach, describe, expect, it, vi } from 'vitest';
import { reverseProxyFacilityApi } from './facilityApps';

function envelope(data: unknown, status = 200): Response {
  return new Response(JSON.stringify({ data }), { status, headers: { 'Content-Type': 'application/json' } });
}

type FetchCall = [string, RequestInit];

function bodyOf(call: FetchCall): unknown {
  return JSON.parse(String(call[1].body));
}

describe('entrance-proxy image actions', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('posts rollback with only explicitly selected servers and a confirmation flag', async () => {
    const fetchMock = vi.fn(async () => envelope({ taskId: 'task-rollback-1' }, 202));
    vi.stubGlobal('fetch', fetchMock);

    const result = await reverseProxyFacilityApi.rollbackImage({ serverIds: ['srv-1'], confirm: true });

    const calls = fetchMock.mock.calls as unknown as FetchCall[];
    expect(calls[0]![0]).toBe('/api/v1/facility-apps/reverse-proxy/image/rollback');
    expect(calls[0]![1].method).toBe('POST');
    expect(bodyOf(calls[0]!)).toEqual({ serverIds: ['srv-1'], confirm: true });
    expect(result.taskId).toBe('task-rollback-1');
  });

  it('omits serverIds for a full rollback and resumes pinned gateways through the centralized endpoint', async () => {
    const fetchMock = vi.fn(async () => envelope({ taskId: 'task-2' }, 202));
    vi.stubGlobal('fetch', fetchMock);

    await reverseProxyFacilityApi.rollbackImage({ confirm: true });
    await reverseProxyFacilityApi.resumeImage(['srv-1', 'srv-2']);

    const calls = fetchMock.mock.calls as unknown as FetchCall[];
    expect(bodyOf(calls[0]!)).toEqual({ confirm: true });
    expect(calls[1]![0]).toBe('/api/v1/facility-apps/reverse-proxy/image/resume');
    expect(calls[1]![1].method).toBe('POST');
    expect(bodyOf(calls[1]!)).toEqual({ serverIds: ['srv-1', 'srv-2'] });
  });
});
