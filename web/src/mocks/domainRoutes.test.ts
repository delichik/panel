// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { installMockApi } from './browser';

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

    const tasks = await fetch('/api/v1/tasks?pageSize=12&page=1');
    const tasksEnvelope = await tasks.json();
    expect(tasksEnvelope.data.total).toBeGreaterThan(80);
    expect(tasksEnvelope.data.items.length).toBe(12);

    const secondPage = await fetch('/api/v1/tasks?pageSize=12&page=2');
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

    const events = await fetch('/api/v1/system-events?pageSize=20&page=1');
    const eventsEnvelope = await events.json();
    expect(eventsEnvelope.data.total).toBeGreaterThan(40);
    expect(eventsEnvelope.data.items.length).toBe(20);
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
    expect(containersEnvelope.data.length).toBeGreaterThan(0);

    const volumes = await fetch('/api/v1/servers/srv-observability-ams/volumes');
    const volumesEnvelope = await volumes.json();
    expect(volumes.status).toBe(200);
    expect(volumesEnvelope.data.length).toBeGreaterThan(0);
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

    const logs = await fetch('/api/v1/applications/app-worker/logs');
    const logsEnvelope = await logs.json();
    expect(logs.status).toBe(503);
    expect(logsEnvelope.error.code).toBe('application_logs_unavailable');
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

  it('serves facility reverse proxy edit session preview on real routes', async () => {
    const created = await fetch('/api/v1/facility-apps/reverse-proxy/edit-sessions', { method: 'POST', body: JSON.stringify({}) });
    const createdEnvelope = await created.json();
    const preview = await fetch(`/api/v1/facility-apps/reverse-proxy/edit-sessions/${createdEnvelope.data.id}/preview`, {
      method: 'POST',
      body: JSON.stringify({ revision: createdEnvelope.data.revision }),
    });
    const previewEnvelope = await preview.json();

    expect(preview.status).toBe(200);
    expect(previewEnvelope.data.token.action).toBe('facility.reverse_proxy.commit');
  });

  it('serves DNS records and exposes provider failures on real routes', async () => {
    const domains = await fetch('/api/v1/dns/domains');
    const domainsEnvelope = await domains.json();
    expect(domainsEnvelope.data.total).toBeGreaterThanOrEqual(12);
    expect(domainsEnvelope.data.items.length).toBe(domainsEnvelope.data.total);

    const records = await fetch('/api/v1/dns/domains/domain-example/records');
    const recordsEnvelope = await records.json();
    expect(records.status).toBe(200);
    expect(recordsEnvelope.data.some((record: { type: string }) => record.type === 'TXT')).toBe(true);

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
    const tasks = await fetch('/api/v1/tasks?operationPage=true&pageSize=80');
    const tasksEnvelope = await tasks.json();
    expect(tasks.status).toBe(200);
    expect(tasksEnvelope.data.items.some((task: { operationId: string }) => task.operationId === 'op-deploy-storefront')).toBe(true);

    const steps = await fetch('/api/v1/tasks/task-deploy-3/steps');
    const stepsEnvelope = await steps.json();
    expect(stepsEnvelope.data.some((step: { status: string }) => step.status === 'failed_retryable')).toBe(true);

    const logs = await fetch('/api/v1/tasks/task-deploy-3/logs');
    const logsEnvelope = await logs.json();
    expect(logsEnvelope.data.logs.length).toBeGreaterThan(30);

    const retry = await fetch('/api/v1/tasks/task-deploy-3/retry', { method: 'POST' });
    expect(retry.status).toBe(202);

    const runNow = await fetch('/api/v1/tasks/task-backup-1/run-now', { method: 'POST' });
    expect(runNow.status).toBe(202);
  });

  it('serves settings, backup, maintenance, and diagnostics routes', async () => {
    const settings = await fetch('/api/v1/settings/runtime');
    const settingsEnvelope = await settings.json();
    expect(settings.status).toBe(200);
    expect(settingsEnvelope.data.cleanupSchedule).toBe('daily');

    const conflict = await fetch('/api/v1/settings/runtime', {
      method: 'PUT',
      body: JSON.stringify({ ...settingsEnvelope.data, logLevel: 'debug', remoteCommandTimeoutSeconds: 13 }),
    });
    const conflictEnvelope = await conflict.json();
    expect(conflict.status).toBe(409);
    expect(conflictEnvelope.error.code).toBe('settings_conflict');

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

    const diagnostics = await fetch('/api/v1/debug/snapshot');
    const diagnosticsEnvelope = await diagnostics.json();
    expect(diagnosticsEnvelope.data.databases.length).toBeGreaterThan(0);
  });
});
