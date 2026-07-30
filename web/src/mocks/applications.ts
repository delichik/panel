import type { ApiEnvelope } from '@/types/api';
import type { ApplicationDto, ApplicationEditSession, ApplicationRuntime, ApplicationSummaryDto, Diagnostic, LogResult, OperationResult } from '@/types/applications';
import type { FacilityAppSummary, FacilityEditSession, ReverseProxyConfig } from '@/types/facilityApps';

const now = '2026-07-21T08:00:00.000Z';

export const mockApplications: ApplicationDto[] = [
  {
    id: 'app-storefront',
    version: 4,
    kind: 'application',
    name: 'storefront',
    enabled: true,
    specYaml: 'name: storefront\nimage: ghcr.io/example/storefront:1.9.0\nports:\n  - label: http\n    to: 8080\nmounts:\n  - type: persistent\n    target: /data\n',
    variables: { NODE_ENV: 'production', FEATURE_FLAG: 'checkout-v2' },
    persistentPath: '/opt/panel/apps/app-storefront/persistent',
    deploymentMode: 'selected',
    deploymentServers: ['srv-edge-sgp', 'srv-api-hkg'],
    reverseProxy: [{ domain: 'shop.example.test', targetType: 'local', targetPort: 8080, originServerIds: ['srv-edge-sgp', 'srv-api-hkg'], anyAccess: { enabled: true, strategy: 'round_robin' }, paths: [{ path: '/', webSocket: false }] }],
    generation: 7,
    specHash: 'hash-storefront',
    imageReference: 'ghcr.io/example/storefront:1.9.0',
    imageDigest: 'sha256:current',
    imageLatestDigest: 'sha256:latest',
    imageCheckedAt: now,
    imageUpdateAvailable: true,
    imageUpdateTargets: [{ serverId: 'srv-edge-sgp', serverName: 'edge-sgp-01', reference: 'ghcr.io/example/storefront:1.9.0', updateAvailable: true, checkedAt: now }],
    jobId: 'panel-storefront',
    namespace: 'default',
    runtimeStatus: 'partially_deployed',
    lastDeploymentId: 'deploy-91',
    createdAt: now,
    updatedAt: now,
  },
  {
    id: 'app-worker',
    version: 2,
    kind: 'application',
    name: 'long-running-worker-with-a-very-long-display-name-for-layout',
    enabled: true,
    specYaml: 'name: worker\nimage: ghcr.io/example/worker:2.0.0\nrestart:\n  policy: unless-stopped\n',
    variables: { QUEUE: 'critical' },
    deploymentMode: 'all',
    deploymentServers: [],
    reverseProxy: [],
    generation: 3,
    specHash: 'hash-worker',
    imageReference: 'ghcr.io/example/worker:2.0.0',
    imageUpdateAvailable: false,
    jobId: 'panel-worker',
    namespace: 'default',
    runtimeStatus: 'deploying',
    createdAt: now,
    updatedAt: now,
  },
  {
    id: 'app-api',
    version: 6,
    kind: 'application',
    name: 'public-api',
    enabled: true,
    specYaml: 'name: public-api\nimage: ghcr.io/example/public-api:4.6.1\nports:\n  - label: http\n    to: 9000\nchecks:\n  - name: readiness\n    type: http\n    path: /ready\n',
    variables: { NODE_ENV: 'production', REGION: 'apac' },
    deploymentMode: 'selected',
    deploymentServers: ['srv-api-hkg', 'srv-api-hkg-02'],
    reverseProxy: [{ domain: 'api.example.test', targetType: 'local', targetPort: 9000, originServerIds: ['srv-api-hkg', 'srv-api-hkg-02'], anyAccess: { enabled: true, strategy: 'least_conn' }, paths: [{ path: '/v1', webSocket: false }, { path: '/events', webSocket: true }] }],
    generation: 12,
    specHash: 'hash-public-api',
    imageReference: 'ghcr.io/example/public-api:4.6.1',
    imageDigest: 'sha256:api-current',
    imageLatestDigest: 'sha256:api-current',
    imageCheckedAt: now,
    imageUpdateAvailable: false,
    jobId: 'panel-public-api',
    namespace: 'default',
    runtimeStatus: 'deployed',
    lastDeploymentId: 'deploy-118',
    createdAt: now,
    updatedAt: now,
  },
  {
    id: 'app-analytics',
    version: 3,
    kind: 'application',
    name: 'analytics-pipeline',
    enabled: true,
    specYaml: 'name: analytics-pipeline\nimage: ghcr.io/example/analytics:3.2.0\nrestart:\n  policy: unless-stopped\nenv:\n  - CLICKHOUSE_URL\n',
    variables: { CLICKHOUSE_URL: 'http://analytics-db:8123', BATCH_SIZE: '5000' },
    deploymentMode: 'selected',
    deploymentServers: ['srv-worker-nrt', 'srv-batch-iad'],
    reverseProxy: [],
    generation: 5,
    specHash: 'hash-analytics',
    imageReference: 'ghcr.io/example/analytics:3.2.0',
    imageUpdateAvailable: true,
    imageLastError: 'Unable to reach registry mirror on worker-nrt-queue-a.',
    jobId: 'panel-analytics',
    namespace: 'default',
    runtimeStatus: 'failed',
    lastError: 'Worker exited after retry budget was exhausted.',
    createdAt: now,
    updatedAt: now,
  },
  {
    id: 'app-internal-docs',
    version: 2,
    kind: 'application',
    name: 'internal-docs',
    enabled: true,
    specYaml: 'name: internal-docs\nimage: ghcr.io/example/docs:2026.07\nports:\n  - label: http\n    to: 8080\n',
    variables: { SITE_MODE: 'internal' },
    persistentPath: '/opt/panel/apps/app-internal-docs/persistent',
    deploymentMode: 'selected',
    deploymentServers: ['srv-observability-ams'],
    reverseProxy: [{ domain: 'docs.example.test', targetType: 'local', targetPort: 8080, originServerIds: ['srv-observability-ams'], anyAccess: { enabled: false }, paths: [{ path: '/', webSocket: false }] }],
    generation: 4,
    specHash: 'hash-docs',
    imageReference: 'ghcr.io/example/docs:2026.07',
    imageUpdateAvailable: false,
    jobId: 'panel-docs',
    namespace: 'default',
    runtimeStatus: 'deployed',
    createdAt: now,
    updatedAt: now,
  },
  {
    id: 'app-disabled',
    version: 1,
    kind: 'application',
    name: 'disabled-preview',
    enabled: false,
    specYaml: 'name: disabled-preview\nimage: nginx:1.28-alpine\n',
    variables: {},
    deploymentMode: 'all',
    deploymentServers: [],
    reverseProxy: [],
    generation: 1,
    specHash: 'hash-disabled',
    imageUpdateAvailable: false,
    jobId: 'panel-disabled',
    namespace: 'default',
    runtimeStatus: 'stopped',
    createdAt: now,
    updatedAt: now,
  },
];

