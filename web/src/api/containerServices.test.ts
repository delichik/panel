import { ApiClient } from './client';
import { createContainerServicesApi } from './containerServices';

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

describe('containerServicesApi', () => {
  it('calls Container Services CRUD and preview endpoints', async () => {
    const fetcher = vi.fn().mockImplementation(() => jsonResponse({ data: {}, error: null }));
    const api = createContainerServicesApi(new ApiClient({ baseUrl: '/api/v1', fetcher }));
    const input = {
      name: 'web',
      enabled: true,
      composeServiceYaml: 'image: nginx:latest\n',
      variables: { DOMAIN: 'example.com' },
      selector: { role: 'edge' },
    };

    await api.list();
    await api.create(input);
    await api.update('service 1', input);
    await api.validate('service 1', input);
    await api.renderPreview('service 1', input);
    await api.schedulePreview('service 1', input);

    expect(fetcher).toHaveBeenNthCalledWith(
      1,
      '/api/v1/container-services',
      expect.objectContaining({ method: 'GET' }),
    );
    expect(fetcher).toHaveBeenNthCalledWith(
      2,
      '/api/v1/container-services',
      expect.objectContaining({ method: 'POST', body: JSON.stringify(input) }),
    );
    expect(fetcher).toHaveBeenNthCalledWith(
      3,
      '/api/v1/container-services/service%201',
      expect.objectContaining({ method: 'PUT', body: JSON.stringify(input) }),
    );
    expect(fetcher).toHaveBeenNthCalledWith(
      4,
      '/api/v1/container-services/service%201/validate',
      expect.objectContaining({ method: 'POST', body: JSON.stringify(input) }),
    );
    expect(fetcher).toHaveBeenNthCalledWith(
      5,
      '/api/v1/container-services/service%201/render-preview',
      expect.objectContaining({ method: 'POST', body: JSON.stringify(input) }),
    );
    expect(fetcher).toHaveBeenNthCalledWith(
      6,
      '/api/v1/container-services/service%201/schedule-preview',
      expect.objectContaining({ method: 'POST', body: JSON.stringify(input) }),
    );
  });

  it('calls lifecycle, file, runtime, and logs endpoints', async () => {
    const fetcher = vi.fn().mockImplementation(() => jsonResponse({ data: { operationId: 'op-1', tasks: [] }, error: null }));
    const api = createContainerServicesApi(new ApiClient({ baseUrl: '/api/v1', fetcher }));

    await api.enablePreview('svc/1');
    await api.enable('svc/1');
    await api.disablePreview('svc/1');
    await api.disable('svc/1');
    await api.reconcile('svc/1');
    await api.restart('svc/1');
    await api.listFiles('svc/1');
    await api.createFile('svc/1', { path: 'nginx.conf', kind: 'template', content: 'server {}' });
    await api.updateFile('svc/1', 'file/1', { path: 'nginx.conf', kind: 'template', content: 'server {}' });
    await api.deleteFile('svc/1', 'file/1');
    await api.runtime('svc/1');
    await api.logs('svc/1', 50);

    expect(fetcher).toHaveBeenNthCalledWith(1, '/api/v1/container-services/svc%2F1/enable-preview', expect.objectContaining({ method: 'POST' }));
    expect(fetcher).toHaveBeenNthCalledWith(2, '/api/v1/container-services/svc%2F1/enable', expect.objectContaining({ method: 'POST' }));
    expect(fetcher).toHaveBeenNthCalledWith(3, '/api/v1/container-services/svc%2F1/disable-preview', expect.objectContaining({ method: 'POST' }));
    expect(fetcher).toHaveBeenNthCalledWith(4, '/api/v1/container-services/svc%2F1/disable', expect.objectContaining({ method: 'POST' }));
    expect(fetcher).toHaveBeenNthCalledWith(5, '/api/v1/container-services/svc%2F1/reconcile', expect.objectContaining({ method: 'POST' }));
    expect(fetcher).toHaveBeenNthCalledWith(6, '/api/v1/container-services/svc%2F1/restart', expect.objectContaining({ method: 'POST' }));
    expect(fetcher).toHaveBeenNthCalledWith(7, '/api/v1/container-services/svc%2F1/files', expect.objectContaining({ method: 'GET' }));
    expect(fetcher).toHaveBeenNthCalledWith(8, '/api/v1/container-services/svc%2F1/files', expect.objectContaining({ method: 'POST' }));
    expect(fetcher).toHaveBeenNthCalledWith(9, '/api/v1/container-services/svc%2F1/files/file%2F1', expect.objectContaining({ method: 'PUT' }));
    expect(fetcher).toHaveBeenNthCalledWith(10, '/api/v1/container-services/svc%2F1/files/file%2F1', expect.objectContaining({ method: 'DELETE' }));
    expect(fetcher).toHaveBeenNthCalledWith(11, '/api/v1/container-services/svc%2F1/runtime', expect.objectContaining({ method: 'GET' }));
    expect(fetcher).toHaveBeenNthCalledWith(12, '/api/v1/container-services/svc%2F1/logs?tail=50', expect.objectContaining({ method: 'GET' }));
  });
});
