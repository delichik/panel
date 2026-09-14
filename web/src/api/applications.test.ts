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

describe('applicationsApi persistent lifecycle', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('sends the persistent deletion confirmation to the backend', async () => {
    const fetchMock = vi.fn(async (_input: RequestInfo | URL, _init?: RequestInit) => response(null));
    vi.stubGlobal('fetch', fetchMock);
    await applicationsApi.delete('app-1', true);
    expect(String(fetchMock.mock.calls[0]?.[0])).toContain('/applications/app-1?confirmPersistentDataDeletion=true');
  });

  it('downloads persistent data from the selected node', async () => {
    const fetchMock = vi.fn(async (_input: RequestInfo | URL, _init?: RequestInit) => new Response(new Blob(['zip']), {
      status: 200,
      headers: { 'Content-Disposition': 'attachment; filename="data.zip"' },
    }));
    vi.stubGlobal('fetch', fetchMock);
    await applicationsApi.downloadPersistentData('app-1', 'srv-old');
    expect(String(fetchMock.mock.calls[0]?.[0])).toContain('/applications/app-1/persistent-data?serverId=srv-old');
  });
});
