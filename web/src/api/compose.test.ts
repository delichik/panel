import { ApiClient } from './client';
import { createComposeApi } from './compose';

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

describe('composeApi', () => {
  it('calls service template CRUD and preview endpoints', async () => {
    const fetcher = vi.fn().mockImplementation(() => jsonResponse({ data: {}, error: null }));
    const api = createComposeApi(new ApiClient({ baseUrl: '/api/v1', fetcher }));
    const input = { name: 'nginx', composeYaml: 'services: {}', variables: [] };

    await api.listTemplates();
    await api.createTemplate(input);
    await api.updateTemplate('template 1', input);
    await api.validateTemplate('template 1', { serverId: 'srv_1' });
    await api.renderTemplatePreview('template 1', { serverId: 'srv_1' });

    expect(fetcher).toHaveBeenNthCalledWith(
      1,
      '/api/v1/service-templates',
      expect.objectContaining({ method: 'GET' }),
    );
    expect(fetcher).toHaveBeenNthCalledWith(
      2,
      '/api/v1/service-templates',
      expect.objectContaining({ method: 'POST', body: JSON.stringify(input) }),
    );
    expect(fetcher).toHaveBeenNthCalledWith(
      3,
      '/api/v1/service-templates/template%201',
      expect.objectContaining({ method: 'PUT', body: JSON.stringify(input) }),
    );
    expect(fetcher).toHaveBeenNthCalledWith(
      4,
      '/api/v1/service-templates/template%201/validate',
      expect.objectContaining({ method: 'POST', body: JSON.stringify({ serverId: 'srv_1' }) }),
    );
    expect(fetcher).toHaveBeenNthCalledWith(
      5,
      '/api/v1/service-templates/template%201/render-preview',
      expect.objectContaining({ method: 'POST', body: JSON.stringify({ serverId: 'srv_1' }) }),
    );
  });

  it('calls template file endpoints with distinct text and binary routes', async () => {
    const fetcher = vi.fn().mockImplementation(() => jsonResponse({ data: {}, error: null }));
    const api = createComposeApi(new ApiClient({ baseUrl: '/api/v1', fetcher }));

    await api.listTemplateFiles('template-1');
    await api.createTemplateTextFile('template-1', { path: 'app.env', content: 'PORT={{ .port }}' });
    await api.createTemplateBinaryFile('template-1', { path: 'logo.png', base64Content: 'AA==' });
    await api.updateTemplateFile('template-1', 'file/1', { path: 'app.env', content: 'PORT=80' });
    await api.deleteTemplateFile('template-1', 'file/1');
    await api.getServerVariables('srv 1');
    await api.updateServerVariables('srv 1', { domain: 'example.com' });

    expect(fetcher).toHaveBeenNthCalledWith(
      1,
      '/api/v1/service-templates/template-1/files',
      expect.objectContaining({ method: 'GET' }),
    );
    expect(fetcher).toHaveBeenNthCalledWith(
      2,
      '/api/v1/service-templates/template-1/files/template',
      expect.objectContaining({ method: 'POST' }),
    );
    expect(fetcher).toHaveBeenNthCalledWith(
      3,
      '/api/v1/service-templates/template-1/files/binary',
      expect.objectContaining({ method: 'POST' }),
    );
    expect(fetcher).toHaveBeenNthCalledWith(
      4,
      '/api/v1/service-templates/template-1/files/file%2F1',
      expect.objectContaining({ method: 'PUT' }),
    );
    expect(fetcher).toHaveBeenNthCalledWith(
      5,
      '/api/v1/service-templates/template-1/files/file%2F1',
      expect.objectContaining({ method: 'DELETE' }),
    );
    expect(fetcher).toHaveBeenNthCalledWith(
      6,
      '/api/v1/servers/srv%201/variables',
      expect.objectContaining({ method: 'GET' }),
    );
    expect(fetcher).toHaveBeenNthCalledWith(
      7,
      '/api/v1/servers/srv%201/variables',
      expect.objectContaining({ method: 'PUT', body: JSON.stringify({ domain: 'example.com' }) }),
    );
  });

  it('calls service lifecycle endpoints and returns task ids', async () => {
    const fetcher = vi.fn().mockImplementation(() => jsonResponse({ data: { taskId: 'task-1' }, error: null }));
    const api = createComposeApi(new ApiClient({ baseUrl: '/api/v1', fetcher }));

    await api.deployService('svc 1');
    await api.syncService('svc 1');
    await api.restartService('svc 1');
    await api.stopService('svc 1');
    await api.removeService('svc 1');

    expect(fetcher).toHaveBeenNthCalledWith(
      1,
      '/api/v1/services/svc%201/deploy',
      expect.objectContaining({ method: 'POST' }),
    );
    expect(fetcher).toHaveBeenNthCalledWith(
      2,
      '/api/v1/services/svc%201/sync',
      expect.objectContaining({ method: 'POST' }),
    );
    expect(fetcher).toHaveBeenNthCalledWith(
      3,
      '/api/v1/services/svc%201/restart',
      expect.objectContaining({ method: 'POST' }),
    );
    expect(fetcher).toHaveBeenNthCalledWith(
      4,
      '/api/v1/services/svc%201/stop',
      expect.objectContaining({ method: 'POST' }),
    );
    expect(fetcher).toHaveBeenNthCalledWith(
      5,
      '/api/v1/services/svc%201/remove',
      expect.objectContaining({ method: 'POST' }),
    );
  });
});
