import { beforeEach, describe, expect, it, vi } from 'vitest';
import { isMockApiEnabled, mockFetch, prepareMockSession, resetMockApi } from './index';

async function request(path: string, init?: RequestInit) {
  return mockFetch(`http://localhost/api/v1${path}`, init);
}

async function data(path: string, init?: RequestInit) {
  const response = await request(path, init);
  expect(response.ok).toBe(true);
  return (await response.json()).data;
}

describe('frontend Mock API', () => {
  beforeEach(() => {
    resetMockApi();
    vi.unstubAllEnvs();
    const values = new Map<string, string>();
    vi.stubGlobal('localStorage', {
      getItem: (key: string) => values.get(key) ?? null,
      setItem: (key: string, value: string) => values.set(key, value),
      removeItem: (key: string) => values.delete(key),
      clear: () => values.clear(),
    });
  });

  it('serves representative data for every main page group', async () => {
    const paths = [
      '/auth/session', '/settings/runtime', '/system/version', '/overview', '/overview/cards',
      '/servers', '/credentials', '/servers/srv-edge/ufw', '/servers/srv-edge/packages/updates',
      '/applications', '/servers/srv-edge/containers', '/servers/srv-edge/images',
      '/servers/srv-edge/networks', '/servers/srv-edge/volumes', '/dns/domains',
      '/certificates', '/self-signed-certificates', '/key-assets', '/key-assets/system',
      '/tasks?page=1&pageSize=20', '/debug/snapshot',
    ];

    for (const path of paths) {
      expect(await data(path)).not.toBeNull();
    }
  });

  it('keeps mutations in memory until the Mock API is reset', async () => {
    const created = await data('/dns/domains', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name: 'created.example.test', provider: 'cloudflare' }),
    });

    expect(created.name).toBe('created.example.test');
    expect(await data('/dns/domains')).toContainEqual(expect.objectContaining({ name: 'created.example.test' }));

    resetMockApi();
    expect(await data('/dns/domains')).not.toContainEqual(expect.objectContaining({ name: 'created.example.test' }));
  });

  it('returns a structured error for an unimplemented route', async () => {
    const response = await request('/not-implemented');
    const body = await response.json();

    expect(response.status).toBe(501);
    expect(body.error.code).toBe('mock_route_not_found');
  });

  it('prepares an authenticated session only in test mode', () => {
    vi.stubEnv('VITE_PANEL_TEST_MODE', 'true');
    expect(isMockApiEnabled()).toBe(true);

    prepareMockSession();

    expect(localStorage.getItem('authToken')).toBe('mock-session-token');
  });
});
