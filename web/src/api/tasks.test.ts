import { afterEach, describe, expect, it, vi } from 'vitest';
import { tasksApi } from './tasks';

describe('tasksApi', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('filters Agent deployment history by task type and server', async () => {
    const fetchMock = vi.fn(async () => new Response(JSON.stringify({
      data: { items: [], total: 0, page: 1, pageSize: 1 },
    }), { status: 200, headers: { 'Content-Type': 'application/json' } }));
    vi.stubGlobal('fetch', fetchMock);

    await tasksApi.list({ serverId: 'srv edge', type: 'server_agent_deploy', page: 1, pageSize: 1 });

    expect(fetchMock).toHaveBeenCalledWith('/api/v1/executions?serverId=srv+edge&type=server_agent_deploy&page=1&pageSize=1', expect.any(Object));
  });
});
