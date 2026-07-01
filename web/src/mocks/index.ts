import type { TaskDto } from '@/types/api';
import { createMockState, mockEarlier } from './data';

let state = createMockState();
let sequence = 100;

export function resetMockApi() {
  state = createMockState();
  sequence = 100;
}

export function isMockApiEnabled() {
  return import.meta.env.VITE_PANEL_TEST_MODE === 'true';
}

export function prepareMockSession() {
  if (isMockApiEnabled() && !globalThis.localStorage?.getItem('authToken')) {
    globalThis.localStorage?.setItem('authToken', 'mock-session-token');
  }
}

function envelope(data: unknown, status = 200, headers: HeadersInit = {}) {
  return new Response(JSON.stringify({ data, error: null }), { status, headers: { 'Content-Type': 'application/json', ...headers } });
}

function failure(method: string, path: string) {
  return new Response(JSON.stringify({ data: null, error: { code: 'mock_route_not_found', message: `Mock API route is not implemented: ${method} ${path}` } }), { status: 501, headers: { 'Content-Type': 'application/json' } });
}

async function jsonBody(request: Request) {
  if (!request.body || !request.headers.get('content-type')?.includes('application/json')) return {} as Record<string, unknown>;
  return await request.json() as Record<string, unknown>;
}

function nextId(prefix: string) {
  sequence += 1;
  return `${prefix}-${sequence}`;
}

function taskCreated(type: string, serverId: string | null = null) {
  const task: TaskDto = { id: nextId('task'), type, serverId, status: 'queued', stage: 'preparing', percentage: 0, summary: `Mock ${type}`, retryCount: 0, maxRetries: 2, createdAt: new Date().toISOString(), startedAt: null, finishedAt: null, allowRunNow: false, allowRetry: false };
  state.tasks.unshift(task as unknown as typeof state.tasks[number]);
  return task;
}

function metrics(card: Record<string, unknown>) {
  const points = Array.from({ length: 12 }, (_, index) => new Date(Date.now() - (11 - index) * 300_000).toISOString());
  const series = (offset: number) => ({ range: card.range ?? '1h', cpu: points.map((time, i) => ({ time, usagePercent: 20 + offset + (i % 4) * 5 })), memory: points.map((time, i) => ({ time, usedBytes: (3 + offset / 10 + i / 20) * 1024 ** 3, totalBytes: 8 * 1024 ** 3 })), disk: points.map((time, i) => ({ time, usedBytes: (42 + offset + i / 10) * 1024 ** 3, totalBytes: 120 * 1024 ** 3 })), network: points.map((time, i) => ({ time, rxBytesPerSecond: 100_000 + i * 12_000, txBytesPerSecond: 70_000 + i * 8_000 })), load: points.map((time, i) => ({ time, load1: .3 + i / 50, load5: .25 + i / 60, load15: .2 + i / 70 })) });
  const serverIds = Array.isArray(card.serverIds) && card.serverIds.length ? card.serverIds as string[] : ['srv-edge', 'srv-db'];
  return { card, metricsByServer: Object.fromEntries(serverIds.map((serverId, index) => [serverId, series(index * 12)])) };
}

function applicationLastError(app: typeof state.applications[number]) {
  return 'lastError' in app ? app.lastError : undefined;
}

function runtimeForApplication(app: typeof state.applications[number]) {
  const observedAt = new Date().toISOString();
  const lastError = applicationLastError(app);
  if (app.id === 'app-api') {
    return {
      applicationId: app.id,
      runtimeId: `runtime-${app.id}`,
      status: 'failed',
      operation: { id: 'op-api-deploy', applicationId: app.id, type: 'deploy', status: 'failed', taskId: 'task-hard-failed', generation: app.generation, specHash: app.specHash, trigger: 'manual', error: lastError, targets: [{ id: 'target-api-edge', operationId: 'op-api-deploy', applicationId: app.id, serverId: 'srv-edge', serverName: '新加坡边缘节点', status: 'running', desiredState: 'running', instanceId: 'app-api-srv-edge', containerName: 'api-service', containerId: 'ctr-api-edge', stage: 'running', createdAt: mockEarlier, startedAt: mockEarlier, updatedAt: observedAt }, { id: 'target-api-staging', operationId: 'op-api-deploy', applicationId: app.id, serverId: 'srv-staging', serverName: '首尔预发节点', status: 'failed', desiredState: 'running', instanceId: 'app-api-srv-staging', containerName: 'api-service', containerId: 'ctr-api', stage: 'verifying', error: lastError, createdAt: mockEarlier, startedAt: mockEarlier, finishedAt: observedAt, updatedAt: observedAt }], createdAt: mockEarlier, startedAt: mockEarlier, finishedAt: observedAt, updatedAt: observedAt },
      instances: [
        { instanceId: 'app-api-srv-edge', serverId: 'srv-edge', serverName: '新加坡边缘节点', containerName: 'api-service', containerId: 'ctr-api-edge', status: 'running', desiredState: 'running', stage: 'running', image: app.imageReference, startedAt: mockEarlier, observedAt },
        { instanceId: 'app-api-srv-staging', serverId: 'srv-staging', serverName: '首尔预发节点', containerName: 'api-service', containerId: 'ctr-api', status: 'failed', desiredState: 'running', stage: 'verifying', image: app.imageReference, startedAt: mockEarlier, finishedAt: observedAt, exitCode: 1, lastError, observedAt },
      ],
      observedAt,
    };
  }
  if (app.id === 'app-admin') {
    return {
      applicationId: app.id,
      runtimeId: `runtime-${app.id}`,
      status: 'deploying',
      operation: { id: 'op-admin-deploy', applicationId: app.id, type: 'deploy', status: 'running', taskId: 'task-running', generation: app.generation, specHash: app.specHash, trigger: 'manual', createdAt: mockEarlier, startedAt: mockEarlier, updatedAt: observedAt },
      instances: [{ instanceId: 'app-admin-srv-edge', serverId: 'srv-edge', serverName: '新加坡边缘节点', containerName: 'admin-console', status: 'pending', desiredState: 'running', stage: 'pulling_image', image: app.imageReference, observedAt }],
      observedAt,
    };
  }
  return {
    applicationId: app.id,
    runtimeId: `runtime-${app.id}`,
    status: app.runtimeStatus ?? 'unknown',
    instances: app.enabled ? [
      { instanceId: `${app.id}-srv-edge`, serverId: 'srv-edge', serverName: '新加坡边缘节点', containerName: app.name, containerId: 'ctr-web', status: 'running', desiredState: 'running', image: app.imageReference, startedAt: mockEarlier, observedAt },
      ...(app.id === 'app-web' ? [{ instanceId: `${app.id}-srv-db`, serverId: 'srv-db', serverName: '东京数据节点', containerName: app.name, status: 'pending', desiredState: 'running', stage: 'waiting_for_agent', image: app.imageReference, lastError: 'agent unavailable', observedAt }] : []),
      ...(app.id === 'app-web' ? Array.from({ length: 120 }, (_, index) => ({ instanceId: `${app.id}-page-${String(index + 1).padStart(2, '0')}`, serverId: index % 2 === 0 ? 'srv-edge' : 'srv-db', serverName: index % 2 === 0 ? '新加坡边缘节点' : '东京数据节点', containerName: `${app.name}-page-${index + 1}`, containerId: `ctr-web-page-${index + 1}`, status: index % 5 === 0 ? 'failed' : index % 3 === 0 ? 'pending' : 'running', desiredState: 'running', stage: index % 5 === 0 ? 'verifying' : 'running', image: app.imageReference, startedAt: mockEarlier, lastError: index % 5 === 0 ? 'mock instance failed health check' : undefined, observedAt })) : []),
    ] : [],
    observedAt,
  };
}

