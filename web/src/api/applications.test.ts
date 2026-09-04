import { afterEach, describe, expect, it, vi } from 'vitest';
import { applicationsApi } from './applications';

function response(data: unknown) {
  return new Response(JSON.stringify({ data }), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  });
}

describe('applicationsApi.list', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('preserves the current paginated response contract', async () => {
    const page = { items: [{ id: 'app-1' }], total: 3, page: 2, pageSize: 1 };
    vi.stubGlobal('fetch', vi.fn(async () => response(page)));

    await expect(applicationsApi.list()).resolves.toEqual(page);
  });

  it('normalizes a legacy array response using the requested pagination', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => response([{ id: 'app-1' }])));

    await expect(applicationsApi.list({ page: 2, pageSize: 20 })).resolves.toEqual({
      items: [{ id: 'app-1' }],
      total: 1,
      page: 2,
      pageSize: 20,
    });
  });

  it('normalizes an empty legacy array without producing undefined items', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => response([])));

    await expect(applicationsApi.list()).resolves.toEqual({ items: [], total: 0, page: 1, pageSize: 50 });
  });

  it.each([
    { total: 0, page: 1, pageSize: 50 },
    { items: null, total: 0, page: 1, pageSize: 50 },
    { items: [], total: -1, page: 1, pageSize: 50 },
  ])('rejects malformed list data: %j', async (data) => {
    vi.stubGlobal('fetch', vi.fn(async () => response(data)));

    await expect(applicationsApi.list()).rejects.toMatchObject({ code: 'invalid_api_response' });
  });
});
