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

  it('starts UFW install tasks for a server', async () => {
    const fetcher = vi.fn().mockResolvedValue(jsonResponse({ data: { taskId: 'task_1' }, error: null }, 202));
    const api = createServersApi(new ApiClient({ baseUrl: '/api/v1', fetcher }));

    await expect(api.installUFW('srv_1')).resolves.toEqual({ taskId: 'task_1' });
    expect(fetcher).toHaveBeenCalledWith('/api/v1/servers/srv_1/ufw/install', expect.objectContaining({ method: 'POST' }));
  });

  it('starts restart tasks for a server', async () => {
    const fetcher = vi.fn().mockResolvedValue(jsonResponse({ data: { taskId: 'task_restart' }, error: null }, 202));
    const api = createServersApi(new ApiClient({ baseUrl: '/api/v1', fetcher }));

    await expect(api.restartServer('srv_1')).resolves.toEqual({ taskId: 'task_restart' });
    expect(fetcher).toHaveBeenCalledWith('/api/v1/servers/srv_1/restart', expect.objectContaining({ method: 'POST' }));
  });

  it('manages UFW state and rules', async () => {
    const state = { serverId: 'srv_1', supported: true, installed: true, active: true, status: 'active', defaultPolicy: '', rules: [] };
    const fetcher = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse({ data: state, error: null }))
      .mockResolvedValueOnce(jsonResponse({ data: state, error: null }))
      .mockResolvedValueOnce(jsonResponse({ data: { taskId: 'task_enable' }, error: null }, 202))
      .mockResolvedValueOnce(jsonResponse({ data: state, error: null }));
    const api = createServersApi(new ApiClient({ baseUrl: '/api/v1', fetcher }));

    await expect(api.ufwState('srv_1')).resolves.toEqual(state);
    await expect(api.allowUFW('srv_1', { port: 443, protocol: 'tcp', from: '10.0.0.0/8' })).resolves.toEqual(state);
    await expect(api.enableUFW('srv_1')).resolves.toEqual({ taskId: 'task_enable' });
    await expect(api.deleteUFWRule('srv_1', 2)).resolves.toEqual(state);

    expect(fetcher).toHaveBeenNthCalledWith(1, '/api/v1/servers/srv_1/ufw', expect.objectContaining({ method: 'GET' }));
    expect(fetcher).toHaveBeenNthCalledWith(2, '/api/v1/servers/srv_1/ufw/rules', expect.objectContaining({ method: 'POST', body: JSON.stringify({ port: 443, protocol: 'tcp', from: '10.0.0.0/8' }) }));
    expect(fetcher).toHaveBeenNthCalledWith(3, '/api/v1/servers/srv_1/ufw/enable', expect.objectContaining({ method: 'POST' }));
    expect(fetcher).toHaveBeenNthCalledWith(4, '/api/v1/servers/srv_1/ufw/rules/2', expect.objectContaining({ method: 'DELETE' }));
  });
});
