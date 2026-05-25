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
    await api.jobs();
    await api.deployments();
    await api.evaluations();
    await api.services();
    await api.controlPlane();
    await api.joinCandidates();
    await api.joinServer('srv_1');
    await api.bootstrapServer('srv_1');

    expect(fetcher).toHaveBeenNthCalledWith(1, '/api/v1/nomad/status', expect.objectContaining({ method: 'GET' }));
    expect(fetcher).toHaveBeenNthCalledWith(2, '/api/v1/nomad/nodes', expect.objectContaining({ method: 'GET' }));
    expect(fetcher).toHaveBeenNthCalledWith(3, '/api/v1/nomad/jobs', expect.objectContaining({ method: 'GET' }));
    expect(fetcher).toHaveBeenNthCalledWith(4, '/api/v1/nomad/deployments', expect.objectContaining({ method: 'GET' }));
    expect(fetcher).toHaveBeenNthCalledWith(5, '/api/v1/nomad/evaluations', expect.objectContaining({ method: 'GET' }));
    expect(fetcher).toHaveBeenNthCalledWith(6, '/api/v1/nomad/services', expect.objectContaining({ method: 'GET' }));
    expect(fetcher).toHaveBeenNthCalledWith(7, '/api/v1/nomad/control-plane', expect.objectContaining({ method: 'GET' }));
    expect(fetcher).toHaveBeenNthCalledWith(8, '/api/v1/nomad/join-candidates', expect.objectContaining({ method: 'GET' }));
    expect(fetcher).toHaveBeenNthCalledWith(9, '/api/v1/nomad/join', expect.objectContaining({ method: 'POST', body: JSON.stringify({ serverId: 'srv_1' }) }));
    expect(fetcher).toHaveBeenNthCalledWith(10, '/api/v1/nomad/bootstrap-server', expect.objectContaining({ method: 'POST', body: JSON.stringify({ serverId: 'srv_1' }) }));
  });
});
