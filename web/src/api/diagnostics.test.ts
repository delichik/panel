import { describe, expect, it, vi } from 'vitest';
import { ApiClient } from './client';
import { createDiagnosticsApi } from './diagnostics';

describe('diagnosticsApi', () => {
  it('loads the authenticated debug snapshot endpoint', async () => {
    const fetcher = vi.fn(async () => new Response(JSON.stringify({ data: { databases: [] }, error: null }), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    }));
    const api = createDiagnosticsApi(new ApiClient({ baseUrl: '/api/v1', fetcher }));

    await api.snapshot();

    expect(fetcher).toHaveBeenCalledWith('/api/v1/debug/snapshot', expect.objectContaining({ method: 'GET' }));
  });
});