const runtimes: Record<string, ApplicationRuntime> = {
  'app-storefront': {
    applicationId: 'app-storefront',
    runtimeId: 'runtime-storefront',
    status: 'partially_deployed',
    observedAt: now,
    instances: [
      { instanceId: 'inst-edge', serverId: 'srv-edge-sgp', serverName: 'edge-sgp-01', containerName: 'panel-storefront', status: 'running', observedGeneration: 7 },
      { instanceId: 'inst-api', serverId: 'srv-api-hkg', serverName: 'api-hkg-01', containerName: 'panel-storefront', status: 'failed', error: 'health check timed out' },
    ],
    operation: { id: 'op-storefront', applicationId: 'app-storefront', type: 'deploy', status: 'deploying', taskId: 'task-application-77', generation: 7, trigger: 'user', createdAt: now, updatedAt: now },
  },
  'app-worker': {
    applicationId: 'app-worker',
    runtimeId: 'runtime-worker',
    status: 'deploying',
    observedAt: now,
    instances: [{ instanceId: 'inst-worker', serverId: 'srv-worker-nrt', serverName: 'worker-nrt-queue-a', containerName: 'panel-worker', status: 'running', observedGeneration: 3 }],
  },
  'app-api': {
    applicationId: 'app-api',
    runtimeId: 'runtime-api',
    status: 'deployed',
    observedAt: now,
    instances: [
      { instanceId: 'inst-api-hkg-1', serverId: 'srv-api-hkg', serverName: 'api-hkg-01', containerName: 'panel-public-api', status: 'running', observedGeneration: 12 },
      { instanceId: 'inst-api-hkg-2', serverId: 'srv-api-hkg-02', serverName: 'api-hkg-02-canary', containerName: 'panel-public-api', status: 'running', observedGeneration: 12 },
    ],
  },
  'app-analytics': {
    applicationId: 'app-analytics',
    runtimeId: 'runtime-analytics',
    status: 'failed',
    observedAt: now,
    instances: [
      { instanceId: 'inst-analytics-a', serverId: 'srv-worker-nrt', serverName: 'worker-nrt-queue-a', containerName: 'panel-analytics', status: 'failed', error: 'exit code 137' },
      { instanceId: 'inst-analytics-b', serverId: 'srv-batch-iad', serverName: 'batch-iad-nightly', containerName: 'panel-analytics', status: 'running', observedGeneration: 5 },
    ],
    operation: { id: 'op-analytics', applicationId: 'app-analytics', type: 'deploy', status: 'failed', taskId: 'task-analytics-17', generation: 5, trigger: 'scheduler', error: 'One target failed.', createdAt: now, updatedAt: now },
  },
  'app-internal-docs': {
    applicationId: 'app-internal-docs',
    runtimeId: 'runtime-docs',
    status: 'deployed',
    observedAt: now,
    instances: [{ instanceId: 'inst-docs', serverId: 'srv-observability-ams', serverName: 'observability-ams', containerName: 'panel-docs', status: 'running', observedGeneration: 4 }],
  },
  'app-disabled': { applicationId: 'app-disabled', runtimeId: 'runtime-disabled', status: 'stopped', observedAt: now, instances: [] },
};

