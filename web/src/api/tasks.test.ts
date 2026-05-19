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
});
