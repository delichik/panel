import { afterEach, describe, expect, it, vi } from 'vitest';
import { ApiClient, setAuthTokenProvider } from './client';

const originalFetch = globalThis.fetch;

afterEach(() => {
  globalThis.fetch = originalFetch;
  setAuthTokenProvider(null);
  vi.restoreAllMocks();
});

describe('ApiClient', () => {
  it('unwraps successful data envelopes', async () => {
    globalThis.fetch = vi.fn(async () => new Response(JSON.stringify({ data: { ok: true } }), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    })) as typeof fetch;

    await expect(new ApiClient('/api/v1').get('/ping')).resolves.toEqual({ ok: true });
  });

  it('throws structured errors for API error envelopes', async () => {
    globalThis.fetch = vi.fn(async () => new Response(JSON.stringify({ error: { code: 'unauthorized', message: 'No session' } }), {
      status: 401,
      headers: { 'Content-Type': 'application/json' },
    })) as typeof fetch;

    await expect(new ApiClient('/api/v1').get('/session')).rejects.toMatchObject({
      status: 401,
      code: 'unauthorized',
      message: 'No session',
    });
  });

  it('rejects HTML responses instead of pretending they are API data', async () => {
    globalThis.fetch = vi.fn(async () => new Response('<html></html>', {
      status: 200,
      headers: { 'Content-Type': 'text/html' },
    })) as typeof fetch;

    await expect(new ApiClient('/api/v1').get('/overview')).rejects.toMatchObject({
      code: 'html_response',
    });
  });

  it('accepts 204 No Content for delete operations', async () => {
    globalThis.fetch = vi.fn(async () => new Response(null, { status: 204 })) as typeof fetch;

    await expect(new ApiClient('/api/v1').delete('/servers/srv-1')).resolves.toBeUndefined();
  });

  it('injects the bearer token from the configured provider', async () => {
    setAuthTokenProvider(() => 'token-1');
    globalThis.fetch = vi.fn(async () => new Response(JSON.stringify({ data: { ok: true } }), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    })) as typeof fetch;

    await new ApiClient('/api/v1').get('/servers');

    const headers = new Headers((vi.mocked(globalThis.fetch).mock.calls[0]?.[1] as RequestInit).headers);
    expect(headers.get('Authorization')).toBe('Bearer token-1');
  });

  it('skips bearer token injection for public requests', async () => {
    setAuthTokenProvider(() => 'token-1');
    globalThis.fetch = vi.fn(async () => new Response(JSON.stringify({ data: { ok: true } }), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    })) as typeof fetch;

    await new ApiClient('/api/v1').get('/settings/public-branding', { skipAuth: true });

    const headers = new Headers((vi.mocked(globalThis.fetch).mock.calls[0]?.[1] as RequestInit).headers);
    expect(headers.has('Authorization')).toBe(false);
  });
});