export let mockFacility: ReverseProxyConfig = {
  id: 'reverse_proxy',
  version: 5,
  deploymentServers: ['srv-edge-sgp', 'srv-api-hkg', 'srv-api-hkg-02'],
  panelHostServerId: 'srv-edge-sgp',
  panelEntry: { enabled: true, serverId: 'srv-edge-sgp', domain: 'panel.example.test' },
  domains: [
    {
      domain: 'static.example.test',
      originServerIds: ['srv-edge-sgp', 'srv-api-hkg'],
      anyAccess: { enabled: true, strategy: 'round_robin' },
      paths: [{ path: '/', ruleType: 'static', sourceType: 'uploaded_bundle', assetId: 'asset-docs' }],
    },
    {
      domain: 'downloads.example.test',
      originServerIds: ['srv-api-hkg-02'],
      anyAccess: { enabled: false },
      paths: [{ path: '/releases', ruleType: 'static', sourceType: 'uploaded_file', assetId: 'asset-release-index' }],
    },
  ],
  staticAssets: [
    { id: 'asset-docs', name: 'docs bundle', kind: 'uploaded_bundle', contentMode: 'binary', filename: 'docs.zip', size: 7340032, sha256: 'sha-docs', createdAt: now, updatedAt: now },
    { id: 'asset-release-index', name: 'release index', kind: 'uploaded_file', contentMode: 'binary', filename: 'index.html', size: 48216, sha256: 'sha-release-index', createdAt: now, updatedAt: now },
  ],
  routeSummaries: [
    { domain: 'panel.example.test', path: '/', source: 'system_panel', serverIds: ['srv-edge-sgp'], httpsStatus: 'domain_certificate', certificateName: 'panel.example.test' },
    { domain: 'shop.example.test', path: '/', source: 'application', serverIds: ['srv-edge-sgp', 'srv-api-hkg'], httpsStatus: 'domain_certificate', applicationId: 'app-storefront', applicationName: 'storefront' },
    { domain: 'api.example.test', path: '/v1', source: 'application', serverIds: ['srv-api-hkg', 'srv-api-hkg-02'], httpsStatus: 'domain_certificate', applicationId: 'app-api', applicationName: 'public-api' },
    { domain: 'static.example.test', path: '/', source: 'static_asset', serverIds: ['srv-edge-sgp', 'srv-api-hkg'], httpsStatus: 'self_signed_certificate' },
    { domain: 'downloads.example.test', path: '/releases', source: 'static_asset', serverIds: ['srv-api-hkg-02'], httpsStatus: 'missing' },
  ],
  applicationRoutes: [
    { applicationId: 'app-storefront', applicationName: 'storefront', deploymentMode: 'selected', deploymentServers: ['srv-edge-sgp', 'srv-api-hkg'], routes: [{ domain: 'shop.example.test', targetPort: 8080, originServerIds: ['srv-edge-sgp', 'srv-api-hkg'], paths: [{ path: '/' }] }] },
    { applicationId: 'app-api', applicationName: 'public-api', deploymentMode: 'selected', deploymentServers: ['srv-api-hkg', 'srv-api-hkg-02'], routes: [{ domain: 'api.example.test', targetPort: 9000, originServerIds: ['srv-api-hkg', 'srv-api-hkg-02'], paths: [{ path: '/v1' }, { path: '/events' }] }] },
    { applicationId: 'app-internal-docs', applicationName: 'internal-docs', deploymentMode: 'selected', deploymentServers: ['srv-observability-ams'], routes: [{ domain: 'docs.example.test', targetPort: 8080, originServerIds: ['srv-observability-ams'], paths: [{ path: '/' }] }] },
  ],
  operation: { id: 'op-facility', applicationId: 'facility-reverse-proxy', type: 'deploy', status: 'deployed', generation: 5, createdAt: now, updatedAt: now },
  updatedAt: now,
  routes: 7,
  enabledServers: ['srv-edge-sgp', 'srv-api-hkg', 'srv-api-hkg-02'],
};