export const mockFetch: typeof fetch = async (input, init = {}) => {
  const request = input instanceof Request ? input : new Request(new URL(String(input), globalThis.location?.origin ?? 'http://localhost'), init);
  const url = new URL(request.url);
  const method = request.method.toUpperCase();
  const path = url.pathname.replace(/^\/api\/v1/, '') || '/';
  await new Promise((resolve) => setTimeout(resolve, 20));

  if (path === '/auth/session' && method === 'GET') return envelope({ authenticated: state.authenticated, token: 'mock-session-token', username: 'demo-admin', passwordChangeRequired: false });
  if (path === '/auth/login' && method === 'POST') { state.authenticated = true; return envelope({ authenticated: true, token: 'mock-session-token', username: 'demo-admin', passwordChangeRequired: false }); }
  if (path === '/auth/logout' && method === 'POST') { state.authenticated = false; return envelope({ authenticated: false }); }
  if ((path === '/auth/account' || path === '/auth/jwt-secret') && method === 'POST') return envelope({ authenticated: true, token: 'mock-session-token', username: 'demo-admin', passwordChangeRequired: false });
  if (path === '/settings/public-branding' && method === 'GET') return envelope(state.runtimeSettings.branding);
  if (path === '/settings/runtime' && method === 'GET') return envelope(state.runtimeSettings);
  if (path === '/settings/runtime' && method === 'PUT') { Object.assign(state.runtimeSettings, await jsonBody(request)); return envelope(state.runtimeSettings); }
  if (path === '/system/version' && method === 'GET') return envelope({ version: '0.2.0-mock', channel: 'dev', commit: 'mock-api', repository: 'local/frontend-test', latestVersion: '0.2.0-mock', updateAvailable: false, checkedAt: new Date().toISOString() });

  if (path === '/servers' && method === 'GET') return envelope(state.servers);
  if (path === '/servers' && method === 'POST') { const body = await jsonBody(request); const row = { ...body, id: nextId('srv'), reachable: true, loadAverage: '0.10 0.08 0.05', lastCheckedAt: new Date().toISOString(), createdAt: new Date().toISOString(), updatedAt: new Date().toISOString() }; state.servers.push(row as typeof state.servers[number]); return envelope(row, 201); }
  if (path === '/servers/probe' && method === 'POST') return envelope({ reachable: true, passwordlessSudo: true, root: false, privileged: true, privilegeMode: 'passwordless_sudo', os: { id: 'ubuntu', versionId: '24.04', prettyName: 'Ubuntu 24.04 LTS', supported: true }, traits: { region: 'mock' } });
  if (path === '/credentials' && method === 'GET') return envelope(state.credentials);
  if (path === '/credentials' && method === 'POST') { const body = await jsonBody(request); const row = { ...body, id: nextId('cred'), createdAt: new Date().toISOString(), updatedAt: new Date().toISOString() }; state.credentials.push(row as typeof state.credentials[number]); return envelope(row, 201); }

  let match = path.match(/^\/credentials\/([^/]+)$/);
  if (match) { const index = state.credentials.findIndex((item) => item.id === match![1]); if (method === 'DELETE') { if (index >= 0) state.credentials.splice(index, 1); return new Response(null, { status: 204 }); } if (method === 'PUT') { const body = await jsonBody(request); state.credentials[index] = { ...state.credentials[index], ...body, updatedAt: new Date().toISOString() } as typeof state.credentials[number]; return envelope(state.credentials[index]); } }
  match = path.match(/^\/servers\/([^/]+)$/);
  if (match) { const index = state.servers.findIndex((item) => item.id === match![1]); if (method === 'DELETE') { if (index >= 0) state.servers.splice(index, 1); return new Response(null, { status: 204 }); } if (method === 'PUT') { const body = await jsonBody(request); state.servers[index] = { ...state.servers[index], ...body, updatedAt: new Date().toISOString() } as typeof state.servers[number]; return envelope(state.servers[index]); } }
  match = path.match(/^\/servers\/([^/]+)\/(test|restart|agent\/deploy|ufw\/install)$/);
  if (match && method === 'POST') { if (match[2] === 'test') return envelope(state.servers.find((item) => item.id === match![1])); return envelope({ taskId: taskCreated(match[2], match[1]).id }, 202); }
  match = path.match(/^\/servers\/([^/]+)\/ufw$/);
  if (match && method === 'GET') return envelope(state.ufw[match[1]] ?? { serverId: match[1], supported: true, installed: false, active: false, status: 'not installed', defaultPolicy: '', rules: [] });
  match = path.match(/^\/servers\/([^/]+)\/ufw\/enable$/);
  if (match && method === 'POST') { if (state.ufw[match[1]]) state.ufw[match[1]].active = true; return envelope({ taskId: taskCreated('ufw.enable', match[1]).id }, 202); }
  match = path.match(/^\/servers\/([^/]+)\/ufw\/rules(?:\/(\d+))?$/);
  if (match) { const current = state.ufw[match[1]]; if (method === 'POST') { const body = await jsonBody(request); current.rules.push({ number: current.rules.length + 1, to: `${body.port}/${body.protocol}`, action: 'ALLOW IN', from: String(body.from || 'Anywhere') }); return envelope(current); } if (method === 'DELETE') { current.rules = current.rules.filter((rule) => rule.number !== Number(match![2])); return envelope(current); } }
  match = path.match(/^\/servers\/([^/]+)\/fail2ban$/);
  if (match && method === 'GET') return envelope(state.fail2ban[match[1]] ?? { serverId: match[1], installed: false, active: false, managed: false, panelConfigPresent: false, jails: [], raw: '', configYaml: 'jails:\n  - name: sshd\n    enabled: true\n    preset: ssh\n    filter: sshd\n    port: ssh\n    logpath: /var/log/auth.log\n    backend: systemd\n    maxretry: 5\n    findtime: 10m\n    bantime: 1h\n    ignoreip:\n      - 127.0.0.1/8\n', config: { jails: [{ name: 'sshd', enabled: true, preset: 'ssh', filter: 'sshd', port: 'ssh', logpath: '/var/log/auth.log', backend: 'systemd', maxretry: 5, findtime: '10m', bantime: '1h', ignoreip: ['127.0.0.1/8'], options: {} }] }, updatedAt: undefined });
  if (match && method === 'PUT') { const body = await jsonBody(request); const current = state.fail2ban[match[1]] ?? { serverId: match[1], installed: false, active: false, managed: false, panelConfigPresent: false, jails: [], raw: '', configYaml: '', config: { jails: [] } }; current.configYaml = String(body.configYaml || ''); current.updatedAt = new Date().toISOString(); state.fail2ban[match[1]] = current; return envelope(current); }
  match = path.match(/^\/servers\/([^/]+)\/fail2ban\/enable$/);
  if (match && method === 'POST') { const body = await jsonBody(request); const current = state.fail2ban[match[1]] ?? { serverId: match[1], installed: false, active: false, managed: false, panelConfigPresent: false, jails: [], raw: '', configYaml: '', config: { jails: [] } }; current.configYaml = String(body.configYaml || current.configYaml || ''); current.installed = true; current.active = true; current.managed = true; current.panelConfigPresent = true; current.jails = ['sshd']; current.updatedAt = new Date().toISOString(); state.fail2ban[match[1]] = current; return envelope({ taskId: taskCreated('server_fail2ban_apply', match[1]).id }, 202); }
  match = path.match(/^\/servers\/([^/]+)\/fail2ban\/release$/);
  if (match && method === 'POST') { const current = state.fail2ban[match[1]]; if (current) { current.managed = false; current.panelConfigPresent = false; current.updatedAt = new Date().toISOString(); } return envelope({ taskId: taskCreated('server_fail2ban_apply', match[1]).id }, 202); }
  match = path.match(/^\/servers\/([^/]+)\/fail2ban\/install$/);
  if (match && method === 'POST') { const current = state.fail2ban[match[1]]; if (current) { current.installed = true; current.active = true; current.managed = true; current.panelConfigPresent = true; } return envelope({ taskId: taskCreated('server_fail2ban_apply', match[1]).id }, 202); }
  match = path.match(/^\/servers\/([^/]+)\/packages\/updates$/);
  if (match && method === 'GET') return envelope({ serverId: match[1], lastRefreshedAt: new Date().toISOString(), updates: state.packageUpdates, refreshing: false });
  match = path.match(/^\/servers\/([^/]+)\/packages\/(refresh|upgrade-selected|upgrade-all)$/);
  if (match && method === 'POST') { const task = taskCreated(`packages.${match[2]}`, match[1]); return envelope(match[2] === 'refresh' ? { serverId: match[1], refreshing: true, taskId: task.id } : { taskId: task.id }, 202); }

  if (path === '/overview' && method === 'GET') return envelope({ servers: state.servers.map((server) => ({ id: server.id, name: server.name, host: server.host, supported: server.os?.supported ?? false, reachable: server.reachable, metricsFresh: server.reachable, packageUpdateCount: server.reachable ? state.packageUpdates.length : 0, loadAverage: server.loadAverage, lastMetricsAt: server.lastCheckedAt, lastPackageRefreshAt: server.lastCheckedAt })) });
  if (path === '/overview/cards' && method === 'GET') return envelope({ cards: state.overviewCards });
  if (path === '/overview/cards' && method === 'PUT') { const body = await jsonBody(request); state.overviewCards = (body.cards as typeof state.overviewCards | undefined) ?? state.overviewCards; return envelope({ cards: state.overviewCards }); }
  match = path.match(/^\/overview\/cards\/([^/]+)\/data$/);
  if (match && method === 'GET') { const card = state.overviewCards.find((item) => item.id === match![1]) ?? { id: match[1], kind: 'cpu', width: 6, height: 4, range: '1h', networkDirection: 'both', serverIds: ['srv-edge', 'srv-db'] }; return envelope(metrics(card)); }

  if (path === '/applications' && method === 'GET') return envelope(state.applications.filter((app: any) => app.kind !== 'facility_application' && !app.deletionRequested));
  if (path === '/applications' && method === 'POST') { const body = await jsonBody(request); const row = { ...body, id: nextId('app'), kind: 'application', deletionRequested: false, generation: 1, specHash: 'sha256:new', jobId: String(body.name ?? 'new-app'), namespace: 'default', runtimeStatus: body.enabled ? 'pending' : 'stopped', allocationCount: 0, createdAt: new Date().toISOString(), updatedAt: new Date().toISOString() }; state.applications.push(row as typeof state.applications[number]); return envelope(row, 201); }
  if (path === '/application-template-catalog' && method === 'GET') return envelope({ variables: [{ key: 'server.name', category: 'server', specExpression: '${server.name}', templateExpression: '{{ server.name }}' }, { key: 'certificate.WEB_TLS.certificate', category: 'certificate', specExpression: '${certificate.WEB_TLS.certificate}', templateExpression: '{{ certificate.WEB_TLS.certificate }}' }], panelFiles: [{ id: 'panel-nginx', resourceId: 'app-web', resourceType: 'application', name: 'nginx.conf', kind: 'template', source: 'panel' }] });
  if (path === '/facility-apps/reverse-proxy' && method === 'GET') return envelope(state.facilityReverseProxy);
  if (path === '/facility-apps/reverse-proxy' && method === 'PUT') {
    const body = await jsonBody(request);
    state.facilityReverseProxy = {
      ...state.facilityReverseProxy,
      deploymentServers: Array.isArray(body.deploymentServers) ? body.deploymentServers : [],
      image: String(body.image || 'nginx:1.27-alpine'),
      staticSites: Array.isArray(body.staticSites) ? body.staticSites : [],
      enabledServers: Array.isArray(body.deploymentServers) ? body.deploymentServers : [],
      routeSummaries: Array.isArray(body.staticSites) ? body.staticSites.map((site: any) => ({ domain: site.domain, path: site.path || '/', source: 'static_site', serverIds: site.deploymentServers?.length ? site.deploymentServers : body.deploymentServers ?? [], httpsStatus: 'disabled' })) : [],
      routes: Array.isArray(body.staticSites) ? body.staticSites.length : 0,
      updatedAt: new Date().toISOString(),
    };
    return envelope(state.facilityReverseProxy);
  }
  if (path === '/facility-apps/reverse-proxy/reconcile' && method === 'POST') return envelope({ config: state.facilityReverseProxy });
  if (path === '/facility-apps/reverse-proxy/static-assets' && method === 'GET') return envelope(state.facilityReverseProxy.staticAssets ?? []);
  if (path === '/facility-apps/reverse-proxy/static-assets' && method === 'POST') {
    const form = await request.formData();
    const file = form.get('file') as File | null;
    const nowText = new Date().toISOString();
    const asset = { id: nextId('facility_static'), name: String(form.get('name') || file?.name || 'Static asset'), kind: String(form.get('kind') || 'uploaded_file'), filename: file?.name || 'asset.bin', size: file?.size || 0, sha256: 'sha256:mock-static-upload', createdAt: nowText, updatedAt: nowText };
    state.facilityReverseProxy.staticAssets = [asset, ...(state.facilityReverseProxy.staticAssets ?? [])];
    return envelope(asset, 201);
  }
  match = path.match(/^\/facility-apps\/reverse-proxy\/static-assets\/([^/]+)$/);
  if (match && method === 'DELETE') { state.facilityReverseProxy.staticAssets = (state.facilityReverseProxy.staticAssets ?? []).filter((asset: any) => asset.id !== match![1]); return new Response(null, { status: 204 }); }
  if (path === '/application-save-sessions' && method === 'POST') return envelope({ id: nextId('save'), expiresAt: '2026-06-22T09:00:00Z', files: [] }, 201);
  match = path.match(/^\/application-save-sessions\/([^/]+)\/(files|files\/archive|files\/delete|commit)$/);
  if (match && method === 'POST') {
    if (match[2] === 'commit') return envelope(state.applications[0]);
    if (match[2] === 'files/archive') {
      const form = await request.formData();
      const basePath = String(form.get('basePath') || 'public').replace(/^\/+|\/+$/g, '');
      const kind = String(form.get('kind') || 'binary');
      return envelope([{ id: nextId('file'), applicationId: '', path: `${basePath || 'public'}/index.html`, kind, contentType: 'text/html', size: 256, sha256: 'sha256:mock-archive', createdAt: new Date().toISOString(), updatedAt: new Date().toISOString() }]);
    }
    const body = await jsonBody(request);
    return match[2] === 'files/delete' ? new Response(null, { status: 204 }) : envelope({ id: nextId('file'), applicationId: '', path: body.path, kind: body.kind, contentType: body.contentType || 'text/plain', size: 128, sha256: 'sha256:mock', createdAt: new Date().toISOString(), updatedAt: new Date().toISOString() });
  }
  match = path.match(/^\/applications\/([^/]+)(?:\/(.*))?$/);
  if (match) {
    const app = state.applications.find((item) => item.id === match![1]); const suffix = match[2] ?? '';
    if (!suffix && method === 'GET') return envelope(app);
    if (!suffix && method === 'PUT') { const body = await jsonBody(request); const index = state.applications.findIndex((item) => item.id === match![1]); state.applications[index] = { ...state.applications[index], ...body, updatedAt: new Date().toISOString() } as typeof state.applications[number]; return envelope(state.applications[index]); }
    if (!suffix && method === 'DELETE') { state.applications = state.applications.filter((item) => item.id !== match![1]); return new Response(null, { status: 204 }); }
    if (suffix === 'files' && method === 'GET') return envelope([{ id: 'file-nginx', applicationId: match[1], path: 'nginx.conf', kind: 'template', contentType: 'text/plain', size: 256, sha256: 'sha256:file', createdAt: new Date().toISOString(), updatedAt: new Date().toISOString() }, { id: 'file-env', applicationId: match[1], path: 'config/production.env', kind: 'binary', contentType: 'text/plain', size: 4096, sha256: 'sha256:env', createdAt: mockEarlier, updatedAt: new Date().toISOString() }, ...Array.from({ length: 120 }, (_, index) => ({ id: `file-page-${index + 1}`, applicationId: match![1], path: `config/generated/page-${String(index + 1).padStart(2, '0')}.conf`, kind: index % 2 === 0 ? 'template' : 'binary', contentType: 'text/plain', size: 512 + index * 64, sha256: `sha256:file-page-${index + 1}`, createdAt: mockEarlier, updatedAt: new Date().toISOString() }))]);
    if (suffix === 'files' && method === 'POST') { const body = await jsonBody(request); return envelope({ id: nextId('file'), applicationId: match[1], path: body.path, kind: body.kind, contentType: body.contentType || 'text/plain', size: 128, sha256: 'sha256:mock', createdAt: new Date().toISOString(), updatedAt: new Date().toISOString() }, 201); }
    if (suffix.startsWith('files/') && method === 'DELETE') return new Response(null, { status: 204 });
    if (suffix === 'runtime' && method === 'GET' && app) return envelope(runtimeForApplication(app));
    if (suffix.startsWith('logs') && method === 'GET') { const lastError = app ? applicationLastError(app) : undefined; return envelope({ instanceId: url.searchParams.get('instanceId') ?? '', containerName: app?.name ?? '', type: url.searchParams.get('type') ?? 'stdout', logs: `[mock] ${app?.name ?? 'application'} service started\n[mock] health check passed\n[mock] warning: configuration reload took 840ms\n${lastError ? `[mock] error: ${lastError}\n` : ''}` }); }
    if ((suffix === 'package' || suffix === 'persistent-data') && method === 'GET') return new Response(`Mock archive for ${app?.name}`, { headers: { 'Content-Type': 'application/octet-stream', 'Content-Disposition': `attachment; filename="${app?.name}-${suffix}.tar.gz"` } });
    if (['deploy', 'stop', 'restart', 'migrate', 'image/update'].includes(suffix) && method === 'POST') return envelope({ taskId: taskCreated(`application.${suffix}`, 'srv-edge').id, application: app }, 202);
    if (suffix === 'validate' && method === 'POST') return envelope({ valid: true, issues: [] });
    if (suffix === 'image/check' && method === 'POST') return envelope(app);
    if (suffix === 'persistent-data' && method === 'POST') return envelope({ taskId: taskCreated('application.restore', 'srv-edge').id }, 202);
  }

  match = path.match(/^\/servers\/([^/]+)\/(containers|images|networks|volumes)(?:\/(.*))?$/);
  if (match) {
    const resource = match[2]; const suffix = match[3] ?? '';
    if (resource === 'containers' && !suffix && method === 'GET') return envelope(state.containers);
    if (resource === 'containers' && suffix.endsWith('/logs') && method === 'GET') return envelope({ containerId: decodeURIComponent(suffix.split('/')[0]), logs: '[mock] container log line 1\n[mock] container log line 2\n' });
    if (resource === 'containers' && suffix && method === 'POST') return envelope({ refreshTaskId: taskCreated('container.action', match[1]).id }, 202);
    if (resource === 'containers' && suffix && method === 'DELETE') { state.containers = state.containers.filter((item) => item.id !== decodeURIComponent(suffix)); return envelope({}); }
    if (resource === 'images' && !suffix && method === 'GET') return envelope({ serverId: match[1], items: state.images, lastRefreshedAt: new Date().toISOString(), refreshing: false });
    if (resource === 'images' && suffix === 'refresh' && method === 'POST') return envelope({ taskId: taskCreated('images.refresh', match[1]).id }, 202);
    if (resource === 'images' && suffix && method !== 'GET') return envelope({ refreshTaskId: taskCreated('images.action', match[1]).id }, 202);
    if (resource === 'networks' && method === 'GET') return envelope(state.networks);
    if (resource === 'volumes' && !suffix && method === 'GET') return envelope(state.volumes);
    if (resource === 'volumes' && method !== 'GET') return envelope({ refreshTaskId: taskCreated('volumes.delete', match[1]).id }, 202);
  }
  if ((path === '/images/upgrade-selected' || path === '/images/upgrade-all') && method === 'POST') return envelope({ taskId: taskCreated('images.upgrade').id }, 202);

  if (path === '/dns/domains' && method === 'GET') return envelope(state.domains);
  if (path === '/dns/domains' && method === 'POST') { const body = await jsonBody(request); const row = { ...body, id: nextId('domain'), createdAt: new Date().toISOString(), updatedAt: new Date().toISOString() }; state.domains.push(row as typeof state.domains[number]); return envelope(row, 201); }
  match = path.match(/^\/dns\/domains\/([^/]+)(?:\/records(?:\/([^/]+))?)?$/);
  if (match) { const domainId = match[1]; const recordId = match[2]; const isRecords = path.includes('/records'); if (isRecords && !recordId && method === 'GET') return envelope(state.records[domainId] ?? []); if (isRecords && !recordId && method === 'POST') { const row = { ...(await jsonBody(request)), id: nextId('rec') }; (state.records[domainId] ??= []).push(row); return envelope(row, 201); } if (isRecords && recordId && method === 'PUT') { const body = await jsonBody(request); const records = state.records[domainId] ?? []; const index = records.findIndex((item) => item.id === recordId); records[index] = { ...records[index], ...body, id: recordId }; state.records[domainId] = records; return envelope(records[index]); } if (isRecords && recordId && method === 'DELETE') { state.records[domainId] = (state.records[domainId] ?? []).filter((item) => item.id !== recordId); return new Response(null, { status: 204 }); } if (!isRecords && method === 'PUT') { const body = await jsonBody(request); const index = state.domains.findIndex((item) => item.id === domainId); state.domains[index] = { ...state.domains[index], ...body, id: domainId, updatedAt: new Date().toISOString() } as typeof state.domains[number]; return envelope(state.domains[index]); } if (!isRecords && method === 'DELETE') { state.domains = state.domains.filter((item) => item.id !== domainId); return new Response(null, { status: 204 }); } }

  if (path === '/certificates' && method === 'GET') return envelope(state.certificates);
  if (path === '/certificates' && method === 'POST') { const body = await jsonBody(request); const domain = state.domains.find((item) => item.id === body.domainId); const row = { ...body, id: nextId('cert'), domain: domain?.name ?? String(body.domainId ?? ''), domains: [body.prefix === '*' ? `*.${domain?.name ?? ''}` : `${body.prefix}.${domain?.name ?? ''}`], certificatePath: `/data/certificates/${body.variableName || 'mock'}.crt`, privateKeyPath: `/data/certificates/${body.variableName || 'mock'}.key`, issuer: "Let's Encrypt", status: 'pending', autoRenew: true, createdAt: new Date().toISOString(), updatedAt: new Date().toISOString() }; state.certificates.unshift(row as typeof state.certificates[number]); return envelope({ certificate: row, taskId: taskCreated('certificate.issue').id }, 201); }
  match = path.match(/^\/certificates\/([^/]+)(?:\/renew)?$/);
  if (match && method === 'POST' && path.endsWith('/renew')) { const cert = state.certificates.find((item) => item.id === match![1]); if (cert) { cert.status = 'issuing'; cert.updatedAt = new Date().toISOString(); } return envelope({ renewed: true }); }
  if (match && method === 'DELETE') { state.certificates = state.certificates.filter((item) => item.id !== match![1]); return new Response(null, { status: 204 }); }
  if (path === '/self-signed-certificates' && method === 'GET') return envelope(state.selfSigned);
  if (path === '/self-signed-cas' && method === 'POST') { const body = await jsonBody(request); const row = { id: nextId('ca'), kind: 'ca', name: body.name, commonName: body.commonName, dnsNames: [], ipAddresses: [], fingerprint: 'SHA256:MOCK:CA', notBefore: new Date().toISOString(), notAfter: '2031-01-01T00:00:00Z', createdAt: new Date().toISOString(), updatedAt: new Date().toISOString() }; state.selfSigned.push(row as typeof state.selfSigned[number]); return envelope(row, 201); }
  if (path === '/self-signed-certificates' && method === 'POST') { const body = await jsonBody(request); const row = { id: nextId('leaf'), parentCaId: body.caId, kind: 'leaf', name: body.name, commonName: body.commonName, dnsNames: body.dnsNames ?? [], ipAddresses: body.ipAddresses ?? [], fingerprint: 'SHA256:MOCK:LEAF', notBefore: new Date().toISOString(), notAfter: '2027-01-01T00:00:00Z', createdAt: new Date().toISOString(), updatedAt: new Date().toISOString() }; state.selfSigned.push(row as typeof state.selfSigned[number]); return envelope(row, 201); }
  match = path.match(/^\/self-signed-certificates\/([^/]+)(?:\/renew)?$/);
  if (match && method === 'POST' && path.endsWith('/renew')) { const cert = state.selfSigned.find((item) => item.id === match![1]); if (cert) cert.updatedAt = new Date().toISOString(); return envelope(cert); }
  if (match && method === 'DELETE') { state.selfSigned = state.selfSigned.filter((item) => item.id !== match![1]); return new Response(null, { status: 204 }); }
  if (path === '/key-assets' && method === 'GET') return envelope(state.keyAssets);
  if (path === '/key-assets/system' && method === 'GET') return envelope(state.systemCertificates);
  match = path.match(/^\/key-assets\/system\/([^/]+)\/reset$/);
  if (match && method === 'POST') return envelope({ taskId: taskCreated('key_assets.system.reset').id }, 202);
  if (['/key-assets/ca', '/key-assets/tls', '/key-assets/ssh/generate', '/key-assets/import'].includes(path) && method === 'POST') { const body = await jsonBody(request); const type = path.includes('/tls') ? 'tls_certificate' : path.includes('/ssh') ? 'ssh_key_pair' : 'ca_certificate'; const row = { id: nextId('key'), type, name: body.name ?? 'Mock asset', parentAssetId: body.caId ?? body.parentAssetId ?? null, algorithm: body.algorithm ?? 'ed25519', keySize: body.keySize ?? null, commonName: body.commonName ?? null, dnsNames: body.dnsNames ?? [], ipAddresses: body.ipAddresses ?? [], fingerprint: 'SHA256:MOCK:ASSET', notBefore: new Date().toISOString(), notAfter: type === 'ssh_key_pair' ? null : '2027-01-01T00:00:00Z', hasCertificate: type !== 'ssh_key_pair', hasPrivateKey: true, hasPublicKey: true, downloadKinds: type === 'ssh_key_pair' ? ['private_key', 'ssh_public_key'] : ['certificate', 'private_key', 'public_key'], childCount: 0, referenceCount: 0, references: [], canReissue: type === 'tls_certificate', canRegenerate: type !== 'tls_certificate', canDelete: true, createdAt: new Date().toISOString(), updatedAt: new Date().toISOString() }; state.keyAssets.unshift(row as typeof state.keyAssets[number]); return envelope({ asset: row, taskId: taskCreated(`key_assets.${type}.create`).id }, 201); }
  if (path === '/key-assets/exports' && method === 'POST') return envelope({ taskId: taskCreated('key_assets.export').id, operationId: 'op-key-export' }, 202);
  match = path.match(/^\/key-assets\/exports\/([^/]+)\/download$/);
  if (match && method === 'GET') return new Response('mock encrypted key asset archive', { headers: { 'Content-Type': 'application/octet-stream', 'Content-Disposition': `attachment; filename="${match[1]}-key-assets.zip"` } });
  if (path === '/key-assets/imports/preflight' && method === 'POST') return envelope({ planId: 'mock-import-plan', expiresAt: '2026-06-22T09:00:00Z', summary: { totalAssets: 3, caCount: 1, tlsCount: 1, sshCount: 1, standaloneTlsCount: 1, conflictCount: 2 }, assets: [{ assetId: 'archive-ca', type: 'ca_certificate', name: 'Archive CA', algorithm: 'ed25519', commonName: 'Archive CA', standalone: true, conflictTypes: ['name_conflict'] }, { assetId: 'archive-tls', type: 'tls_certificate', name: 'Archive TLS', parentAssetId: null, algorithm: 'rsa', keySize: 2048, commonName: 'archive.example.test', standalone: true, conflictTypes: ['overwrite_in_use'] }, { assetId: 'archive-ssh', type: 'ssh_key_pair', name: 'Archive SSH', algorithm: 'ed25519', standalone: true, conflictTypes: [] }], conflicts: [{ assetId: 'archive-ca', assetName: 'Archive CA', assetType: 'ca_certificate', conflictType: 'name_conflict', existingAssetId: 'key-ca', existingAssetName: '用户根 CA' }, { assetId: 'archive-tls', assetName: 'Archive TLS', assetType: 'tls_certificate', conflictType: 'overwrite_in_use', overwriteCandidates: [{ assetId: 'key-tls', name: '内部服务 TLS', type: 'tls_certificate' }], affectedReferences: [{ resourceType: 'application', resourceId: 'app-web', resourceName: 'website', relation: 'tls' }] }], requiresDangerConfirm: true });
  match = path.match(/^\/key-assets\/imports\/([^/]+)\/execute$/);
  if (match && method === 'POST') return envelope({ taskId: taskCreated('key_assets.import').id, operationId: 'op-key-import' }, 202);
  match = path.match(/^\/key-assets\/([^/]+)$/);
  if (match && method === 'GET') return envelope(state.keyAssets.find((item) => item.id === match![1]));
  if (match && method === 'DELETE') { state.keyAssets = state.keyAssets.filter((item) => item.id !== match![1]); return new Response(null, { status: 204 }); }
  match = path.match(/^\/key-assets\/([^/]+)\/(reissue|regenerate)$/);
  if (match && method === 'POST') return envelope({ asset: state.keyAssets.find((item) => item.id === match![1]), taskId: taskCreated(`key_assets.${match[2]}`).id }, 202);
  match = path.match(/^\/key-assets\/([^/]+)\/files\/([^/]+)$/);
  if (match && method === 'GET') return new Response('mock key asset content', { headers: { 'Content-Type': 'application/octet-stream', 'Content-Disposition': `attachment; filename="${match[1]}-${match[2]}.pem"` } });

  if (path === '/tasks' && method === 'GET') { const page = Number(url.searchParams.get('page') ?? 1); const pageSize = Number(url.searchParams.get('pageSize') ?? 20); const statuses = url.searchParams.getAll('status'); const serverId = url.searchParams.get('serverId'); const types = url.searchParams.getAll('type'); const operationPage = url.searchParams.get('operationPage') === 'true'; const filtered = state.tasks.filter((task) => (!statuses.length || statuses.includes(task.status)) && (!serverId || task.serverId === serverId) && (!types.length || types.includes(task.type))); if (operationPage) { const keys = Array.from(new Set(filtered.map((task) => task.operationId || task.id))); const pageKeys = new Set(keys.slice((page - 1) * pageSize, page * pageSize)); return envelope({ items: filtered.filter((task) => pageKeys.has(task.operationId || task.id)), total: keys.length, page, pageSize }); } const items = filtered.slice((page - 1) * pageSize, page * pageSize); return envelope({ items: items.length || !filtered.length ? items : filtered.slice(0, pageSize), total: Math.max(filtered.length, pageSize * 50), page, pageSize }); }
  match = path.match(/^\/tasks\/([^/]+)(?:\/(logs|steps|retry|run-now))?$/);
  if (match) { const task = state.tasks.find((item) => item.id === match![1]); const suffix = match[2]; if (!suffix && method === 'GET') return envelope(task); if (suffix === 'logs' && method === 'GET') return envelope({ nextCursor: 3, logs: [{ cursor: 1, time: mockEarlier, stream: 'system', line: 'Mock task accepted' }, { cursor: 2, time: mockEarlier, stream: 'stdout', line: 'Executing simulated operation' }, { cursor: 3, time: new Date().toISOString(), stream: task?.status === 'failed_retryable' ? 'stderr' : 'stdout', line: task?.error ?? 'Operation is healthy' }] }); if (suffix === 'steps' && method === 'GET') return envelope([{ id: `${match[1]}-1`, taskId: match[1], step: 'prepare', status: 'completed', percentage: 100, startedAt: mockEarlier, finishedAt: mockEarlier }, { id: `${match[1]}-2`, taskId: match[1], step: 'execute', status: task?.status === 'failed_retryable' ? 'failed' : task?.status ?? 'running', percentage: task?.percentage, startedAt: mockEarlier, error: task?.error ?? null }]); if ((suffix === 'retry' || suffix === 'run-now') && method === 'POST' && task) { task.status = 'queued'; task.stage = 'preparing'; task.percentage = 0; task.error = undefined; return envelope(task); } }

  if (path === '/debug/snapshot' && method === 'GET') return envelope({ collectedAt: new Date().toISOString(), process: { startedAt: mockEarlier, uptimeSeconds: 7200, pid: 4242, goVersion: 'go1.24.4', os: 'windows', architecture: 'amd64', cpuCount: 8, goroutineCount: 37, cgoCallCount: 0 }, memory: { allocBytes: 48_000_000, totalAllocBytes: 220_000_000, sysBytes: 96_000_000, heapAllocBytes: 44_000_000, heapInUseBytes: 52_000_000, heapIdleBytes: 12_000_000, heapReleasedBytes: 4_000_000, heapObjects: 123456, stackInUseBytes: 2_000_000, stackSysBytes: 2_000_000, mspanInUseBytes: 120000, mcacheInUseBytes: 4800, nextGcBytes: 88_000_000, gcCycles: 42, forcedGcCycles: 0, gcPauseTotalNs: 12_000_000, lastGcAt: new Date().toISOString() }, tasks: { workerRunning: false, registeredTypes: 22, executableTypes: 17, periodicTypes: 5, runningExecutions: 2, definitions: [{ type: 'system.cleanup', hidden: false, executable: true, periodic: true, allowRunNow: true, allowRetry: true, defaultMaxRetries: 1, concurrencyPolicy: 'forbid', staleQueuedAfterSeconds: 300, periodicIntervalSeconds: 86400 }, { type: 'application.deploy', hidden: false, executable: true, periodic: false, allowRunNow: false, allowRetry: true, defaultMaxRetries: 2, concurrencyPolicy: 'replace', staleQueuedAfterSeconds: 600, periodicIntervalSeconds: 0 }, { type: 'internal.metrics.compact', hidden: true, executable: false, periodic: true, allowRunNow: false, allowRetry: false, defaultMaxRetries: 0, concurrencyPolicy: 'skip', staleQueuedAfterSeconds: 0, periodicIntervalSeconds: 3600 }] }, databases: [{ name: 'app', healthy: true, fileSizeBytes: 8_000_000, pageSizeBytes: 4096, pageCount: 2000, freePageCount: 50, usedBytes: 7_800_000, freeBytes: 200_000, connections: { maxOpenConnections: 10, openConnections: 2, inUse: 1, idle: 1, waitCount: 0, waitDurationNs: 0, maxIdleClosed: 0, maxIdleTimeClosed: 0, maxLifetimeClosed: 0 }, tables: [{ name: 'servers', rowCount: state.servers.length, dataSizeBytes: 500_000, indexSizeBytes: 120_000, totalSizeBytes: 620_000, databasePercent: 7.75 }, { name: 'key_assets', rowCount: state.keyAssets.length, dataSizeBytes: 360_000, indexSizeBytes: 90_000, totalSizeBytes: 450_000, databasePercent: 5.62 }] }, { name: 'task', healthy: true, fileSizeBytes: 16_000_000, pageSizeBytes: 4096, pageCount: 4000, freePageCount: 80, usedBytes: 15_600_000, freeBytes: 400_000, connections: { maxOpenConnections: 10, openConnections: 3, inUse: 2, idle: 1, waitCount: 4, waitDurationNs: 120_000_000, maxIdleClosed: 0, maxIdleTimeClosed: 1, maxLifetimeClosed: 0 }, tables: [{ name: 'tasks', rowCount: state.tasks.length, dataSizeBytes: 900_000, indexSizeBytes: 180_000, totalSizeBytes: 1_080_000, databasePercent: 6.75, errorCode: 'mock_table_size_warning' }] }, { name: 'metrics', healthy: false, errorCode: 'mock_metrics_db_locked', tableSizeErrorCode: 'mock_table_stats_unavailable', fileSizeBytes: 24_000_000, pageSizeBytes: 4096, pageCount: 6000, freePageCount: 120, usedBytes: 23_500_000, freeBytes: 500_000, connections: { maxOpenConnections: 10, openConnections: 10, inUse: 10, idle: 0, waitCount: 42, waitDurationNs: 1_800_000_000, maxIdleClosed: 2, maxIdleTimeClosed: 3, maxLifetimeClosed: 1 }, tables: [] }] });

  return failure(method, path);
};
