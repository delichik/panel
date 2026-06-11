import { ApiClient } from './client';
import { createNomadApi } from './nomad';

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

describe('nomadApi', () => {
  it('calls Nomad inventory endpoints', async () => {
    const fetcher = vi.fn().mockImplementation(() => jsonResponse({ data: {}, error: null }));
    const api = createNomadApi(new ApiClient({ baseUrl: '/api/v1', fetcher }));

    await api.status();
    await api.nodes();
    await api.controlPlane();
    await api.joinCandidates();
    await api.joinServer('srv_1');
    await api.bootstrapServer({ serverId: 'srv_1', advertiseAddress: '10.0.0.10' });
    await api.redeployNode({ serverId: 'srv_1', role: 'server' });
    await api.rebuildCluster({ serverId: 'srv_1', advertiseAddress: '10.0.0.10' });
    await api.switchServer({ serverId: 'srv_1', advertiseAddress: '10.0.0.10' });
    await api.removeNode({ serverId: 'srv_1', nodeId: 'node_1' });
    await api.updateReverseProxy({ serverId: 'srv_1', enabled: true, staticFiles: false, staticSites: [] });

    expect(fetcher).toHaveBeenNthCalledWith(1, '/api/v1/nomad/status', expect.objectContaining({ method: 'GET' }));
    expect(fetcher).toHaveBeenNthCalledWith(2, '/api/v1/nomad/nodes', expect.objectContaining({ method: 'GET' }));
    expect(fetcher).toHaveBeenNthCalledWith(3, '/api/v1/nomad/control-plane', expect.objectContaining({ method: 'GET' }));
    expect(fetcher).toHaveBeenNthCalledWith(4, '/api/v1/nomad/join-candidates', expect.objectContaining({ method: 'GET' }));
    expect(fetcher).toHaveBeenNthCalledWith(5, '/api/v1/nomad/join', expect.objectContaining({ method: 'POST', body: JSON.stringify({ serverId: 'srv_1' }) }));
    expect(fetcher).toHaveBeenNthCalledWith(6, '/api/v1/nomad/bootstrap-server', expect.objectContaining({ method: 'POST', body: JSON.stringify({ serverId: 'srv_1', advertiseAddress: '10.0.0.10' }) }));
    expect(fetcher).toHaveBeenNthCalledWith(7, '/api/v1/nomad/redeploy-node', expect.objectContaining({ method: 'POST', body: JSON.stringify({ serverId: 'srv_1', role: 'server' }) }));
    expect(fetcher).toHaveBeenNthCalledWith(8, '/api/v1/nomad/rebuild-cluster', expect.objectContaining({ method: 'POST', body: JSON.stringify({ serverId: 'srv_1', advertiseAddress: '10.0.0.10' }) }));
    expect(fetcher).toHaveBeenNthCalledWith(9, '/api/v1/nomad/switch-server', expect.objectContaining({ method: 'POST', body: JSON.stringify({ serverId: 'srv_1', advertiseAddress: '10.0.0.10' }) }));
    expect(fetcher).toHaveBeenNthCalledWith(10, '/api/v1/nomad/remove-node', expect.objectContaining({ method: 'POST', body: JSON.stringify({ serverId: 'srv_1', nodeId: 'node_1' }) }));
    expect(fetcher).toHaveBeenNthCalledWith(11, '/api/v1/nomad/reverse-proxy', expect.objectContaining({ method: 'PUT', body: JSON.stringify({ serverId: 'srv_1', enabled: true, staticFiles: false, staticSites: [] }) }));
  });

  it('normalizes nullable control-plane arrays', async () => {
    const fetcher = vi.fn().mockResolvedValue(
      jsonResponse({
        data: {
          status: 'connected',
          leader: 'node-1',
          nodes: null,
          joinCandidates: null,
          bootstrapCandidates: null,
        },
        error: null,
      }),
    );
    const api = createNomadApi(new ApiClient({ baseUrl: '/api/v1', fetcher }));

    await expect(api.controlPlane()).resolves.toEqual({
      status: 'connected',
      leader: 'node-1',
      nodes: [],
      joinCandidates: [],
      bootstrapCandidates: [],
    });
  });
});