export function mockApplicationSummaries(): ApplicationSummaryDto[] {
  return mockApplications.map((app) => ({
    id: app.id,
    name: app.name,
    enabled: app.enabled,
    imageReference: app.imageReference,
    jobId: app.jobId,
    namespace: app.namespace,
    runtimeStatus: app.runtimeStatus,
    imageUpdateAvailable: app.imageUpdateAvailable,
    lastError: app.lastError,
    updatedAt: app.updatedAt,
  }));
}

export function mockFacilitySummaries(): FacilityAppSummary[] {
  return [{
    kind: 'reverse-proxy',
    titleKey: 'applicationsPage.entranceProxyFacility',
    descriptionKey: 'applicationsPage.entranceProxyFacilityDescription',
    categoryKey: 'applicationsPage.facilityCategoryTraffic',
    status: mockFacility.lastError ? 'degraded' : 'available',
    updatedAt: mockFacility.updatedAt,
    operationStatus: mockFacility.operation?.status,
    lastError: mockFacility.lastError,
  }];
}

const appSessions = new Map<string, ApplicationEditSession>();
const facilitySessions = new Map<string, FacilityEditSession>();

export function appRuntime(id: string) {
  return runtimes[id];
}

export function appLogs(id: string): LogResult {
  if (id === 'app-worker') throw new Error('Runtime log stream is temporarily unavailable.');
  return { instanceId: 'inst-edge', containerName: 'panel-storefront', type: 'stdout', logs: ['[info] listening on :8080', '[info] route / resolved', '[warn] upstream retry from core-1'].join('\n') };
}

