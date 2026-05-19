import { ApiClient } from './client';
import { createDockerApi } from './docker';

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

describe('dockerApi', () => {
  it('calls documented runtime endpoints', async () => {
    const fetcher = vi.fn().mockImplementation(() => jsonResponse({ data: [], error: null }));
    const api = createDockerApi(new ApiClient({ baseUrl: '/api/v1', fetcher }));

    await api.listServices('server-1');
    await api.listNetworks('server-1');
    await api.listVolumes('server-1');
    await api.listImages('server-1');

    expect(fetcher).toHaveBeenNthCalledWith(
      1,
      '/api/v1/servers/server-1/docker/services',
      expect.objectContaining({ method: 'GET' }),
    );
    expect(fetcher).toHaveBeenNthCalledWith(
      2,
      '/api/v1/servers/server-1/docker/networks',
      expect.objectContaining({ method: 'GET' }),
    );
    expect(fetcher).toHaveBeenNthCalledWith(
      3,
      '/api/v1/servers/server-1/docker/volumes',
      expect.objectContaining({ method: 'GET' }),
    );
    expect(fetcher).toHaveBeenNthCalledWith(
      4,
      '/api/v1/servers/server-1/docker/images',
      expect.objectContaining({ method: 'GET' }),
    );
  });

  it('posts image update actions with selected image ids', async () => {
    const fetcher = vi.fn().mockResolvedValue(jsonResponse({ data: { taskId: 'task-1' }, error: null }));
    const api = createDockerApi(new ApiClient({ baseUrl: '/api/v1', fetcher }));

    await expect(api.updateSelectedImages('server-1', ['sha256:abc'])).resolves.toEqual({ taskId: 'task-1' });
    expect(fetcher).toHaveBeenCalledWith(
      '/api/v1/servers/server-1/docker/images/update-selected',
      expect.objectContaining({
        method: 'POST',
        body: JSON.stringify({ imageIds: ['sha256:abc'] }),
      }),
    );
  });
});
