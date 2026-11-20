// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { installMockApi } from './browser';
import { setDebugFailure } from './debug';

const nativeFetch = globalThis.fetch;

describe('domain mock routes', () => {
  beforeEach(() => {
    vi.stubEnv('VITE_PANEL_TEST_AUTH', 'true');
    installMockApi();
  });

  afterEach(() => {
    vi.unstubAllEnvs();
    globalThis.fetch = nativeFetch;
  });

  it('serves overview card configuration on the real route', async () => {
    const response = await fetch('/api/v1/overview/cards');
    const envelope = await response.json();
    expect(response.status).toBe(200);
    expect(envelope.data.cards.length).toBeGreaterThan(0);
  });

  it('scopes on-demand facility diagnostics to an existing gateway', async () => {
    const config = await (await fetch('/api/v1/facility-apps/reverse-proxy')).json();
    const serverId = config.data.deploymentServers[0];
    const response = await fetch(`/api/v1/facility-apps/reverse-proxy/diagnostics?serverId=${serverId}`);
    const result = await response.json();
    expect(response.status).toBe(200);
    expect(result.data).toMatchObject({ serverId, status: 'sampled', issues: [], truncated: false });
    expect((await fetch('/api/v1/facility-apps/reverse-proxy/diagnostics')).status).toBe(422);
    expect((await fetch('/api/v1/facility-apps/reverse-proxy/diagnostics?serverId=unrelated')).status).toBe(404);
  });

  it('requires cleanup confirmation and returns progress for the same run', async () => {
    const clock = vi.spyOn(Date, 'now').mockReturnValue(100000);
    try {
      const rejected = await fetch('/api/v1/debug/clear-runtime-data', { method: 'POST', body: '{}' });
      expect(rejected.status).toBe(422);
      const request = { method: 'POST', body: JSON.stringify({ confirmation: 'CLEAR RUNTIME DATA' }) };
      const accepted = await fetch('/api/v1/debug/clear-runtime-data', request);
      const initial = (await accepted.json()).data;
      expect(accepted.status).toBe(202);
      expect(initial).toMatchObject({ running: true, cleared: false, stage: 'stopping_workers' });
      const duplicate = await fetch('/api/v1/debug/clear-runtime-data', request);
      expect((await duplicate.json()).data.runId).toBe(initial.runId);
      clock.mockReturnValue(107000);
      const completed = await fetch('/api/v1/debug/clear-runtime-data');
      expect((await completed.json()).data).toMatchObject({ runId: initial.runId, running: false, cleared: true, status: 'succeeded', stage: 'completed' });
    } finally {
      clock.mockRestore();
    }
  });

  it('keeps runtime and task diagnostics available when database collection fails', async () => {
    setDebugFailure(true);
    const [runtime, tasks, databases] = await Promise.all([
      fetch('/api/v1/debug/runtime'),
      fetch('/api/v1/debug/tasks'),
      fetch('/api/v1/debug/databases'),
    ]);

    expect(runtime.status).toBe(200);
    expect(tasks.status).toBe(200);
    expect(databases.status).toBe(503);
  });

  it('serves demo-sized inventory data for pagination and varied states', async () => {
    const servers = await fetch('/api/v1/servers?pageSize=200');
    const serversEnvelope = await servers.json();
    expect(serversEnvelope.data.total).toBeGreaterThanOrEqual(15);
    expect(serversEnvelope.data.items.length).toBe(serversEnvelope.data.total);
    expect(serversEnvelope.data.items.some((server: { reachable: boolean }) => !server.reachable)).toBe(true);

    const credentials = await fetch('/api/v1/credentials?pageSize=200');
    const credentialsEnvelope = await credentials.json();
    expect(credentialsEnvelope.data.total).toBeGreaterThanOrEqual(15);
    expect(credentialsEnvelope.data.items.length).toBe(credentialsEnvelope.data.total);

    const tasks = await fetch('/api/v1/executions?pageSize=12&page=1');
    const tasksEnvelope = await tasks.json();
    expect(tasksEnvelope.data.total).toBeGreaterThan(80);
    expect(tasksEnvelope.data.items.length).toBe(12);

    const secondPage = await fetch('/api/v1/executions?pageSize=12&page=2');
    const secondEnvelope = await secondPage.json();
    expect(secondEnvelope.data.items[0].id).not.toBe(tasksEnvelope.data.items[0].id);
  });

  it('paginates and filters inventory list endpoints with ListPage envelopes', async () => {
    const page = await fetch('/api/v1/servers?page=1&pageSize=5');
    const pageEnvelope = await page.json();
    expect(page.status).toBe(200);
    expect(pageEnvelope.data.page).toBe(1);
    expect(pageEnvelope.data.pageSize).toBe(5);
    expect(pageEnvelope.data.items.length).toBe(5);
    expect(pageEnvelope.data.total).toBeGreaterThan(5);

    const filtered = await fetch('/api/v1/servers?q=hkg&pageSize=50');
    const filteredEnvelope = await filtered.json();
    expect(filteredEnvelope.data.total).toBeGreaterThan(0);
    expect(filteredEnvelope.data.items.every((server: { name: string; host: string; id: string }) =>
      [server.name, server.host, server.id].some((value) => value.toLowerCase().includes('hkg')),
    )).toBe(true);

    const domains = await fetch('/api/v1/dns/domains?q=shop');
    const domainsEnvelope = await domains.json();
    expect(domainsEnvelope.data.items.some((domain: { name: string }) => domain.name.includes('shop'))).toBe(true);

    const apps = await fetch('/api/v1/applications?q=storefront');
    const appsEnvelope = await apps.json();
    expect(appsEnvelope.data.items.some((app: { name: string; id: string }) => app.id.includes('storefront') || app.name.toLowerCase().includes('storefront'))).toBe(true);

    const detail = await fetch('/api/v1/servers/srv-edge-sgp');
    const detailEnvelope = await detail.json();
    expect(detail.status).toBe(200);
    expect(detailEnvelope.data.id).toBe('srv-edge-sgp');
  });

  it('serves the notifications long-poll baseline as an empty page', async () => {
    const response = await fetch('/api/v1/notifications?after=7&wait=20');
    const envelope = await response.json();
    expect(response.status).toBe(200);
    expect(envelope.data).toMatchObject({ items: [], cursor: 7 });
  });

  it('aborts the notifications mock long poll with AbortError', async () => {
    const controller = new AbortController();
    const pending = fetch('/api/v1/notifications?after=7', { signal: controller.signal });
    controller.abort();
    await expect(pending).rejects.toMatchObject({ name: 'AbortError' });
  });

  it('serves expanded demo application and certificate inventory', async () => {
    const apps = await fetch('/api/v1/applications?pageSize=200');
    const appsEnvelope = await apps.json();
    expect(appsEnvelope.data.total).toBeGreaterThanOrEqual(12);
    expect(appsEnvelope.data.items.some((app: { runtimeStatus: string }) => app.runtimeStatus === 'failed')).toBe(true);
    expect(appsEnvelope.data.items.some((app: { id: string }) => app.id === 'app-billing')).toBe(true);

    const certs = await fetch('/api/v1/certificates?pageSize=200');
    const certsEnvelope = await certs.json();
    expect(certsEnvelope.data.total).toBeGreaterThanOrEqual(10);
    expect(certsEnvelope.data.items.some((cert: { status: string }) => cert.status === 'renewing')).toBe(true);

    const contexts = await fetch('/api/v1/events?limit=20');
    const contextsEnvelope = await contexts.json();
    expect(contexts.status).toBe(200);
    expect(contextsEnvelope.data.items.length).toBeGreaterThan(0);
  });
  it('serves resource and security data for expanded demo servers', async () => {
    const ufw = await fetch('/api/v1/servers/srv-api-hkg/ufw');
    const ufwEnvelope = await ufw.json();
    expect(ufw.status).toBe(200);
    expect(ufwEnvelope.data.rules.length).toBeGreaterThan(0);

    const fail2ban = await fetch('/api/v1/servers/srv-db-fra/fail2ban');
    const fail2banEnvelope = await fail2ban.json();
    expect(fail2ban.status).toBe(200);
    expect(fail2banEnvelope.data.jails.length).toBeGreaterThan(0);

    const packages = await fetch('/api/v1/servers/srv-staging-mad/packages/updates');
    const packagesEnvelope = await packages.json();
    expect(packages.status).toBe(200);
    expect(packagesEnvelope.data.updates.length).toBeGreaterThan(0);

    const containers = await fetch('/api/v1/servers/srv-media-syd/containers');
    const containersEnvelope = await containers.json();
    expect(containers.status).toBe(200);
    expect(containersEnvelope.data.items.length).toBeGreaterThan(0);

    const networks = await fetch('/api/v1/servers/srv-media-syd/networks');
    const networksEnvelope = await networks.json();
    expect(networks.status).toBe(200);
    expect(networksEnvelope.data.items.length).toBeGreaterThan(0);

    const volumes = await fetch('/api/v1/servers/srv-observability-ams/volumes');
    const volumesEnvelope = await volumes.json();
    expect(volumes.status).toBe(200);
    expect(volumesEnvelope.data.items.length).toBeGreaterThan(0);
  });

  it('returns credential conflict when a credential is referenced', async () => {
    const response = await fetch('/api/v1/credentials/cred-root-key', { method: 'DELETE' });
    const envelope = await response.json();
    expect(response.status).toBe(409);
    expect(envelope.error.code).toBe('credential_in_use');
  });

  it('returns mock_route_not_found for missing routes', async () => {
    const response = await fetch('/api/v1/unknown/domain');
    const envelope = await response.json();
    expect(response.status).toBe(404);
    expect(envelope.error.code).toBe('mock_route_not_found');
  });

  it('serves application runtime and log errors on real application routes', async () => {
    const runtime = await fetch('/api/v1/applications/app-storefront/runtime');
    const runtimeEnvelope = await runtime.json();
    expect(runtime.status).toBe(200);
    expect(runtimeEnvelope.data.instances.length).toBeGreaterThan(0);

    const missingInstance = await fetch('/api/v1/applications/app-worker/logs');
    expect(missingInstance.status).toBe(422);
    const logs = await fetch('/api/v1/applications/app-worker/logs?instanceId=inst-worker');
    const logsEnvelope = await logs.json();
    expect(logs.status).toBe(503);
    expect(logsEnvelope.error.code).toBe('application_logs_unavailable');
  });

  it('returns logs for the selected application instance', async () => {
    const response = await fetch('/api/v1/applications/app-storefront/logs?instanceId=inst-api&tail=240');
    const envelope = await response.json();
    expect(response.status).toBe(200);
    expect(envelope.data).toMatchObject({ instanceId: 'inst-api', containerName: 'panel-storefront' });
    expect((await fetch('/api/v1/applications/app-storefront/logs?instanceId=inst-worker')).status).toBe(404);
  });

  it('runs durable application edit session validation and conflict commit', async () => {
    const created = await fetch('/api/v1/application-edit-sessions', {
      method: 'POST',
      body: JSON.stringify({ applicationId: 'app-storefront' }),
    });
    const createdEnvelope = await created.json();
    const sessionId = createdEnvelope.data.id;

    const patched = await fetch(`/api/v1/application-edit-sessions/${sessionId}/draft`, {
      method: 'PATCH',
      body: JSON.stringify({ draft: { ...createdEnvelope.data.draft, name: 'conflict-app' } }),
    });
    const patchedEnvelope = await patched.json();

    const validated = await fetch(`/api/v1/application-edit-sessions/${sessionId}/validate`, {
      method: 'POST',
      body: JSON.stringify({ revision: patchedEnvelope.data.revision }),
    });
    const validationEnvelope = await validated.json();
    expect(validationEnvelope.data.valid).toBe(false);

    const committed = await fetch(`/api/v1/application-edit-sessions/${sessionId}/commit`, { method: 'POST', body: JSON.stringify({}) });
    const commitEnvelope = await committed.json();
    expect(committed.status).toBe(409);
    expect(commitEnvelope.error.code).toBe('resource_version_conflict');
  });

  it('serves the auto-update partition and the entrance-proxy settings/image routes', async () => {
    const autoUpdate = await fetch('/api/v1/settings/runtime/auto-update', {
      method: 'PUT',
      body: JSON.stringify({ appsEnabled: true, packagesEnabled: false, packagesMode: 'all', scheduleTime: '05:30' }),
    });
    const autoUpdateEnvelope = await autoUpdate.json();
    expect(autoUpdate.status).toBe(200);
    expect(autoUpdateEnvelope.data.autoUpdate).toMatchObject({ appsEnabled: true, packagesEnabled: false, packagesMode: 'all', scheduleTime: '05:30' });
    // 只读的 lastRun 投影必须保留。
    expect(autoUpdateEnvelope.data.autoUpdate.lastRunAt).toBeTruthy();

    const run = await fetch('/api/v1/settings/runtime/auto-update/run', { method: 'POST', body: '{}' });
    expect(run.status).toBe(202);
    expect((await run.json()).data.taskId).toContain('task-auto-update-manual');

    const config = await (await fetch('/api/v1/facility-apps/reverse-proxy')).json();
    expect(config.data.settings).toMatchObject({ httpPortMode: 'serve', imageAutoUpdateEnabled: true, imageUpdateIntervalSeconds: 0, customHttpDirectives: [] });
    expect(config.data.image).toMatchObject({ reference: 'nginx:1.28-alpine' });
    // 每域名 80 端口覆盖随记录投影，未显式设置的域名归一化为继承（空串）。
    expect(config.data.domains.find((domain: { domain: string }) => domain.domain === 'shop.example.test').httpPortMode).toBe('redirect');
    expect(config.data.domains.find((domain: { domain: string }) => domain.domain === 'downloads.example.test').httpPortMode).toBe('serve');
    expect(config.data.domains.find((domain: { domain: string }) => domain.domain === 'static.example.test').httpPortMode).toBe('');

    const saved = await fetch('/api/v1/facility-apps/reverse-proxy/settings', {
      method: 'PUT',
      body: JSON.stringify({
        httpPortMode: 'redirect',
        imageAutoUpdateEnabled: false,
        imageUpdateIntervalSeconds: 120,
        customHttpDirectives: ['client_max_body_size 100m;'],
        hsts: { enabled: true, maxAgeSeconds: 3600, includeSubDomains: true },
        serverTokens: 'on',
        clientMaxBodySize: '128m',
        tlsMinVersion: '1.3',
      }),
    });
    const savedEnvelope = await saved.json();
    expect(saved.status).toBe(200);
    expect(savedEnvelope.data.settings).toMatchObject({
      httpPortMode: 'redirect',
      imageAutoUpdateEnabled: false,
      imageUpdateIntervalSeconds: 120,
      customHttpDirectives: ['client_max_body_size 100m;'],
      hsts: { enabled: true, maxAgeSeconds: 3600, includeSubDomains: true },
      serverTokens: 'on',
      clientMaxBodySize: '128m',
      tlsMinVersion: '1.3',
    });

    const invalidMode = await fetch('/api/v1/facility-apps/reverse-proxy/settings', {
      method: 'PUT',
      body: JSON.stringify({ httpPortMode: 'every' }),
    });
    expect(invalidMode.status).toBe(422);
    expect((await invalidMode.json()).error.code).toBe('facility_http_port_mode_invalid');

    for (const imageUpdateIntervalSeconds of [601, -1, 1.5]) {
      const invalidInterval = await fetch('/api/v1/facility-apps/reverse-proxy/settings', {
        method: 'PUT',
        body: JSON.stringify({ httpPortMode: 'serve', imageUpdateIntervalSeconds }),
      });
      expect(invalidInterval.status).toBe(422);
      expect((await invalidInterval.json()).error.code).toBe('facility_image_update_interval_invalid');
    }

    const denied = await fetch('/api/v1/facility-apps/reverse-proxy/image/update', { method: 'POST', body: JSON.stringify({ serverIds: [], confirm: false }) });
    expect(denied.status).toBe(422);
    expect((await denied.json()).error.code).toBe('facility_proxy_image_update_confirmation_required');

    // 首次更新前没有 previous_digest：回滚为幂等空操作 200 + skipped。
    const skippedRollback = await fetch('/api/v1/facility-apps/reverse-proxy/image/rollback', { method: 'POST', body: JSON.stringify({ confirm: true }) });
    expect(skippedRollback.status).toBe(200);
    expect((await skippedRollback.json()).data).toMatchObject({ skipped: true });

    const check = await fetch('/api/v1/facility-apps/reverse-proxy/image/check', { method: 'POST' });
    expect(check.status).toBe(202);
    expect((await check.json()).data.taskId).toContain('task-facility-image-check');

    const update = await fetch('/api/v1/facility-apps/reverse-proxy/image/update', { method: 'POST', body: JSON.stringify({ serverIds: [], confirm: true }) });
    expect(update.status).toBe(202);
    expect((await update.json()).data.taskId).toContain('task-facility-image-update');
    const updatedImage = (await (await fetch('/api/v1/facility-apps/reverse-proxy')).json()).data.image;
    expect(updatedImage).toMatchObject({ status: 'up_to_date', updateAvailable: false });
    expect(updatedImage.previousDigest).toBeTruthy();

    const deniedRollback = await fetch('/api/v1/facility-apps/reverse-proxy/image/rollback', { method: 'POST', body: JSON.stringify({ confirm: false }) });
    expect(deniedRollback.status).toBe(422);
    expect((await deniedRollback.json()).error.code).toBe('facility_proxy_image_rollback_confirmation_required');
    const rollback = await fetch('/api/v1/facility-apps/reverse-proxy/image/rollback', { method: 'POST', body: JSON.stringify({ confirm: true }) });
    expect(rollback.status).toBe(202);
    expect((await rollback.json()).data.taskId).toContain('task-facility-image-rollback');
    expect((await (await fetch('/api/v1/facility-apps/reverse-proxy')).json()).data.image).toMatchObject({ status: 'pinned', pinned: true });

    const resume = await fetch('/api/v1/facility-apps/reverse-proxy/image/resume', { method: 'POST', body: '{}' });
    expect(resume.status).toBe(200);
    expect((await resume.json()).data).toMatchObject({ pinned: false, status: 'up_to_date' });
    expect((await (await fetch('/api/v1/facility-apps/reverse-proxy')).json()).data.image).toMatchObject({ status: 'up_to_date', pinned: false });
  });

  it('serves image registry settings with pointer semantics and validation', async () => {
    const initial = await (await fetch('/api/v1/settings/runtime')).json();
    expect(initial.data.imageRegistry).toEqual({ endpoint: '', scheme: 'https' });

    const endpointOnly = await fetch('/api/v1/settings/runtime/image-registry', {
      method: 'PUT',
      body: JSON.stringify({ endpoint: 'Registry.Example.COM:5000' }),
    });
    const endpointEnvelope = await endpointOnly.json();
    expect(endpointOnly.status).toBe(200);
    // 省略 scheme = 保留当前值；endpoint 规范化为主机小写 + 端口。
    expect(endpointEnvelope.data.imageRegistry).toEqual({ endpoint: 'registry.example.com:5000', scheme: 'https' });

    const schemeOnly = await (await fetch('/api/v1/settings/runtime/image-registry', {
      method: 'PUT',
      body: JSON.stringify({ scheme: 'http' }),
    })).json();
    expect(schemeOnly.data.imageRegistry).toEqual({ endpoint: 'registry.example.com:5000', scheme: 'http' });

    // 显式空串 endpoint = 恢复官方仓库，且不省略字段。
    const official = await (await fetch('/api/v1/settings/runtime/image-registry', {
      method: 'PUT',
      body: JSON.stringify({ endpoint: '' }),
    })).json();
    expect(official.data.imageRegistry).toEqual({ endpoint: '', scheme: 'http' });

    const invalidEndpoints = [
      'https://registry.example.com',
      'registry.example.com/v2',
      'registry.example.com?x=1',
      'user@registry.example.com',
      'registry example.com',
      '"registry.example.com"',
      'registry\\example.com',
      'registry.example.com:0',
      'registry.example.com:65536',
    ];
    for (const endpoint of invalidEndpoints) {
      const response = await fetch('/api/v1/settings/runtime/image-registry', {
        method: 'PUT',
        body: JSON.stringify({ endpoint }),
      });
      expect(response.status, endpoint).toBe(422);
      expect((await response.json()).error.code, endpoint).toBe('invalid_image_registry_endpoint');
    }

    const invalidScheme = await fetch('/api/v1/settings/runtime/image-registry', {
      method: 'PUT',
      body: JSON.stringify({ scheme: 'ftp' }),
    });
    expect(invalidScheme.status).toBe(422);
    expect((await invalidScheme.json()).error.code).toBe('invalid_image_registry_scheme');

    // 失败整笔不生效：仍是上一次成功的显式官方仓库 + http。
    const persisted = await (await fetch('/api/v1/settings/runtime')).json();
    expect(persisted.data.imageRegistry).toEqual({ endpoint: '', scheme: 'http' });
  });

  it('serves facility reverse proxy edit session preview on real routes', async () => {
    const created = await fetch('/api/v1/facility-apps/reverse-proxy/edit-sessions', { method: 'POST', body: JSON.stringify({ recordId: 'static.example.test' }) });
    const createdEnvelope = await created.json();
    const preview = await fetch(`/api/v1/facility-apps/reverse-proxy/edit-sessions/${createdEnvelope.data.id}/preview`, {
      method: 'POST',
      body: JSON.stringify({ revision: createdEnvelope.data.revision }),
    });
    const previewEnvelope = await preview.json();

    expect(preview.status).toBe(200);
    expect(previewEnvelope.data.token.action).toBe('facility.reverse_proxy.commit');
  });

  it('round-trips a per-domain port 80 override through a config-record commit', async () => {
    const record = (await (await fetch('/api/v1/facility-apps/reverse-proxy/config-records/status.example.test')).json()).data;
    expect(record.domain.httpPortMode).toBe('');
    const created = await fetch('/api/v1/facility-apps/reverse-proxy/edit-sessions', { method: 'POST', body: JSON.stringify({ recordId: 'status.example.test' }) });
    const session = (await created.json()).data;
    const patched = await fetch(`/api/v1/facility-apps/reverse-proxy/edit-sessions/${session.id}/draft`, {
      method: 'PATCH',
      body: JSON.stringify({ draft: { ...record.domain, httpPortMode: 'redirect' } }),
    });
    const patchedSession = (await patched.json()).data;
    expect(patchedSession.draft.httpPortMode).toBe('redirect');
    const preview = await fetch(`/api/v1/facility-apps/reverse-proxy/edit-sessions/${session.id}/preview`, {
      method: 'POST',
      body: JSON.stringify({ revision: patchedSession.revision }),
    });
    const previewToken = (await preview.json()).data.token.value;
    const commit = await fetch(`/api/v1/facility-apps/reverse-proxy/edit-sessions/${session.id}/commit`, {
      method: 'POST',
      body: JSON.stringify({ revision: patchedSession.revision, baseResourceVersion: session.baseResourceVersion.value, previewToken }),
    });
    const committed = (await commit.json()).data;
    expect(commit.status).toBe(200);
    expect(committed.record.domain.httpPortMode).toBe('redirect');
    const aggregate = (await (await fetch('/api/v1/facility-apps/reverse-proxy')).json()).data;
    expect(aggregate.domains.find((domain: { domain: string }) => domain.domain === 'status.example.test').httpPortMode).toBe('redirect');
  });

  it('round-trips per-domain HSTS and body-limit overrides and rejects invalid sizes', async () => {
    const record = (await (await fetch('/api/v1/facility-apps/reverse-proxy/config-records/downloads.example.test')).json()).data;
    const created = await fetch('/api/v1/facility-apps/reverse-proxy/edit-sessions', { method: 'POST', body: JSON.stringify({ recordId: 'downloads.example.test' }) });
    const session = (await created.json()).data;

    const invalidDraft = { ...record.domain, hstsEnabled: true, clientMaxBodySize: '50mb' };
    const patchedInvalid = await fetch(`/api/v1/facility-apps/reverse-proxy/edit-sessions/${session.id}/draft`, {
      method: 'PATCH',
      body: JSON.stringify({ draft: invalidDraft }),
    });
    const invalidSession = (await patchedInvalid.json()).data;
    const invalidValidation = await fetch(`/api/v1/facility-apps/reverse-proxy/edit-sessions/${session.id}/validate`, {
      method: 'POST',
      body: JSON.stringify({ revision: invalidSession.revision }),
    });
    const invalidResult = (await invalidValidation.json()).data;
    expect(invalidResult.valid).toBe(false);
    expect(invalidResult.diagnostics.some((item: { code: string }) => item.code === 'facility_client_max_body_size_invalid')).toBe(true);

    const invalidCommit = await fetch(`/api/v1/facility-apps/reverse-proxy/edit-sessions/${session.id}/commit`, {
      method: 'POST',
      body: JSON.stringify({ revision: invalidSession.revision, baseResourceVersion: session.baseResourceVersion.value }),
    });
    expect(invalidCommit.status).toBe(422);
    expect((await invalidCommit.json()).error.code).toBe('facility_client_max_body_size_invalid');
    const unchanged = (await (await fetch('/api/v1/facility-apps/reverse-proxy/config-records/downloads.example.test')).json()).data;
    expect(unchanged.domain.clientMaxBodySize).toBe('');

    const validDraft = { ...record.domain, hstsEnabled: true, clientMaxBodySize: '256m' };
    const patched = await fetch(`/api/v1/facility-apps/reverse-proxy/edit-sessions/${session.id}/draft`, {
      method: 'PATCH',
      body: JSON.stringify({ draft: validDraft }),
    });
    const patchedSession = (await patched.json()).data;
    const preview = await fetch(`/api/v1/facility-apps/reverse-proxy/edit-sessions/${session.id}/preview`, {
      method: 'POST',
      body: JSON.stringify({ revision: patchedSession.revision }),
    });
    const previewToken = (await preview.json()).data.token.value;
    const commit = await fetch(`/api/v1/facility-apps/reverse-proxy/edit-sessions/${session.id}/commit`, {
      method: 'POST',
      body: JSON.stringify({ revision: patchedSession.revision, baseResourceVersion: session.baseResourceVersion.value, previewToken }),
    });
    expect(commit.status).toBe(200);
    expect((await commit.json()).data.record.domain).toMatchObject({ hstsEnabled: true, clientMaxBodySize: '256m' });

    const committedRecord = (await (await fetch('/api/v1/facility-apps/reverse-proxy/config-records/downloads.example.test')).json()).data;
    expect(committedRecord.domain).toMatchObject({ hstsEnabled: true, clientMaxBodySize: '256m' });
    const aggregate = (await (await fetch('/api/v1/facility-apps/reverse-proxy')).json()).data;
    expect(aggregate.domains.find((domain: { domain: string }) => domain.domain === 'downloads.example.test')).toMatchObject({ hstsEnabled: true, clientMaxBodySize: '256m' });

    const appSession = (await (await fetch('/api/v1/application-edit-sessions', { method: 'POST', body: JSON.stringify({ applicationId: 'app-storefront' }) })).json()).data;
    const invalidRuleDraft = {
      ...appSession.draft,
      reverseProxy: [{ ...appSession.draft.reverseProxy[0], clientMaxBodySize: '2g' }],
    };
    const patchedAppInvalid = await fetch(`/api/v1/application-edit-sessions/${appSession.id}/draft`, {
      method: 'PATCH',
      body: JSON.stringify({ draft: invalidRuleDraft }),
    });
    const invalidAppSession = (await patchedAppInvalid.json()).data;
    const appValidation = await fetch(`/api/v1/application-edit-sessions/${appSession.id}/validate`, {
      method: 'POST',
      body: JSON.stringify({ revision: invalidAppSession.revision }),
    });
    const appValidationResult = (await appValidation.json()).data;
    expect(appValidationResult.valid).toBe(false);
    expect(appValidationResult.diagnostics.some((item: { code: string }) => item.code === 'application_reverse_proxy_client_max_body_size_invalid')).toBe(true);

    const invalidAppCommit = await fetch(`/api/v1/application-edit-sessions/${appSession.id}/commit`, {
      method: 'POST',
      body: JSON.stringify({ revision: invalidAppSession.revision }),
    });
    expect(invalidAppCommit.status).toBe(422);
    expect((await invalidAppCommit.json()).error.code).toBe('application_reverse_proxy_client_max_body_size_invalid');
  });

  it('serves DNS records and exposes provider failures on real routes', async () => {
    const domains = await fetch('/api/v1/dns/domains');
    const domainsEnvelope = await domains.json();
    expect(domainsEnvelope.data.total).toBeGreaterThanOrEqual(12);
    expect(domainsEnvelope.data.items.length).toBe(domainsEnvelope.data.total);

    const records = await fetch('/api/v1/dns/domains/domain-example/records');
    const recordsEnvelope = await records.json();
    expect(records.status).toBe(200);
    expect(recordsEnvelope.data.items.some((record: { type: string }) => record.type === 'TXT')).toBe(true);

    const failed = await fetch('/api/v1/dns/domains/domain-error/records');
    const failedEnvelope = await failed.json();
    expect(failed.status).toBe(502);
    expect(failedEnvelope.error.code).toBe('dns_provider_error');
  });

  it('creates certificate tasks on the real certificate route', async () => {
    const existing = await fetch('/api/v1/certificates');
    const existingEnvelope = await existing.json();
    expect(existingEnvelope.data.total).toBeGreaterThanOrEqual(10);
    expect(existingEnvelope.data.items.length).toBe(existingEnvelope.data.total);

    const response = await fetch('/api/v1/certificates', {
      method: 'POST',
      body: JSON.stringify({ name: 'new cert', domainId: 'domain-example', prefix: 'new', scope: 'single' }),
    });
    const envelope = await response.json();
    expect(response.status).toBe(201);
    expect(envelope.data.taskId).toContain('task-cert-');
  });

  it('blocks referenced key asset deletion and reports batch import conflicts', async () => {
    const deleted = await fetch('/api/v1/key-assets/asset-ca-platform', { method: 'DELETE' });
    const deletedEnvelope = await deleted.json();
    expect(deleted.status).toBe(409);
    expect(deletedEnvelope.error.code).toBe('key_asset_in_use');

    const form = new FormData();
    form.set('file', new Blob(['archive']));
    form.set('password', 'long-enough-password');
    const preflight = await fetch('/api/v1/key-assets/imports/preflight', { method: 'POST', body: form });
    const preflightEnvelope = await preflight.json();
    expect(preflight.status).toBe(200);
    expect(preflightEnvelope.data.requiresDangerConfirm).toBe(true);
  });

  it('serves task operation groups with steps, long logs, retry, and run-now routes', async () => {
    const tasks = await fetch('/api/v1/executions?operationPage=true&pageSize=80');
    const tasksEnvelope = await tasks.json();
    expect(tasks.status).toBe(200);
    expect(tasksEnvelope.data.items.some((task: { operationId: string }) => task.operationId === 'op-deploy-storefront')).toBe(true);

    const steps = await fetch('/api/v1/executions/task-deploy-3/steps');
    const stepsEnvelope = await steps.json();
    expect(stepsEnvelope.data.some((step: { status: string }) => step.status === 'failed_retryable')).toBe(true);

    const logs = await fetch('/api/v1/executions/task-deploy-3/logs');
    const logsEnvelope = await logs.json();
    expect(logsEnvelope.data.logs.length).toBeGreaterThan(30);

    const retry = await fetch('/api/v1/executions/task-deploy-3/retry', { method: 'POST' });
    expect(retry.status).toBe(202);

    const runNow = await fetch('/api/v1/executions/task-backup-1/run-now', { method: 'POST' });
    expect(runNow.status).toBe(202);
  });

  it('serves settings, backup, maintenance, and diagnostics routes', async () => {
    const settings = await fetch('/api/v1/settings/runtime');
    const settingsEnvelope = await settings.json();
    expect(settings.status).toBe(200);
    expect(settingsEnvelope.data.containerReportIntervalSeconds).toBe(30);
    // 指标保留/采集间隔/清理计划已移出运行时设置，由指标设施契约管理。
    expect(settingsEnvelope.data).not.toHaveProperty('metricsRetentionDays');
    expect(settingsEnvelope.data).not.toHaveProperty('metricsCollectionIntervalSeconds');
    expect(settingsEnvelope.data).not.toHaveProperty('cleanupSchedule');

    const conflict = await fetch('/api/v1/settings/runtime/general', {
      method: 'PUT',
      body: JSON.stringify({ containerReportIntervalSeconds: 13, language: 'en', logLevel: 'debug', reconcileTraceEnabled: false }),
    });
    const conflictEnvelope = await conflict.json();
    expect(conflict.status).toBe(409);
    expect(conflictEnvelope.error.code).toBe('settings_conflict');

    // 每个分区子资源只写本分区字段，仍返回完整 runtime 设置。
    const security = await (await fetch('/api/v1/settings/runtime/security', {
      method: 'PUT',
      body: JSON.stringify({ tokenExpiration: '30d', remoteCommandTimeoutSeconds: 60 }),
    })).json();
    expect(security.data.tokenExpiration).toBe('30d');
    expect(security.data.remoteCommandTimeoutSeconds).toBe(60);
    expect(security.data.containerReportIntervalSeconds).toBe(30);

    const certificates = await (await fetch('/api/v1/settings/runtime/certificates', {
      method: 'PUT',
      body: JSON.stringify({ panel: { domain: 'Panel.Example.com', tlsCertificateId: '' }, certificates: { email: 'ops@example.com', dnsPropagationDelaySeconds: 10 } }),
    })).json();
    expect(certificates.data.panel.domain).toBe('Panel.Example.com');
    expect(certificates.data.certificates.email).toBe('ops@example.com');
    expect(certificates.data.tokenExpiration).toBe('30d');

    const agent = await (await fetch('/api/v1/settings/runtime/agent', {
      method: 'PUT',
      body: JSON.stringify({ downloadBaseUrl: 'https://cdn.example.com', downloadVerifyTls: true, transferTimeoutSeconds: 300 }),
    })).json();
    expect(agent.data.agent).toMatchObject({ downloadBaseUrl: 'https://cdn.example.com', downloadVerifyTls: true, transferTimeoutSeconds: 300 });
    expect(agent.data.certificates.email).toBe('ops@example.com');

    const branding = await (await fetch('/api/v1/settings/runtime/branding', {
      method: 'PUT',
      body: JSON.stringify({ loginTitle: 'Ops', loginSubtitle: 'Control plane' }),
    })).json();
    expect(branding.data.branding).toEqual({ loginTitle: 'Ops', loginSubtitle: 'Control plane' });
    expect(branding.data.agent.downloadVerifyTls).toBe(true);

    const unauthenticated = await fetch('/api/v1/auth/session');
    const unauthenticatedEnvelope = await unauthenticated.json();
    expect(unauthenticatedEnvelope.data.authenticated).toBe(false);

    const login = await fetch('/api/v1/auth/login', { method: 'POST', body: JSON.stringify({ username: 'admin', password: 'admin' }) });
    const loginEnvelope = await login.json();
    expect(loginEnvelope.data.authenticated).toBe(true);
    expect(loginEnvelope.data.token).toContain('panel_mock_admin');

    const authenticated = await fetch('/api/v1/auth/session', { headers: { Authorization: `Bearer ${loginEnvelope.data.token}` } });
    const authenticatedEnvelope = await authenticated.json();
    expect(authenticatedEnvelope.data.authenticated).toBe(true);

    const accountTooShort = await fetch('/api/v1/auth/account', {
      method: 'POST',
      body: JSON.stringify({ username: 'admin', currentPassword: 'admin', newPassword: 'short' }),
    });
    const accountTooShortEnvelope = await accountTooShort.json();
    expect(accountTooShort.status).toBe(422);
    expect(accountTooShortEnvelope.error.code).toBe('admin_password_too_short');

    const account = await fetch('/api/v1/auth/account', {
      method: 'POST',
      body: JSON.stringify({ username: 'admin', currentPassword: 'admin', newPassword: 'new-admin-password' }),
    });
    const accountEnvelope = await account.json();
    expect(account.status).toBe(200);
    expect(accountEnvelope.data.passwordChangeRequired).toBe(false);

    const jwtSecret = await fetch('/api/v1/auth/jwt-secret', { method: 'POST', body: JSON.stringify({ jwtSecret: 'a-secure-demo-secret' }) });
    const jwtSecretEnvelope = await jwtSecret.json();
    expect(jwtSecret.status).toBe(200);
    expect(jwtSecretEnvelope.data.authenticated).toBe(true);

    const systemCertificates = await fetch('/api/v1/key-assets/system');
    const systemCertificatesEnvelope = await systemCertificates.json();
    expect(systemCertificates.status).toBe(200);
    expect(systemCertificatesEnvelope.data.length).toBeGreaterThanOrEqual(10);

    const resetCertificate = await fetch('/api/v1/key-assets/system/agent-panel-client/reset', { method: 'POST' });
    const resetCertificateEnvelope = await resetCertificate.json();
    expect(resetCertificate.status).toBe(202);
    expect(resetCertificateEnvelope.data.taskId).toContain('task-agent-cert-reset-');

    const current = await fetch('/api/v1/backups/export/current');
    const currentEnvelope = await current.json();
    expect(currentEnvelope.data.mode).toBe('backup_exporting');

    const runtimeDiagnostics = await fetch('/api/v1/debug/runtime');
    const taskDiagnostics = await fetch('/api/v1/debug/tasks');
    const databaseDiagnostics = await fetch('/api/v1/debug/databases');
    const runtimeEnvelope = await runtimeDiagnostics.json();
    const taskEnvelope = await taskDiagnostics.json();
    const databaseEnvelope = await databaseDiagnostics.json();
    expect(runtimeEnvelope.data.process.pid).toBeGreaterThan(0);
    expect(taskEnvelope.data.tasks.registeredTypes).toBeGreaterThan(0);
    expect(databaseEnvelope.data.databases.length).toBeGreaterThan(0);
  });

  it('covers committed files, edit session uploads, DNS/resource refresh and certificate detail routes', async () => {
    const fileList = await fetch('/api/v1/applications/app-storefront/files');
    const fileListEnvelope = await fileList.json();
    expect(fileList.status).toBe(200);
    expect(fileListEnvelope.data.some((file: { name: string }) => file.name === 'app.yaml')).toBe(true);

    const fileContent = await fetch('/api/v1/applications/app-storefront/files/app.yaml/content');
    expect(fileContent.status).toBe(200);
    expect(fileContent.headers.get('Content-Type')).toContain('text/yaml');

    const created = await fetch('/api/v1/application-edit-sessions', { method: 'POST', body: JSON.stringify({ applicationId: 'app-storefront' }) });
    const createdEnvelope = await created.json();
    const sessionId = createdEnvelope.data.id;
    const sessionFile = await fetch(`/api/v1/application-edit-sessions/${sessionId}/files/env-template/content`);
    expect(sessionFile.status).toBe(200);
    expect((await sessionFile.text()).includes('HOST')).toBe(true);

    const uploadForm = new FormData();
    uploadForm.set('file', new File(['mock upload content'], 'extra.env', { type: 'text/plain' }));
    uploadForm.set('revision', '1');
    uploadForm.set('name', 'extra.env');
    const uploaded = await fetch(`/api/v1/application-edit-sessions/${sessionId}/uploads/extra.env`, { method: 'PUT', body: uploadForm });
    const uploadedEnvelope = await uploaded.json();
    expect(uploaded.status).toBe(200);
    expect(uploadedEnvelope.data.files.some((file: { name: string }) => file.name === 'extra.env')).toBe(true);

    const facilityCreated = await fetch('/api/v1/facility-apps/reverse-proxy/edit-sessions', { method: 'POST', body: JSON.stringify({ recordId: 'static.example.test' }) });
    const facilityEnvelope = await facilityCreated.json();
    const facilitySessionId = facilityEnvelope.data.id;
    const facilityAsset = await fetch(`/api/v1/facility-apps/reverse-proxy/edit-sessions/${facilitySessionId}/assets/docs%20bundle/content`);
    expect(facilityAsset.status).toBe(200);
    const staticAsset = await fetch('/api/v1/facility-apps/reverse-proxy/static-assets/status%20page/content?recordId=status.example.test');
    expect(staticAsset.status).toBe(200);

    const refreshed = await fetch('/api/v1/dns/domains/domain-example/records/refresh', { method: 'POST' });
    expect(refreshed.status).toBe(202);
    const failed = await fetch('/api/v1/dns/domains/domain-error/records/refresh', { method: 'POST' });
    const failedEnvelope = await failed.json();
    expect(failed.status).toBe(502);
    expect(failedEnvelope.error.code).toBe('dns_provider_error');

    const networks = await fetch('/api/v1/servers/srv-media-syd/networks/refresh', { method: 'POST' });
    expect(networks.status).toBe(202);
    const volumes = await fetch('/api/v1/servers/srv-observability-ams/volumes/refresh', { method: 'POST' });
    expect(volumes.status).toBe(202);

    const certs = await fetch('/api/v1/certificates?pageSize=200');
    const certsEnvelope = await certs.json();
    const certDetail = await fetch(`/api/v1/certificates/${certsEnvelope.data.items[0].id}`);
    expect(certDetail.status).toBe(200);
  });

  it('serves network probe configuration, matrix, and per-node results on real routes', async () => {
    const config = await (await fetch('/api/v1/facility-apps/network-probe')).json();
    expect(config.data).toMatchObject({
      mode: 'all',
      serverIds: [],
      intervalSeconds: 60,
      pingCount: 3,
      pingTimeoutSeconds: 2,
      version: expect.any(Number),
    });

    // 旧 payload 缺省三个节拍参数时必须按默认值补齐，而不是报错或写回 0。
    const legacy = await fetch('/api/v1/facility-apps/network-probe', {
      method: 'PUT',
      body: JSON.stringify({ mode: 'all', serverIds: [] }),
    });
    expect(legacy.status).toBe(200);
    expect((await legacy.json()).data).toMatchObject({ intervalSeconds: 60, pingCount: 3, pingTimeoutSeconds: 2 });

    const invalidSettings: Array<{ body: Record<string, unknown>; code: string }> = [
      // 探测间隔已改为最小 1 分钟：59 秒非法，60 秒合法。
      { body: { mode: 'all', serverIds: [], intervalSeconds: 59 }, code: 'network_probe_interval_invalid' },
      { body: { mode: 'all', serverIds: [], intervalSeconds: 3601 }, code: 'network_probe_interval_invalid' },
      { body: { mode: 'all', serverIds: [], pingCount: 0 }, code: 'network_probe_ping_count_invalid' },
      { body: { mode: 'all', serverIds: [], pingCount: 11 }, code: 'network_probe_ping_count_invalid' },
      { body: { mode: 'all', serverIds: [], pingTimeoutSeconds: 0 }, code: 'network_probe_ping_timeout_invalid' },
      { body: { mode: 'all', serverIds: [], pingTimeoutSeconds: 11 }, code: 'network_probe_ping_timeout_invalid' },
    ];
    for (const item of invalidSettings) {
      const response = await fetch('/api/v1/facility-apps/network-probe', { method: 'PUT', body: JSON.stringify(item.body) });
      expect(response.status, item.code).toBe(422);
      expect((await response.json()).error.code).toBe(item.code);
    }

    // 下界 60 秒（1 分钟）必须被接受。
    const minimum = await fetch('/api/v1/facility-apps/network-probe', {
      method: 'PUT',
      body: JSON.stringify({ mode: 'all', serverIds: [], intervalSeconds: 60 }),
    });
    expect(minimum.status).toBe(200);
    expect((await minimum.json()).data).toMatchObject({ intervalSeconds: 60 });

    const rejected = await fetch('/api/v1/facility-apps/network-probe', {
      method: 'PUT',
      body: JSON.stringify({ mode: 'selected', serverIds: [] }),
    });
    expect(rejected.status).toBe(422);
    expect((await rejected.json()).error.code).toBe('network_probe_servers_required');

    const unknown = await fetch('/api/v1/facility-apps/network-probe', {
      method: 'PUT',
      body: JSON.stringify({ mode: 'selected', serverIds: ['srv-missing'] }),
    });
    expect(unknown.status).toBe(404);
    expect((await unknown.json()).error.code).toBe('network_probe_server_not_found');

    const saved = await fetch('/api/v1/facility-apps/network-probe', {
      method: 'PUT',
      body: JSON.stringify({ mode: 'selected', serverIds: ['srv-edge-sgp'], intervalSeconds: 120, pingCount: 5, pingTimeoutSeconds: 4 }),
    });
    expect(saved.status).toBe(200);
    expect((await saved.json()).data).toMatchObject({
      mode: 'selected',
      serverIds: ['srv-edge-sgp'],
      intervalSeconds: 120,
      pingCount: 5,
      pingTimeoutSeconds: 4,
    });
    const persisted = await (await fetch('/api/v1/facility-apps/network-probe')).json();
    expect(persisted.data).toMatchObject({ intervalSeconds: 120, pingCount: 5, pingTimeoutSeconds: 4 });

    const matrix = await (await fetch('/api/v1/facility-apps/network-probe/results')).json();
    expect(matrix.data.mode).toBe('selected');
    expect(matrix.data.nodes.map((node: { serverId: string }) => node.serverId)).toEqual(['srv-edge-sgp']);
    expect(matrix.data.results.length).toBeGreaterThan(0);

    // view=targets：按目标聚合、覆盖全部服务器（无结果也保留空摘要），history=1 附带跨源历史。
    const targetView = await (await fetch('/api/v1/facility-apps/network-probe/results?view=targets&history=1')).json();
    expect(targetView.data.nodes.length).toBeGreaterThanOrEqual(15);
    const coreTarget = targetView.data.nodes.find((node: { serverId: string }) => node.serverId === 'srv-core-fra');
    expect(coreTarget).toMatchObject({ name: expect.any(String), address: expect.any(String), sourceCount: 1, stale: expect.any(Boolean) });
    expect(Array.isArray(coreTarget.history)).toBe(true);
    expect(coreTarget.history.length).toBeGreaterThan(0);
    const selfTarget = targetView.data.nodes.find((node: { serverId: string }) => node.serverId === 'srv-edge-sgp');
    expect(selfTarget.sourceCount).toBe(0);
    expect(targetView.data.updatedAt).not.toBeNull();

    // 不带 history 时按后端 omitempty 语义省略历史字段。
    const targetsWithoutHistory = await (await fetch('/api/v1/facility-apps/network-probe/results?view=targets')).json();
    expect(targetsWithoutHistory.data.nodes[0].history).toBeUndefined();

    const source = await (await fetch('/api/v1/servers/srv-edge-sgp/network-probes')).json();
    expect(source.data).toMatchObject({ serverId: 'srv-edge-sgp', enabled: true });
    expect(source.data.nodes.some((node: { serverId: string }) => node.serverId === 'srv-core-fra')).toBe(true);
    expect(source.data.results.every((result: { sourceServerId: string }) => result.sourceServerId === 'srv-edge-sgp')).toBe(true);

    // includeHistory=true 时必须附带 24h 历史点。
    const withHistory = await (await fetch('/api/v1/servers/srv-edge-sgp/network-probes?history=1')).json();
    expect(withHistory.data.results.every((result: { history?: unknown[] }) => Array.isArray(result.history) && result.history.length > 0)).toBe(true);

    const nonProbe = await (await fetch('/api/v1/servers/srv-core-fra/network-probes')).json();
    expect(nonProbe.data.enabled).toBe(false);
    expect(nonProbe.data.nodes).toEqual([]);

    expect((await fetch('/api/v1/servers/srv-missing/network-probes')).status).toBe(404);
  });

  it('serves and validates metrics facility configuration on real routes', async () => {
    const initial = await (await fetch('/api/v1/facility-apps/metrics')).json();
    expect(initial.data).toMatchObject({
      mode: 'all',
      serverIds: [],
      retentionDays: 7,
      collectionIntervalSeconds: 60,
      cleanupSchedule: 'daily',
      version: 0,
    });

    const validBody = { mode: 'all', serverIds: [] as string[], retentionDays: 7, collectionIntervalSeconds: 60, cleanupSchedule: 'daily' };
    const invalid: Array<{ body: Record<string, unknown>; code: string; status?: number }> = [
      { body: { ...validBody, mode: 'every' }, code: 'metrics_facility_mode_invalid' },
      { body: { ...validBody, mode: 'selected', serverIds: [] }, code: 'metrics_facility_servers_required' },
      { body: { ...validBody, retentionDays: 0 }, code: 'metrics_facility_retention_invalid' },
      { body: { ...validBody, collectionIntervalSeconds: 0 }, code: 'metrics_facility_interval_invalid' },
      { body: { ...validBody, cleanupSchedule: 'monthly' }, code: 'metrics_facility_cleanup_schedule_invalid' },
      { body: { ...validBody, mode: 'selected', serverIds: ['srv-missing'] }, code: 'metrics_facility_server_not_found' },
    ];
    for (const item of invalid) {
      const response = await fetch('/api/v1/facility-apps/metrics', { method: 'PUT', body: JSON.stringify(item.body) });
      expect(response.status, item.code).toBe(item.status ?? 422);
      expect((await response.json()).error.code).toBe(item.code);
    }
    // 任一校验失败都不得写库、不得 bump version。
    expect((await (await fetch('/api/v1/facility-apps/metrics')).json()).data.version).toBe(0);

    const saved = await fetch('/api/v1/facility-apps/metrics', {
      method: 'PUT',
      body: JSON.stringify({ mode: 'selected', serverIds: ['srv-edge-sgp'], retentionDays: 14, collectionIntervalSeconds: 120, cleanupSchedule: 'hourly' }),
    });
    expect(saved.status).toBe(200);
    expect((await saved.json()).data).toMatchObject({
      mode: 'selected',
      serverIds: ['srv-edge-sgp'],
      retentionDays: 14,
      collectionIntervalSeconds: 120,
      cleanupSchedule: 'hourly',
      version: 1,
    });

    const persisted = await (await fetch('/api/v1/facility-apps/metrics')).json();
    expect(persisted.data).toMatchObject({
      mode: 'selected',
      serverIds: ['srv-edge-sgp'],
      retentionDays: 14,
      collectionIntervalSeconds: 120,
      cleanupSchedule: 'hourly',
    });

    // all 模式忽略 serverIds，与后端归一化一致。
    const normalized = await fetch('/api/v1/facility-apps/metrics', {
      method: 'PUT',
      body: JSON.stringify({ mode: 'all', serverIds: ['srv-edge-sgp'], retentionDays: 7, collectionIntervalSeconds: 60, cleanupSchedule: 'daily' }),
    });
    expect((await normalized.json()).data).toMatchObject({ mode: 'all', serverIds: [] });
  });

});
