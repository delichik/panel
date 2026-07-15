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

  it('includes varied states for visual test coverage', async () => {
    const servers = await data('/servers');
    const applications = await data('/applications');
    const certificates = await data('/certificates');
    const keyAssets = await data('/key-assets');
    const tasks = await data('/tasks?page=1&pageSize=50');
    const debug = await data('/debug/snapshot');

    expect(servers).toEqual(expect.arrayContaining([
      expect.objectContaining({ reachable: false }),
      expect.objectContaining({ privilege: expect.objectContaining({ mode: 'none' }) }),
      expect.objectContaining({ traits: expect.objectContaining({ 'agent.status': 'compatible' }) }),
    ]));
    expect(applications).toEqual(expect.arrayContaining([
      expect.objectContaining({ runtimeStatus: 'running', imageUpdateAvailable: true }),
      expect.objectContaining({ runtimeStatus: 'failed' }),
      expect.objectContaining({ runtimeStatus: 'deploying' }),
    ]));
    expect(certificates).toEqual(expect.arrayContaining([
      expect.objectContaining({ status: 'issued' }),
      expect.objectContaining({ status: 'issuing' }),
      expect.objectContaining({ status: 'failed' }),
    ]));
    expect(keyAssets).toEqual(expect.arrayContaining([
      expect.objectContaining({ algorithm: 'rsa' }),
      expect.objectContaining({ hasPrivateKey: false }),
    ]));
    expect(tasks.items.map((task: { status: string }) => task.status)).toEqual(expect.arrayContaining(['queued', 'scheduled', 'running', 'completed', 'failed', 'failed_retryable', 'blocked', 'cancelled']));
    expect(tasks.items).toEqual(expect.arrayContaining([expect.objectContaining({ parentTaskId: 'task-batch-parent' })]));
    expect(debug.databases).toEqual(expect.arrayContaining([expect.objectContaining({ healthy: false })]));
  });

  it('provides enough rows to display multi-page pagination controls', async () => {
    const servers = await data('/servers');
    const credentials = await data('/credentials');
    const domains = await data('/dns/domains');
    const records = await data('/dns/domains/domain-example/records');
    const packages = await data('/servers/srv-edge/packages/updates');
    const applications = await data('/applications');
    const certificates = await data('/certificates');
    const files = await data('/applications/app-web/files');
    const runtime = await data('/applications/app-web/runtime');

    for (const rows of [servers, credentials, domains, records, packages.updates, applications, certificates, files, runtime.instances]) {
      expect(rows.length).toBeGreaterThan(100);
    }
  });

  it('keeps task pages populated beyond the first page', async () => {
    const first = await data('/tasks?page=1&pageSize=20');
    const second = await data('/tasks?page=2&pageSize=20');
    const far = await data('/tasks?page=99&pageSize=20');

    expect(first.total).toBeGreaterThanOrEqual(1000);
    expect(second.items.length).toBeGreaterThan(0);
    expect(far.items.length).toBeGreaterThan(0);
  });

  it('persists overview card edits in memory', async () => {
    const configuration = { cards: [{ id: 'custom-network', kind: 'network', width: 4, height: 3, range: '6h', networkDirection: 'tx', serverIds: ['srv-edge'] }] };

    await data('/overview/cards', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(configuration),
    });

    expect(await data('/overview/cards')).toEqual(configuration);
    expect((await data('/overview/cards/custom-network/data')).card).toEqual(configuration.cards[0]);
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

  it('supports edit and operation routes used by visual pages', async () => {
    const updatedDomain = await data('/dns/domains/domain-example', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name: 'edited.example.test', provider: 'cloudflare' }),
    });
    const updatedRecord = await data('/dns/domains/domain-example/records/rec-a', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name: '@', type: 'A', value: '203.0.113.44', ttl: 1, proxied: false }),
    });
    const renewed = await data('/certificates/cert-web/renew', { method: 'POST' });
    const reset = await data('/key-assets/system/agent-ca/reset', { method: 'POST' });
    const preflight = await data('/key-assets/imports/preflight', { method: 'POST', body: new FormData() });

    expect(updatedDomain.name).toBe('edited.example.test');
    expect(updatedRecord.value).toBe('203.0.113.44');
    expect(renewed.renewed).toBe(true);
    expect(reset.taskId).toMatch(/^task-/);
    expect(preflight.requiresDangerConfirm).toBe(true);
  });

  it('commits facility reverse proxy drafts through the Mock save session', async () => {
    const session = await data('/facility-apps/reverse-proxy/save-sessions', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ baseUpdatedAt: '2026-07-14T10:00:00Z' }),
    });
    const upload = new FormData();
    upload.set('name', 'Draft home');
    upload.set('kind', 'uploaded_file');
    upload.set('file', new File(['home'], 'index.html'));
    const asset = await data(`/facility-apps/reverse-proxy/save-sessions/${session.id}/assets`, { method: 'POST', body: upload });
    const committed = await data(`/facility-apps/reverse-proxy/save-sessions/${session.id}/commit`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        save: {
          deploymentServers: ['srv-edge'],
          panelEntry: { enabled: false, serverId: '', domain: '' },
          domains: [{ domain: 'draft.example.test', originServerIds: ['srv-edge'], anyAccess: { enabled: false, strategy: 'round_robin', primaryOriginServerId: '' }, paths: [{ path: '/', ruleType: 'static', sourceType: 'uploaded_file', assetId: asset.id }] }],
        },
      }),
    });

    expect(committed.applyRequested).toBe(true);
    expect(committed.config.staticAssets).toContainEqual(expect.objectContaining({ id: asset.id }));
    expect(committed.config.domains[0].originServerIds).toEqual(['srv-edge']);
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
