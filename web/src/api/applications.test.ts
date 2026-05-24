import { ApiClient } from './client';
import { createApplicationsApi } from './applications';

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

describe('applicationsApi', () => {
  it('calls Application CRUD and validation endpoints', async () => {
    const fetcher = vi.fn().mockImplementation(() => jsonResponse({ data: {}, error: null }));
    const api = createApplicationsApi(new ApiClient({ baseUrl: '/api/v1', fetcher }));
    const input = {
      name: 'web',
      enabled: true,
      specYaml: 'name: web\nimage: nginx:1.27\n',
      variables: { MODE: 'prod' },
    };

    await api.list();
    await api.create(input);
    await api.update('app 1', input);
    await api.validate('app 1');
    await api.plan('app 1');

    expect(fetcher).toHaveBeenNthCalledWith(1, '/api/v1/applications', expect.objectContaining({ method: 'GET' }));
    expect(fetcher).toHaveBeenNthCalledWith(2, '/api/v1/applications', expect.objectContaining({ method: 'POST', body: JSON.stringify(input) }));
    expect(fetcher).toHaveBeenNthCalledWith(3, '/api/v1/applications/app%201', expect.objectContaining({ method: 'PUT', body: JSON.stringify(input) }));
    expect(fetcher).toHaveBeenNthCalledWith(4, '/api/v1/applications/app%201/validate', expect.objectContaining({ method: 'POST' }));
    expect(fetcher).toHaveBeenNthCalledWith(5, '/api/v1/applications/app%201/plan', expect.objectContaining({ method: 'POST' }));
  });

  it('calls Application runtime operation endpoints', async () => {
    const fetcher = vi.fn().mockImplementation(() => jsonResponse({ data: {}, error: null }));
    const api = createApplicationsApi(new ApiClient({ baseUrl: '/api/v1', fetcher }));

    await api.deploy('app/1');
    await api.stop('app/1');
    await api.restart('app/1');
    await api.runtime('app/1');
    await api.logs('app/1', { allocId: 'alloc/1', task: 'web', type: 'stdout', tail: 50 });

    expect(fetcher).toHaveBeenNthCalledWith(1, '/api/v1/applications/app%2F1/deploy', expect.objectContaining({ method: 'POST' }));
    expect(fetcher).toHaveBeenNthCalledWith(2, '/api/v1/applications/app%2F1/stop', expect.objectContaining({ method: 'POST' }));
    expect(fetcher).toHaveBeenNthCalledWith(3, '/api/v1/applications/app%2F1/restart', expect.objectContaining({ method: 'POST' }));
    expect(fetcher).toHaveBeenNthCalledWith(4, '/api/v1/applications/app%2F1/runtime', expect.objectContaining({ method: 'GET' }));
    expect(fetcher).toHaveBeenNthCalledWith(5, '/api/v1/applications/app%2F1/logs?allocId=alloc%2F1&task=web&type=stdout&tail=50', expect.objectContaining({ method: 'GET' }));
  });
});