export function appOperation(id: string, operation: string): OperationResult | null {
  const app = mockApplications.find((item) => item.id === id);
  if (!app) return null;
  if (operation === 'stop') app.enabled = false;
  if (operation === 'image/update') app.imageUpdateAvailable = false;
  app.runtimeStatus = operation === 'deploy' ? 'deploying' : app.runtimeStatus;
  return { taskId: `task-${operation.replace('/', '-')}-${Date.now()}`, application: app, runtime: runtimes[id] };
}

export function deleteApp(id: string) {
  const index = mockApplications.findIndex((item) => item.id === id);
  if (index < 0) return false;
  mockApplications.splice(index, 1);
  return true;
}

export function beginAppSession(applicationId?: string): ApplicationEditSession {
  const app = mockApplications.find((item) => item.id === applicationId);
  const session: ApplicationEditSession = {
    id: `aedit-${Date.now()}`,
    applicationId,
    clientDraftKey: applicationId ? `application:${applicationId}` : 'application:create',
    state: 'active',
    baseResourceVersion: { value: String(app?.version ?? 0), updatedAt: now },
    draft: {
      name: app?.name ?? '',
      enabled: app?.enabled ?? true,
      specYaml: app?.specYaml ?? 'name: new-app\nimage: nginx:1.28-alpine\nports:\n  - label: http\n    to: 80\n',
      variables: app?.variables ?? {},
      deploymentMode: app?.deploymentMode === 'selected' ? 'selected' : 'all',
      deploymentServers: app?.deploymentServers ?? [],
      reverseProxy: app?.reverseProxy ?? [],
    },
    revision: 1,
    files: [{ fileKey: 'file-env', path: 'config/env.template', kind: 'template', contentType: 'text/plain', size: 12, sha256: 'sha-env', contentBase64: 'SE9TVD17eyBob3N0IH19Cg==', createdAt: now, updatedAt: now }],
    idleExpiresAt: '2026-07-22T08:00:00.000Z',
    absoluteExpiresAt: '2026-07-28T08:00:00.000Z',
    createdAt: now,
    updatedAt: now,
  };
  appSessions.set(session.id, session);
  return session;
}

export function appSession(id: string) {
  return appSessions.get(id);
}

export function patchAppSession(id: string, draft: ApplicationEditSession['draft']) {
  const session = appSessions.get(id);
  if (!session) return null;
  session.draft = draft;
  session.revision += 1;
  session.updatedAt = now;
  return session;
}

export function getAppFile(id: string, fileKey: string) {
  const file = appSessions.get(id)?.files.find((item) => item.fileKey === fileKey);
  return file ? { ...file, contentBase64: file.contentBase64 ?? '' } : null;
}

export function putAppFile(id: string, fileKey: string, input: { path: string; kind: string; contentType: string; contentBase64: string }) {
  const session = appSessions.get(id);
  if (!session) return null;
  session.files = session.files.filter((file) => file.fileKey !== fileKey);
  session.files.push({ fileKey, path: input.path, kind: input.kind, contentType: input.contentType, size: input.contentBase64.length, sha256: `sha-${fileKey}`, contentBase64: input.contentBase64, createdAt: now, updatedAt: now });
  session.revision += 1;
  return session;
}

export function uploadAppArchive(id: string, input: { fileKey: string; basePath: string; filename: string; size: number; contentType: string }) {
  const session = appSessions.get(id);
  if (!session) return null;
  const normalizedPath = input.basePath.trim() && input.basePath !== '/' ? input.basePath : input.filename;
  session.files = session.files.filter((file) => file.fileKey !== input.fileKey);
  session.files.push({
    fileKey: input.fileKey,
    path: normalizedPath,
    kind: 'archive',
    contentType: input.contentType,
    size: input.size,
    sha256: `sha-${input.fileKey}`,
    createdAt: now,
    updatedAt: now,
  });
  session.revision += 1;
  session.updatedAt = now;
  return session;
}

