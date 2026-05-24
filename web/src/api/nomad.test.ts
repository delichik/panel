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

    expect(fetcher).toHaveBeenNthCalledWith(1, '/api/v1/nomad/status', expect.objectContaining({ method: 'GET' }));
    expect(fetcher).toHaveBeenNthCalledWith(2, '/api/v1/nomad/nodes', expect.objectContaining({ method: 'GET' }));
    expect(fetcher).toHaveBeenNthCalledWith(3, '/api/v1/nomad/jobs', expect.objectContaining({ method: 'GET' }));
    expect(fetcher).toHaveBeenNthCalledWith(4, '/api/v1/nomad/deployments', expect.objectContaining({ method: 'GET' }));
    expect(fetcher).toHaveBeenNthCalledWith(5, '/api/v1/nomad/evaluations', expect.objectContaining({ method: 'GET' }));
    expect(fetcher).toHaveBeenNthCalledWith(6, '/api/v1/nomad/services', expect.objectContaining({ method: 'GET' }));
  });
});
