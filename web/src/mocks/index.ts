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
  return { card, metricsByServer: { 'srv-edge': series(0), 'srv-db': series(12) } };
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
  match = path.match(/^\/servers\/([^/]+)\/packages\/updates$/);
  if (match && method === 'GET') return envelope({ serverId: match[1], lastRefreshedAt: new Date().toISOString(), updates: state.packageUpdates, refreshing: false });
  match = path.match(/^\/servers\/([^/]+)\/packages\/(refresh|upgrade-selected|upgrade-all)$/);
  if (match && method === 'POST') { const task = taskCreated(`packages.${match[2]}`, match[1]); return envelope(match[2] === 'refresh' ? { serverId: match[1], refreshing: true, taskId: task.id } : { taskId: task.id }, 202); }

  if (path === '/overview' && method === 'GET') return envelope({ servers: state.servers.map((server) => ({ id: server.id, name: server.name, host: server.host, supported: server.os?.supported ?? false, reachable: server.reachable, metricsFresh: server.reachable, packageUpdateCount: server.reachable ? state.packageUpdates.length : 0, loadAverage: server.loadAverage, lastMetricsAt: server.lastCheckedAt, lastPackageRefreshAt: server.lastCheckedAt })) });
  if (path === '/overview/cards' && method === 'GET') return envelope({ cards: [{ id: 'cpu-main', kind: 'cpu', width: 6, height: 4, range: '1h', networkDirection: 'both', serverIds: ['srv-edge', 'srv-db'] }, { id: 'memory-main', kind: 'memory', width: 6, height: 4, range: '1h', networkDirection: 'both', serverIds: ['srv-edge', 'srv-db'] }, { id: 'packages-main', kind: 'packageUpdates', width: 4, height: 3, range: '1h', networkDirection: 'both', serverIds: ['srv-edge', 'srv-db'] }] });
  if (path === '/overview/cards' && method === 'PUT') return envelope(await jsonBody(request));
  match = path.match(/^\/overview\/cards\/([^/]+)\/data$/);
  if (match && method === 'GET') { const kinds: Record<string, string> = { 'cpu-main': 'cpu', 'memory-main': 'memory', 'packages-main': 'packageUpdates' }; return envelope(metrics({ id: match[1], kind: kinds[match[1]] ?? 'cpu', width: 6, height: 4, range: '1h', networkDirection: 'both', serverIds: ['srv-edge', 'srv-db'] })); }

  if (path === '/applications' && method === 'GET') return envelope(state.applications);
  if (path === '/application-template-catalog' && method === 'GET') return envelope({ variables: [{ key: 'server.name', category: 'server', specExpression: '${server.name}', templateExpression: '{{ server.name }}' }, { key: 'certificate.WEB_TLS.certificate', category: 'certificate', specExpression: '${certificate.WEB_TLS.certificate}', templateExpression: '{{ certificate.WEB_TLS.certificate }}' }], panelFiles: [{ id: 'panel-nginx', resourceId: 'app-web', resourceType: 'application', name: 'nginx.conf', kind: 'template', source: 'panel' }] });
  if (path === '/application-save-sessions' && method === 'POST') return envelope({ id: nextId('save'), expiresAt: '2026-06-22T09:00:00Z', files: [] }, 201);
  match = path.match(/^\/application-save-sessions\/([^/]+)\/(files|files\/delete|commit)$/);
  if (match && method === 'POST') { if (match[2] === 'commit') return envelope(state.applications[0]); const body = await jsonBody(request); return match[2] === 'files/delete' ? new Response(null, { status: 204 }) : envelope({ id: nextId('file'), applicationId: '', path: body.path, kind: body.kind, contentType: body.contentType || 'text/plain', size: 128, sha256: 'sha256:mock', createdAt: new Date().toISOString(), updatedAt: new Date().toISOString() }); }
  match = path.match(/^\/applications\/([^/]+)(?:\/(.*))?$/);
  if (match) {
    const app = state.applications.find((item) => item.id === match![1]); const suffix = match[2] ?? '';
    if (!suffix && method === 'GET') return envelope(app);
    if (!suffix && method === 'DELETE') { state.applications = state.applications.filter((item) => item.id !== match![1]); return new Response(null, { status: 204 }); }
    if (suffix === 'files' && method === 'GET') return envelope([{ id: 'file-nginx', applicationId: match[1], path: 'nginx.conf', kind: 'template', contentType: 'text/plain', size: 256, sha256: 'sha256:file', createdAt: new Date().toISOString(), updatedAt: new Date().toISOString() }]);
    if (suffix === 'runtime' && method === 'GET') return envelope({ applicationId: match[1], runtimeId: `runtime-${match[1]}`, status: app?.runtimeStatus ?? 'unknown', instances: app?.enabled ? [{ instanceId: `${match[1]}-srv-edge`, serverId: 'srv-edge', serverName: '新加坡边缘节点', containerName: app.name, containerId: 'ctr-web', status: 'running', desiredState: 'running', image: app.imageReference, startedAt: mockEarlier, observedAt: new Date().toISOString() }] : [], observedAt: new Date().toISOString() });
    if (suffix.startsWith('logs') && method === 'GET') return envelope({ instanceId: url.searchParams.get('instanceId') ?? '', containerName: app?.name ?? '', type: url.searchParams.get('type') ?? 'stdout', logs: '[mock] service started\n[mock] health check passed\n[mock] request GET / 200\n' });
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
  if (match) { const domainId = match[1]; const recordId = match[2]; const isRecords = path.includes('/records'); if (isRecords && !recordId && method === 'GET') return envelope(state.records[domainId] ?? []); if (isRecords && !recordId && method === 'POST') { const row = { ...(await jsonBody(request)), id: nextId('rec') }; (state.records[domainId] ??= []).push(row); return envelope(row, 201); } if (isRecords && recordId && method === 'DELETE') { state.records[domainId] = (state.records[domainId] ?? []).filter((item) => item.id !== recordId); return new Response(null, { status: 204 }); } if (!isRecords && method === 'DELETE') { state.domains = state.domains.filter((item) => item.id !== domainId); return new Response(null, { status: 204 }); } }

  if (path === '/certificates' && method === 'GET') return envelope(state.certificates);
  if (path === '/self-signed-certificates' && method === 'GET') return envelope(state.selfSigned);
  if (path === '/key-assets' && method === 'GET') return envelope(state.keyAssets);
  if (path === '/key-assets/system' && method === 'GET') return envelope(state.systemCertificates);
  match = path.match(/^\/key-assets\/([^/]+)$/);
  if (match && method === 'GET') return envelope(state.keyAssets.find((item) => item.id === match![1]));
  match = path.match(/^\/key-assets\/([^/]+)\/files\/([^/]+)$/);
  if (match && method === 'GET') return new Response('mock key asset content', { headers: { 'Content-Type': 'application/octet-stream', 'Content-Disposition': `attachment; filename="${match[1]}-${match[2]}.pem"` } });

  if (path === '/tasks' && method === 'GET') { const page = Number(url.searchParams.get('page') ?? 1); const pageSize = Number(url.searchParams.get('pageSize') ?? 20); const statuses = url.searchParams.getAll('status'); const serverId = url.searchParams.get('serverId'); const types = url.searchParams.getAll('type'); const filtered = state.tasks.filter((task) => (!statuses.length || statuses.includes(task.status)) && (!serverId || task.serverId === serverId) && (!types.length || types.includes(task.type))); return envelope({ items: filtered.slice((page - 1) * pageSize, page * pageSize), total: filtered.length, page, pageSize }); }
  match = path.match(/^\/tasks\/([^/]+)(?:\/(logs|steps|retry|run-now))?$/);
  if (match) { const task = state.tasks.find((item) => item.id === match![1]); const suffix = match[2]; if (!suffix && method === 'GET') return envelope(task); if (suffix === 'logs' && method === 'GET') return envelope({ nextCursor: 3, logs: [{ cursor: 1, time: mockEarlier, stream: 'system', line: 'Mock task accepted' }, { cursor: 2, time: mockEarlier, stream: 'stdout', line: 'Executing simulated operation' }, { cursor: 3, time: new Date().toISOString(), stream: task?.status === 'failed_retryable' ? 'stderr' : 'stdout', line: task?.error ?? 'Operation is healthy' }] }); if (suffix === 'steps' && method === 'GET') return envelope([{ id: `${match[1]}-1`, taskId: match[1], step: 'prepare', status: 'completed', percentage: 100, startedAt: mockEarlier, finishedAt: mockEarlier }, { id: `${match[1]}-2`, taskId: match[1], step: 'execute', status: task?.status === 'failed_retryable' ? 'failed' : task?.status ?? 'running', percentage: task?.percentage, startedAt: mockEarlier, error: task?.error ?? null }]); if ((suffix === 'retry' || suffix === 'run-now') && method === 'POST' && task) { task.status = 'queued'; task.stage = 'preparing'; task.percentage = 0; task.error = undefined; return envelope(task); } }

  if (path === '/debug/snapshot' && method === 'GET') return envelope({ collectedAt: new Date().toISOString(), process: { startedAt: mockEarlier, uptimeSeconds: 7200, pid: 4242, goVersion: 'go1.24.4', os: 'windows', architecture: 'amd64', cpuCount: 8, goroutineCount: 37, cgoCallCount: 0 }, memory: { allocBytes: 48_000_000, totalAllocBytes: 220_000_000, sysBytes: 96_000_000, heapAllocBytes: 44_000_000, heapInUseBytes: 52_000_000, heapIdleBytes: 12_000_000, heapReleasedBytes: 4_000_000, heapObjects: 123456, stackInUseBytes: 2_000_000, stackSysBytes: 2_000_000, mspanInUseBytes: 120000, mcacheInUseBytes: 4800, nextGcBytes: 88_000_000, gcCycles: 42, forcedGcCycles: 0, gcPauseTotalNs: 12_000_000, lastGcAt: new Date().toISOString() }, tasks: { workerRunning: true, registeredTypes: 18, executableTypes: 15, periodicTypes: 4, runningExecutions: 1, definitions: [{ type: 'system.cleanup', hidden: false, executable: true, periodic: true, allowRunNow: true, allowRetry: true, defaultMaxRetries: 1, concurrencyPolicy: 'forbid', staleQueuedAfterSeconds: 300, periodicIntervalSeconds: 86400 }] }, databases: ['app', 'task', 'metrics'].map((name, index) => ({ name, healthy: true, fileSizeBytes: (index + 1) * 8_000_000, pageSizeBytes: 4096, pageCount: 2000, freePageCount: 50, usedBytes: 7_800_000, freeBytes: 200_000, connections: { maxOpenConnections: 10, openConnections: 2, inUse: 1, idle: 1, waitCount: 0, waitDurationNs: 0, maxIdleClosed: 0, maxIdleTimeClosed: 0, maxLifetimeClosed: 0 }, tables: [{ name: `${name}_records`, rowCount: 120 + index * 80, dataSizeBytes: 500_000, indexSizeBytes: 120_000, totalSizeBytes: 620_000, databasePercent: 7.75 }] })) });

  return failure(method, path);
};