export function deleteAppFile(id: string, fileKey: string) {
  const session = appSessions.get(id);
  if (!session) return null;
  session.files = session.files.filter((file) => file.fileKey !== fileKey);
  session.revision += 1;
  return session;
}

export function appDiagnostics(session: ApplicationEditSession): Diagnostic[] {
  const issues: Diagnostic[] = [];
  if (!session.draft.name.trim()) issues.push({ code: 'application_name_required', severity: 'error', field: 'name', message: 'Application name is required.' });
  if (session.draft.name.includes('conflict')) issues.push({ code: 'resource_version_conflict', severity: 'error', message: 'Application changed while editing.' });
  if (session.draft.specYaml.length > 800) issues.push({ code: 'application_long_config', severity: 'warning', field: 'specYaml', message: 'Configuration is long; preview before committing.' });
  return issues;
}

export function commitAppSession(id: string) {
  const session = appSessions.get(id);
  if (!session) return null;
  if (session.draft.name.includes('conflict')) throw new Error('Application changed while editing.');
  if (session.draft.name.includes('commit-fail')) throw new Error('Commit failed after preview.');
  const app: ApplicationDto = {
    id: session.applicationId || `app-${Date.now()}`,
    version: Number(session.baseResourceVersion.value || 0) + 1,
    kind: 'application',
    name: session.draft.name,
    enabled: session.draft.enabled,
    specYaml: session.draft.specYaml,
    variables: session.draft.variables,
    deploymentMode: session.draft.deploymentMode,
    deploymentServers: session.draft.deploymentServers,
    reverseProxy: session.draft.reverseProxy,
    generation: 1,
    specHash: `sha-${Date.now()}`,
    imageReference: imageFromSpec(session.draft.specYaml),
    imageUpdateAvailable: false,
    jobId: `panel-${session.draft.name}`,
    namespace: 'default',
    runtimeStatus: session.draft.enabled ? 'deploying' : 'stopped',
    createdAt: now,
    updatedAt: now,
  };
  const index = mockApplications.findIndex((item) => item.id === app.id);
  if (index >= 0) mockApplications[index] = app;
  else mockApplications.unshift(app);
  return { application: app, resourceVersion: { value: String(app.version), updatedAt: now }, applyRequested: app.enabled, diagnostics: [{ code: 'application_apply_operation_reference_unavailable', severity: 'info', message: 'Apply requested.' }] };
}

export function restorePersistentData(id: string): OperationResult | null {
  const app = mockApplications.find((item) => item.id === id);
  if (!app) return null;
  app.runtimeStatus = app.enabled ? 'deploying' : app.runtimeStatus;
  app.updatedAt = now;
  return {
    taskId: `task-persistent-restore-${Date.now()}`,
    application: app,
    runtime: runtimes[id],
  };
}

export function beginFacilitySession(): FacilityEditSession {
  const session: FacilityEditSession = {
    id: `fedit-${Date.now()}`,
    clientDraftKey: 'facility:reverse-proxy',
    state: 'active',
    baseResourceVersion: { value: String(mockFacility.version), updatedAt: mockFacility.updatedAt },
    draft: { deploymentServers: mockFacility.deploymentServers, panelEntry: mockFacility.panelEntry, domains: mockFacility.domains },
    revision: 1,
    assets: mockFacility.staticAssets.map((asset) => ({ assetKey: asset.id, sourceAssetId: asset.id, name: asset.name, kind: asset.kind, contentMode: asset.contentMode, filename: asset.filename, size: asset.size, sha256: asset.sha256, createdAt: asset.createdAt, updatedAt: asset.updatedAt })),
    idleExpiresAt: '2026-07-22T08:00:00.000Z',
    absoluteExpiresAt: '2026-07-28T08:00:00.000Z',
    createdAt: now,
    updatedAt: now,
  };
  facilitySessions.set(session.id, session);
  return session;
}

