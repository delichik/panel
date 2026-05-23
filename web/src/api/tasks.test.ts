import { ApiClient } from './client';
import { tasksApi, createTasksApi } from './tasks';

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

describe('tasksApi', () => {
  it('requests paginated task lists', async () => {
    const fetcher = vi.fn().mockResolvedValue(jsonResponse({ data: { items: [], total: 0, page: 2, pageSize: 20 }, error: null }));
    const api = createTasksApi(new ApiClient({ baseUrl: '/api/v1', fetcher }));

    await expect(api.list({ page: 2, pageSize: 20, status: 'running', serverId: 'srv_1', type: 'docker' })).resolves.toEqual({
      items: [],
      total: 0,
      page: 2,
      pageSize: 20,
    });
    expect(fetcher).toHaveBeenCalledWith(
      '/api/v1/tasks?page=2&pageSize=20&status=running&serverId=srv_1&type=docker',
      expect.objectContaining({ method: 'GET' }),
    );
  });

  it('keeps the default exported client available', () => {
    expect(tasksApi.get).toBeDefined();
  });

  it('runs scheduled tasks immediately', async () => {
    const fetcher = vi.fn().mockResolvedValue(jsonResponse({ data: { id: 'task_1' }, error: null }));
    const api = createTasksApi(new ApiClient({ baseUrl: '/api/v1', fetcher }));

    await api.runNow('task 1');

    expect(fetcher).toHaveBeenCalledWith(
      '/api/v1/tasks/task 1/run-now',
      expect.objectContaining({ method: 'POST' }),
    );
  });

  it('filters by operation_id and calls task step and retry endpoints', async () => {
    const fetcher = vi.fn().mockImplementation(() => jsonResponse({ data: { items: [], total: 0, page: 1, pageSize: 20 }, error: null }));
    const api = createTasksApi(new ApiClient({ baseUrl: '/api/v1', fetcher }));

    await api.list({ operationId: 'op 1' });
    await api.steps('task 1');
    await api.retry('task 1');

    expect(fetcher).toHaveBeenNthCalledWith(
      1,
      '/api/v1/tasks?page=1&pageSize=20&operation_id=op+1',
      expect.objectContaining({ method: 'GET' }),
    );
    expect(fetcher).toHaveBeenNthCalledWith(
      2,
      '/api/v1/tasks/task 1/steps',
      expect.objectContaining({ method: 'GET' }),
    );
    expect(fetcher).toHaveBeenNthCalledWith(
      3,
      '/api/v1/tasks/task 1/retry',
      expect.objectContaining({ method: 'POST' }),
    );
  });
});
