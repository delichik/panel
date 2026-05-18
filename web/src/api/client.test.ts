import { ApiClient } from './client';

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

describe('ApiClient', () => {
  it('unwraps successful response envelopes', async () => {
    const fetcher = vi.fn().mockResolvedValue(jsonResponse({ data: { ok: true }, error: null }));
    const client = new ApiClient({ baseUrl: '/api/v1', fetcher });

    await expect(client.get<{ ok: boolean }>('/health')).resolves.toEqual({ ok: true });
    expect(fetcher).toHaveBeenCalledWith('/api/v1/health', expect.objectContaining({ credentials: 'include' }));
  });

  it('throws typed errors from error envelopes', async () => {
    const fetcher = vi.fn().mockResolvedValue(
      jsonResponse(
        {
          data: null,
          error: { code: 'unauthorized', message: 'bad credentials', details: { field: 'password' } },
        },
        401,
      ),
    );
    const client = new ApiClient({ fetcher });

    await expect(client.post('/auth/login', {})).rejects.toMatchObject({
      status: 401,
      code: 'unauthorized',
      message: 'bad credentials',
      details: { field: 'password' },
    });
  });
});
