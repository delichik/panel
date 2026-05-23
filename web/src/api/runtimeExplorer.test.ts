import { ApiClient } from './client';
import { createRuntimeExplorerApi } from './runtimeExplorer';

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

describe('runtimeExplorerApi', () => {
  it('uses the Runtime Explorer namespace for node resources and actions', async () => {
    const fetcher = vi.fn().mockImplementation(() => jsonResponse({ data: {}, error: null }));
    const api = createRuntimeExplorerApi(new ApiClient({ baseUrl: '/api/v1', fetcher }));

    await api.getNodeRuntime('node 1');
    await api.restartContainer('node 1', 'container/1');
    await api.stopContainer('node 1', 'container/1');
    await api.deleteContainer('node 1', 'container/1');
    await api.prune('node 1');

    expect(fetcher).toHaveBeenNthCalledWith(
      1,
      '/api/v1/runtime-explorer/nodes/node%201',
      expect.objectContaining({ method: 'GET' }),
    );
    expect(fetcher).toHaveBeenNthCalledWith(
      2,
      '/api/v1/runtime-explorer/nodes/node%201/containers/container%2F1/restart',
      expect.objectContaining({ method: 'POST' }),
    );
    expect(fetcher).toHaveBeenNthCalledWith(
      3,
      '/api/v1/runtime-explorer/nodes/node%201/containers/container%2F1/stop',
      expect.objectContaining({ method: 'POST' }),
    );
    expect(fetcher).toHaveBeenNthCalledWith(
      4,
      '/api/v1/runtime-explorer/nodes/node%201/containers/container%2F1',
      expect.objectContaining({ method: 'DELETE' }),
    );
    expect(fetcher).toHaveBeenNthCalledWith(
      5,
      '/api/v1/runtime-explorer/nodes/node%201/prune',
      expect.objectContaining({ method: 'POST' }),
    );
  });
});
