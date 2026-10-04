import { afterEach, describe, expect, it, vi } from 'vitest';
import { serversApi } from './servers';

describe('serversApi', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('requests server metrics with a backend-supported default range', async () => {
    const fetchMock = vi.fn(async () => new Response(JSON.stringify({
      data: { range: '1h', cpu: [], memory: [], disk: [], network: [], load: [] },
    }), { status: 200, headers: { 'Content-Type': 'application/json' } }));
    vi.stubGlobal('fetch', fetchMock);

    await serversApi.metrics('srv-main');

    expect(fetchMock).toHaveBeenCalledWith('/api/v1/servers/srv-main/metrics?range=1h', expect.any(Object));
  });

  it('requests the per-node tailscale apply endpoint and returns the task receipt', async () => {
    const fetchMock = vi.fn(async () => new Response(JSON.stringify({ data: { taskId: 'task-tailscale-apply-1' } }), { status: 202, headers: { 'Content-Type': 'application/json' } }));
    vi.stubGlobal('fetch', fetchMock);

    await expect(serversApi.applyTailscale('srv-edge/sgp')).resolves.toEqual({ taskId: 'task-tailscale-apply-1' });
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/servers/srv-edge%2Fsgp/tailscale/apply', expect.objectContaining({ method: 'POST' }));
  });
});
