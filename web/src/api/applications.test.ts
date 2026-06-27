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
    await api.files('app 1');
    await api.saveFile('app 1', { path: 'config/app.conf', kind: 'template', contentBase64: 'aGVsbG8=' });
    await api.deleteFile('app 1', 'file/1');
    await api.beginSaveSession({ applicationId: 'app 1', save: input });
    await api.uploadSaveSessionFile('session/1', { path: 'config/app.conf', kind: 'template', contentBase64: 'aGVsbG8=' });
    await api.uploadSaveSessionArchive('session/1', { basePath: 'public', kind: 'binary', file: new File(['zip'], 'public.zip') });
    await api.deleteSaveSessionFile('session/1', { path: 'config/old.conf' });
    await api.commitSaveSession('session/1');
    await api.package('app 1');
    await api.validate('app 1');
    await api.plan('app 1');
    await api.checkImage('app 1');

    expect(fetcher).toHaveBeenNthCalledWith(1, '/api/v1/applications', expect.objectContaining({ method: 'GET' }));
    expect(fetcher).toHaveBeenNthCalledWith(2, '/api/v1/applications', expect.objectContaining({ method: 'POST', body: JSON.stringify(input) }));
    expect(fetcher).toHaveBeenNthCalledWith(3, '/api/v1/applications/app%201', expect.objectContaining({ method: 'PUT', body: JSON.stringify(input) }));
    expect(fetcher).toHaveBeenNthCalledWith(4, '/api/v1/applications/app%201/files', expect.objectContaining({ method: 'GET' }));
    expect(fetcher).toHaveBeenNthCalledWith(5, '/api/v1/applications/app%201/files', expect.objectContaining({ method: 'POST', body: JSON.stringify({ path: 'config/app.conf', kind: 'template', contentBase64: 'aGVsbG8=' }) }));
    expect(fetcher).toHaveBeenNthCalledWith(6, '/api/v1/applications/app%201/files/file%2F1', expect.objectContaining({ method: 'DELETE' }));
    expect(fetcher).toHaveBeenNthCalledWith(7, '/api/v1/application-save-sessions', expect.objectContaining({ method: 'POST', body: JSON.stringify({ applicationId: 'app 1', save: input }) }));
    expect(fetcher).toHaveBeenNthCalledWith(8, '/api/v1/application-save-sessions/session%2F1/files', expect.objectContaining({ method: 'POST' }));
    expect(fetcher).toHaveBeenNthCalledWith(9, '/api/v1/application-save-sessions/session%2F1/files/archive', expect.objectContaining({ method: 'POST', body: expect.any(FormData) }));
    expect(fetcher).toHaveBeenNthCalledWith(10, '/api/v1/application-save-sessions/session%2F1/files/delete', expect.objectContaining({ method: 'POST', body: JSON.stringify({ path: 'config/old.conf' }) }));
    expect(fetcher).toHaveBeenNthCalledWith(11, '/api/v1/application-save-sessions/session%2F1/commit', expect.objectContaining({ method: 'POST' }));
    expect(fetcher).toHaveBeenNthCalledWith(12, '/api/v1/applications/app%201/package', expect.objectContaining({ method: 'GET' }));
    expect(fetcher).toHaveBeenNthCalledWith(13, '/api/v1/applications/app%201/validate', expect.objectContaining({ method: 'POST' }));
    expect(fetcher).toHaveBeenNthCalledWith(14, '/api/v1/applications/app%201/plan', expect.objectContaining({ method: 'POST' }));
    expect(fetcher).toHaveBeenNthCalledWith(15, '/api/v1/applications/app%201/image/check', expect.objectContaining({ method: 'POST' }));
  });

  it('calls Application runtime operation endpoints', async () => {
    const fetcher = vi.fn().mockImplementation(() => jsonResponse({ data: {}, error: null }));
    const api = createApplicationsApi(new ApiClient({ baseUrl: '/api/v1', fetcher }));

    await api.deploy('app/1');
    await api.stop('app/1');
    await api.restart('app/1');
    await api.updateImage('app/1');
    await api.runtime('app/1');
    await api.logs('app/1', { instanceId: 'app/1-srv/1', containerName: 'web', type: 'stdout', tail: 50 });

    expect(fetcher).toHaveBeenNthCalledWith(1, '/api/v1/applications/app%2F1/deploy', expect.objectContaining({ method: 'POST' }));
    expect(fetcher).toHaveBeenNthCalledWith(2, '/api/v1/applications/app%2F1/stop', expect.objectContaining({ method: 'POST' }));
    expect(fetcher).toHaveBeenNthCalledWith(3, '/api/v1/applications/app%2F1/restart', expect.objectContaining({ method: 'POST' }));
    expect(fetcher).toHaveBeenNthCalledWith(4, '/api/v1/applications/app%2F1/image/update', expect.objectContaining({ method: 'POST' }));
    expect(fetcher).toHaveBeenNthCalledWith(5, '/api/v1/applications/app%2F1/runtime', expect.objectContaining({ method: 'GET' }));
    expect(fetcher).toHaveBeenNthCalledWith(6, '/api/v1/applications/app%2F1/logs?instanceId=app%2F1-srv%2F1&containerName=web&type=stdout&tail=50', expect.objectContaining({ method: 'GET' }));
  });
});