export function facilitySession(id: string) {
  return facilitySessions.get(id);
}

export function patchFacilitySession(id: string, draft: FacilityEditSession['draft']) {
  const session = facilitySessions.get(id);
  if (!session) return null;
  session.draft = draft;
  session.revision += 1;
  return session;
}

export function putFacilityAsset(id: string, assetKey: string, input: { name: string; kind: string; contentMode?: 'text' | 'binary'; filename: string; size: number }) {
  const session = facilitySessions.get(id);
  if (!session) return null;
  session.assets = session.assets.filter((asset) => asset.assetKey !== assetKey);
  session.assets.push({
    assetKey,
    name: input.name,
    kind: input.kind,
    contentMode: input.contentMode ?? 'binary',
    filename: input.filename,
    size: input.size,
    sha256: `sha-${assetKey}`,
    createdAt: now,
    updatedAt: now,
  });
  session.revision += 1;
  session.updatedAt = now;
  return session;
}

export function deleteFacilityAsset(id: string, assetKey: string) {
  const session = facilitySessions.get(id);
  if (!session) return null;
  session.assets = session.assets.filter((asset) => asset.assetKey !== assetKey);
  session.revision += 1;
  session.updatedAt = now;
  return session;
}

export function facilityDiagnostics(session: FacilityEditSession): Diagnostic[] {
  const issues: Diagnostic[] = [];
  if (!session.draft.deploymentServers.length) issues.push({ code: 'facility_gateway_servers_required', severity: 'error', field: 'deploymentServers', message: 'At least one gateway server is required.' });
  if (session.draft.panelEntry.enabled && !session.draft.panelEntry.domain) issues.push({ code: 'facility_panel_entry_domain_invalid', severity: 'error', field: 'panelEntry.domain', message: 'Panel entry domain is required.' });
  if (session.draft.domains.some((domain) => domain.domain.includes('conflict'))) issues.push({ code: 'facility_domain_owner_conflict', severity: 'error', message: 'Domain is already used by another route.' });
  if (JSON.stringify(session.draft).length > 1200) issues.push({ code: 'facility_long_config', severity: 'warning', message: 'Gateway configuration is long.' });
  return issues;
}

export function commitFacilitySession(id: string) {
  const session = facilitySessions.get(id);
  if (!session) return null;
  if (session.draft.domains.some((domain) => domain.domain.includes('commit-fail'))) throw new Error('Gateway commit failed after preview.');
  mockFacility = {
    ...mockFacility,
    version: mockFacility.version + 1,
    deploymentServers: session.draft.deploymentServers,
    panelEntry: session.draft.panelEntry,
    domains: session.draft.domains,
    staticAssets: session.assets.map((asset) => ({
      id: asset.sourceAssetId || asset.assetKey,
      name: asset.name,
      kind: asset.kind,
      contentMode: asset.contentMode,
      filename: asset.filename,
      size: asset.size,
      sha256: asset.sha256,
      createdAt: asset.createdAt,
      updatedAt: asset.updatedAt,
    })),
    updatedAt: now,
    routes: session.draft.domains.reduce((sum, domain) => sum + domain.paths.length, session.draft.panelEntry.enabled ? 1 : 0),
  };
  return { config: mockFacility, resourceVersion: { value: String(mockFacility.version), updatedAt: now }, applyRequested: true, diagnostics: [] };
}

export function data<T>(payload: T, status = 200) {
  return new Response(JSON.stringify({ data: payload } satisfies ApiEnvelope<T>), { status, headers: { 'Content-Type': 'application/json' } });
}

function imageFromSpec(spec: string) {
  const line = spec.split('\n').find((item) => item.trim().startsWith('image:'));
  return line?.split(':').slice(1).join(':').trim() || 'nginx:1.28-alpine';
}
