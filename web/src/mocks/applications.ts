import type { ApiEnvelope } from '@/api/client';
import type { ApplicationDto, ApplicationEditSession, ApplicationFile, ApplicationRuntime, ApplicationSummaryDto, Diagnostic, LogResult, OperationResult } from '@/types/applications';
import type { FacilityEditSession, ReverseProxyConfig, StorageShareConfig, StorageShareSaveInput } from '@/types/facilityApps';

const now = '2026-08-01T08:00:00.000Z';

export const mockApplications: ApplicationDto[] = [
  {
    id: 'app-storefront',
    version: 4,
    kind: 'application',
    name: 'storefront',
    enabled: true,
    specYaml: 'name: storefront\nimage: ghcr.io/example/storefront:1.9.0\nports:\n  - label: http\n    to: 8080\nmounts:\n  - type: persistent\n    target: /data\n',
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
  {
    id: 'app-billing',
    version: 8,
    kind: 'application',
    name: 'billing-portal',
    enabled: true,
    specYaml: 'name: billing-portal\nimage: ghcr.io/example/billing:5.1.2\nports:\n  - label: http\n    to: 8088\nmounts:\n  - type: persistent\n    target: /var/lib/billing\n',
    persistentPath: '/opt/panel/apps/app-billing/persistent',
    deploymentMode: 'selected',
    deploymentServers: ['srv-api-hkg', 'srv-edge-sgp'],
    reverseProxy: [{ domain: 'billing.example.test', targetType: 'local', targetPort: 8088, originServerIds: ['srv-api-hkg', 'srv-edge-sgp'], anyAccess: { enabled: true, strategy: 'least_conn' }, paths: [{ path: '/', webSocket: false }, { path: '/webhooks', webSocket: false }] }],
    generation: 9,
    specHash: 'hash-billing',
    imageReference: 'ghcr.io/example/billing:5.1.2',
    imageDigest: 'sha256:billing-current',
    imageLatestDigest: 'sha256:billing-newer',
    imageCheckedAt: now,
    imageUpdateAvailable: true,
    imageUpdateTargets: [{ serverId: 'srv-api-hkg', serverName: 'api-hkg-01', reference: 'ghcr.io/example/billing:5.1.2', updateAvailable: true, checkedAt: now }],
    jobId: 'panel-billing',
    namespace: 'finance',
    runtimeStatus: 'deployed',
    lastDeploymentId: 'deploy-billing-44',
    createdAt: now,
    updatedAt: now,
  },
  {
    id: 'app-webhook',
    version: 3,
    kind: 'application',
    name: 'webhook-ingress',
    enabled: true,
    specYaml: 'name: webhook-ingress\nimage: ghcr.io/example/webhooks:1.4.0\nports:\n  - label: http\n    to: 8091\n',
    deploymentMode: 'selected',
    deploymentServers: ['srv-edge-sgp', 'srv-edge-lax'],
    reverseProxy: [{ domain: 'hooks.example.test', targetType: 'local', targetPort: 8091, originServerIds: ['srv-edge-sgp', 'srv-edge-lax'], anyAccess: { enabled: true, strategy: 'round_robin' }, paths: [{ path: '/v1/hooks', webSocket: false }] }],
    generation: 4,
    specHash: 'hash-webhook',
    imageReference: 'ghcr.io/example/webhooks:1.4.0',
    imageUpdateAvailable: false,
    jobId: 'panel-webhooks',
    namespace: 'default',
    runtimeStatus: 'partially_deployed',
    lastError: 'srv-edge-lax agent report is stale.',
    createdAt: now,
    updatedAt: now,
  },
  {
    id: 'app-media',
    version: 5,
    kind: 'application',
    name: 'media-transcoder',
    enabled: true,
    specYaml: 'name: media-transcoder\nimage: ghcr.io/example/transcoder:0.12.3\nrestart:\n  policy: unless-stopped\nresources:\n  gpu: 1\n',
    persistentPath: '/opt/panel/apps/app-media/persistent',
    deploymentMode: 'selected',
    deploymentServers: ['srv-media-syd', 'srv-gpu-nrt'],
    reverseProxy: [],
    generation: 6,
    specHash: 'hash-media',
    imageReference: 'ghcr.io/example/transcoder:0.12.3',
    imageUpdateAvailable: true,
    imageLastError: 'Registry rate limited while checking media image.',
    jobId: 'panel-media',
    namespace: 'media',
    runtimeStatus: 'deploying',
    createdAt: now,
    updatedAt: now,
  },
  {
    id: 'app-cache-sidecar',
    version: 2,
    kind: 'application',
    name: 'redis-sidecar',
    enabled: true,
    specYaml: 'name: redis-sidecar\nimage: redis:7.4-alpine\nports:\n  - label: redis\n    to: 6379\n',
    deploymentMode: 'selected',
    deploymentServers: ['srv-cache-sfo'],
    reverseProxy: [],
    generation: 2,
    specHash: 'hash-redis',
    imageReference: 'redis:7.4-alpine',
    imageUpdateAvailable: false,
    jobId: 'panel-redis-sidecar',
    namespace: 'infra',
    runtimeStatus: 'deployed',
    createdAt: now,
    updatedAt: now,
  },
  {
    id: 'app-canary-broken',
    version: 11,
    kind: 'application',
    name: 'checkout-canary',
    enabled: true,
    specYaml: 'name: checkout-canary\nimage: ghcr.io/example/checkout:canary-2026.08.01\nports:\n  - label: http\n    to: 8080\n',
    deploymentMode: 'selected',
    deploymentServers: ['srv-api-hkg-02'],
    reverseProxy: [{ domain: 'checkout-canary.example.test', targetType: 'local', targetPort: 8080, originServerIds: ['srv-api-hkg-02'], anyAccess: { enabled: false }, paths: [{ path: '/', webSocket: false }] }],
    generation: 11,
    specHash: 'hash-canary',
    imageReference: 'ghcr.io/example/checkout:canary-2026.08.01',
    imageUpdateAvailable: false,
    jobId: 'panel-checkout-canary',
    namespace: 'default',
    runtimeStatus: 'failed',
    lastError: 'Canary probe failed: /ready returned 503 for 3 consecutive checks.',
    createdAt: now,
    updatedAt: now,
  },
  {
    id: 'app-backup-agent',
    version: 1,
    kind: 'application',
    name: 'backup-agent',
    enabled: true,
    specYaml: 'name: backup-agent\nimage: ghcr.io/example/backup-agent:2.1.0\nschedule: "0 2 * * *"\n',
    deploymentMode: 'selected',
    deploymentServers: ['srv-backup-fra', 'srv-db-fra'],
    reverseProxy: [],
    generation: 1,
    specHash: 'hash-backup',
    imageReference: 'ghcr.io/example/backup-agent:2.1.0',
    imageUpdateAvailable: false,
    jobId: 'panel-backup-agent',
    namespace: 'ops',
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
  'app-billing': {
    applicationId: 'app-billing',
    runtimeId: 'runtime-billing',
    status: 'deployed',
    observedAt: now,
    instances: [
      { instanceId: 'inst-billing-api', serverId: 'srv-api-hkg', serverName: 'api-hkg-01', containerName: 'panel-billing', status: 'running', observedGeneration: 9 },
      { instanceId: 'inst-billing-edge', serverId: 'srv-edge-sgp', serverName: 'edge-sgp-01', containerName: 'panel-billing', status: 'running', observedGeneration: 9 },
    ],
  },
  'app-webhook': {
    applicationId: 'app-webhook',
    runtimeId: 'runtime-webhook',
    status: 'partially_deployed',
    observedAt: now,
    instances: [
      { instanceId: 'inst-webhook-sgp', serverId: 'srv-edge-sgp', serverName: 'edge-sgp-01', containerName: 'panel-webhooks', status: 'running', observedGeneration: 4 },
      { instanceId: 'inst-webhook-lax', serverId: 'srv-edge-lax', serverName: 'edge-lax-01', containerName: 'panel-webhooks', status: 'unknown', error: 'agent report stale for 12m' },
    ],
  },
  'app-media': {
    applicationId: 'app-media',
    runtimeId: 'runtime-media',
    status: 'deploying',
    observedAt: now,
    instances: [
      { instanceId: 'inst-media-syd', serverId: 'srv-media-syd', serverName: 'media-syd-transcode', containerName: 'panel-media', status: 'running', observedGeneration: 6 },
      { instanceId: 'inst-media-gpu', serverId: 'srv-gpu-nrt', serverName: 'gpu-nrt-render', containerName: 'panel-media', status: 'starting', observedGeneration: 6 },
    ],
    operation: { id: 'op-media', applicationId: 'app-media', type: 'deploy', status: 'deploying', taskId: 'task-media-12', generation: 6, trigger: 'user', createdAt: now, updatedAt: now },
  },
  'app-cache-sidecar': {
    applicationId: 'app-cache-sidecar',
    runtimeId: 'runtime-redis',
    status: 'deployed',
    observedAt: now,
    instances: [{ instanceId: 'inst-redis', serverId: 'srv-cache-sfo', serverName: 'cache-sfo-edge', containerName: 'panel-redis-sidecar', status: 'running', observedGeneration: 2 }],
  },
  'app-canary-broken': {
    applicationId: 'app-canary-broken',
    runtimeId: 'runtime-canary',
    status: 'failed',
    observedAt: now,
    instances: [{ instanceId: 'inst-canary', serverId: 'srv-api-hkg-02', serverName: 'api-hkg-02-canary', containerName: 'panel-checkout-canary', status: 'failed', error: 'readiness probe failed' }],
    operation: { id: 'op-canary', applicationId: 'app-canary-broken', type: 'deploy', status: 'failed', taskId: 'task-canary-3', generation: 11, trigger: 'user', error: 'Canary probe failed.', createdAt: now, updatedAt: now },
  },
  'app-backup-agent': {
    applicationId: 'app-backup-agent',
    runtimeId: 'runtime-backup',
    status: 'stopped',
    observedAt: now,
    instances: [],
  },
};

export let mockFacility: ReverseProxyConfig = {
  id: 'reverse_proxy',
  version: 8,
  deploymentServers: ['srv-edge-sgp', 'srv-edge-sgp-02', 'srv-api-hkg', 'srv-api-hkg-02', 'srv-edge-lax'],
  domains: [
    {
      domain: 'static.example.test',
      originServerIds: ['srv-edge-sgp', 'srv-api-hkg'],
      anyAccess: { enabled: true, strategy: 'round_robin' },
      paths: [{ path: '/', ruleType: 'static', sourceType: 'uploaded_bundle', assetName: 'docs bundle' }],
    },
    {
      domain: 'downloads.example.test',
      originServerIds: ['srv-api-hkg-02'],
      anyAccess: { enabled: false },
      paths: [{ path: '/releases', ruleType: 'static', sourceType: 'uploaded_file', assetName: 'release index' }],
    },
    {
      domain: 'status.example.test',
      originServerIds: ['srv-edge-sgp-02'],
      anyAccess: { enabled: true, strategy: 'first' },
      paths: [{ path: '/', ruleType: 'static', sourceType: 'uploaded_file', assetName: 'status page' }],
    },
  ],
  staticAssets: [
    { name: 'docs bundle', kind: 'uploaded_bundle', contentMode: 'binary', filename: 'docs.zip', size: 7340032, sha256: 'sha-docs', createdAt: now, updatedAt: now },
    { name: 'release index', kind: 'uploaded_file', contentMode: 'binary', filename: 'index.html', size: 48216, sha256: 'sha-release-index', createdAt: now, updatedAt: now },
    { name: 'status page', kind: 'uploaded_file', contentMode: 'text', filename: 'status.html', size: 8192, sha256: 'sha-status', createdAt: now, updatedAt: now },
  ],
  routeSummaries: [
    { domain: 'panel.example.test', path: '/', source: 'system_panel', serverIds: ['srv-edge-sgp'], httpsStatus: 'domain_certificate', certificateName: 'panel.example.test' },
    { domain: 'shop.example.test', path: '/', source: 'application', serverIds: ['srv-edge-sgp', 'srv-api-hkg'], httpsStatus: 'domain_certificate', applicationId: 'app-storefront', applicationName: 'storefront' },
    { domain: 'api.example.test', path: '/v1', source: 'application', serverIds: ['srv-api-hkg', 'srv-api-hkg-02'], httpsStatus: 'domain_certificate', applicationId: 'app-api', applicationName: 'public-api' },
    { domain: 'billing.example.test', path: '/', source: 'application', serverIds: ['srv-api-hkg', 'srv-edge-sgp'], httpsStatus: 'domain_certificate', applicationId: 'app-billing', applicationName: 'billing-portal' },
    { domain: 'hooks.example.test', path: '/v1/hooks', source: 'application', serverIds: ['srv-edge-sgp', 'srv-edge-lax'], httpsStatus: 'domain_certificate', applicationId: 'app-webhook', applicationName: 'webhook-ingress' },
    { domain: 'static.example.test', path: '/', source: 'static_asset', serverIds: ['srv-edge-sgp', 'srv-api-hkg'], httpsStatus: 'self_signed_certificate' },
    { domain: 'downloads.example.test', path: '/releases', source: 'static_asset', serverIds: ['srv-api-hkg-02'], httpsStatus: 'missing' },
    { domain: 'status.example.test', path: '/', source: 'static_asset', serverIds: ['srv-edge-sgp-02'], httpsStatus: 'domain_certificate', certificateName: 'status.example.test' },
  ],
  applicationRoutes: [
    { applicationId: 'app-storefront', applicationName: 'storefront', deploymentMode: 'selected', deploymentServers: ['srv-edge-sgp', 'srv-api-hkg'], routes: [{ domain: 'shop.example.test', targetPort: 8080, originServerIds: ['srv-edge-sgp', 'srv-api-hkg'], paths: [{ path: '/' }] }] },
    { applicationId: 'app-api', applicationName: 'public-api', deploymentMode: 'selected', deploymentServers: ['srv-api-hkg', 'srv-api-hkg-02'], routes: [{ domain: 'api.example.test', targetPort: 9000, originServerIds: ['srv-api-hkg', 'srv-api-hkg-02'], paths: [{ path: '/v1' }, { path: '/events' }] }] },
    { applicationId: 'app-billing', applicationName: 'billing-portal', deploymentMode: 'selected', deploymentServers: ['srv-api-hkg', 'srv-edge-sgp'], routes: [{ domain: 'billing.example.test', targetPort: 8088, originServerIds: ['srv-api-hkg', 'srv-edge-sgp'], paths: [{ path: '/' }, { path: '/webhooks' }] }] },
    { applicationId: 'app-webhook', applicationName: 'webhook-ingress', deploymentMode: 'selected', deploymentServers: ['srv-edge-sgp', 'srv-edge-lax'], routes: [{ domain: 'hooks.example.test', targetPort: 8091, originServerIds: ['srv-edge-sgp', 'srv-edge-lax'], paths: [{ path: '/v1/hooks' }] }] },
    { applicationId: 'app-internal-docs', applicationName: 'internal-docs', deploymentMode: 'selected', deploymentServers: ['srv-observability-ams'], routes: [{ domain: 'docs.example.test', targetPort: 8080, originServerIds: ['srv-observability-ams'], paths: [{ path: '/' }] }] },
  ],
  operation: { id: 'op-facility', applicationId: 'facility-reverse-proxy', type: 'deploy', status: 'deployed', generation: 8, createdAt: now, updatedAt: now },
  updatedAt: now,
  routes: 12,
  enabledServers: ['srv-edge-sgp', 'srv-edge-sgp-02', 'srv-api-hkg', 'srv-api-hkg-02', 'srv-edge-lax'],
};

export function mockApplicationSummaries(): ApplicationSummaryDto[] {
  return mockApplications.map((app) => ({
    id: app.id,
    name: app.name,
    enabled: app.enabled,
    imageReference: app.imageReference,
    instanceCount: runtimes[app.id]?.instances.length ?? 0,
    jobId: app.jobId,
    namespace: app.namespace,
    runtimeStatus: app.runtimeStatus,
    imageUpdateAvailable: app.imageUpdateAvailable,
    lastError: app.lastError,
    updatedAt: app.updatedAt,
  }));
}

const appSessions = new Map<string, ApplicationEditSession>();
const facilitySessions = new Map<string, FacilityEditSession>();

export function appRuntime(id: string) {
  return runtimes[id];
}

const logLines: Record<string, string[]> = {
  'app-storefront': [
    '[info] listening on :8080',
    '[info] route / resolved',
    '[warn] upstream retry from core-1',
    '[info] checkout session created sid=cs_test_01',
    '[info] inventory cache hit sku=sku-441',
  ],
  'app-api': [
    '[info] public-api ready region=apac',
    '[info] GET /v1/health 200 4ms',
    '[info] GET /v1/orders 200 18ms',
    '[warn] rate limit soft threshold crossed for tenant=acme',
  ],
  'app-billing': [
    '[info] billing-portal ready',
    '[info] webhook signature verified provider=stripe',
    '[info] invoice finalized id=in_1042',
  ],
  'app-webhook': [
    '[info] webhook-ingress listening on :8091',
    '[info] accepted delivery id=deliv_88 source=github',
    '[warn] retry scheduled for delivery id=deliv_91',
  ],
  'app-media': [
    '[info] transcoder worker online gpu=1',
    '[info] job job_221 queued preset=slow',
    '[info] job job_220 completed duration=42s',
  ],
  'app-canary-broken': [
    '[error] readiness probe failed path=/ready status=503',
    '[error] circuit open after 3 consecutive failures',
    '[warn] traffic weight reduced to 0',
  ],
  'app-analytics': [
    '[error] worker exited code=137',
    '[warn] batch flush deferred because clickhouse is slow',
    '[info] residual workers still processing shard=2',
  ],
};

export function appLogs(id: string): LogResult {
  if (id === 'app-worker') throw new Error('Runtime log stream is temporarily unavailable.');
  const lines = logLines[id] ?? [
    `[info] ${id} runtime attached`,
    '[info] health endpoint responded 200',
    '[debug] metrics scrape completed',
  ];
  return {
    instanceId: `inst-${id.replace(/^app-/, '')}`,
    containerName: `panel-${id.replace(/^app-/, '')}`,
    type: 'stdout',
    logs: lines.join('\n'),
  };
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

export function listAppFiles(applicationId: string): ApplicationFile[] | null {
  const app = mockApplications.find((item) => item.id === applicationId);
  if (!app) return null;
  const files: ApplicationFile[] = [
    {
      name: 'app.yaml',
      kind: 'binary',
      contentType: 'text/yaml',
      size: app.specYaml.length,
      sha256: `sha-spec-${applicationId}`,
      createdAt: app.createdAt,
      updatedAt: app.updatedAt,
    },
  ];
  if (app.persistentPath) {
    files.push({
      name: 'persistent/',
      kind: 'binary',
      contentType: 'application/vnd.panel.directory',
      size: 0,
      sha256: `sha-persistent-${applicationId}`,
      createdAt: app.createdAt,
      updatedAt: app.updatedAt,
    });
  }
  return files;
}

export function deployedAppFileContent(applicationId: string, fileName: string): { name: string; contentType: string; content: string } | null {
  const app = mockApplications.find((item) => item.id === applicationId);
  if (!app) return null;
  if (fileName === 'app.yaml') return { name: fileName, contentType: 'text/yaml', content: app.specYaml };
  return null;
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
      specYaml: app?.specYaml ?? '',
      deploymentMode: app?.deploymentMode === 'selected' ? 'selected' : 'all',
      deploymentServers: app?.deploymentServers ?? [],
      reverseProxy: app?.reverseProxy ?? [],
    },
    revision: 1,
    files: [{ name: 'env-template', kind: 'template', contentType: 'text/plain', size: 12, sha256: 'sha-env', contentBase64: 'SE9TVD17eyBob3N0IH19Cg==', createdAt: now, updatedAt: now }],
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

export function getAppFile(id: string, fileName: string) {
  const file = appSessions.get(id)?.files.find((item) => item.name === fileName);
  return file ? { ...file, contentBase64: file.contentBase64 ?? '' } : null;
}

export function putAppFile(id: string, fileName: string, input: { name: string; kind: string; contentType: string; contentBase64: string }) {
  const session = appSessions.get(id);
  if (!session) return null;
  session.files = session.files.filter((file) => file.name !== fileName);
  session.files.push({ name: input.name, kind: input.kind, contentType: input.contentType, size: input.contentBase64.length, sha256: `sha-${fileName}`, contentBase64: input.contentBase64, createdAt: now, updatedAt: now });
  session.revision += 1;
  return session;
}

export function uploadAppArchive(id: string, input: { name: string; filename: string; size: number; contentType: string }) {
  const session = appSessions.get(id);
  if (!session) return null;
  const name = input.name.trim() || input.filename;
  session.files = session.files.filter((file) => file.name !== name);
  session.files.push({
    name,
    kind: 'archive',
    contentType: input.contentType,
    size: input.size,
    sha256: `sha-${name}`,
    createdAt: now,
    updatedAt: now,
  });
  session.revision += 1;
  session.updatedAt = now;
  return session;
}

export function uploadAppFile(id: string, fileName: string, input: { name: string; filename: string; size: number; contentType: string }) {
  const session = appSessions.get(id);
  if (!session) return null;
  const name = input.name.trim() || input.filename;
  session.files = session.files.filter((file) => file.name !== name);
  session.files.push({
    name,
    kind: 'binary',
    contentType: input.contentType,
    size: input.size,
    sha256: `sha-${name}`,
    createdAt: now,
    updatedAt: now,
  });
  session.revision += 1;
  session.updatedAt = now;
  return session;
}

export function appFileContent(id: string, fileName: string): { name: string; contentType: string; content: string } | null {
  const file = appSessions.get(id)?.files.find((item) => item.name === fileName);
  if (!file) return null;
  return {
    name: file.name,
    contentType: file.contentType || 'application/octet-stream',
    content: decodeBase64(file.contentBase64 ?? ''),
  };
}

function decodeBase64(value: string) {
  const binary = atob(value);
  const bytes = Uint8Array.from(binary, (character) => character.charCodeAt(0));
  return new TextDecoder('utf-8').decode(bytes);
}

export function deleteAppFile(id: string, fileName: string) {
  const session = appSessions.get(id);
  if (!session) return null;
  session.files = session.files.filter((file) => file.name !== fileName);
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
    draft: { deploymentServers: mockFacility.deploymentServers, domains: mockFacility.domains },
    revision: 1,
    assets: mockFacility.staticAssets.map((asset) => ({ name: asset.name, kind: asset.kind, contentMode: asset.contentMode, filename: asset.filename, size: asset.size, sha256: asset.sha256, createdAt: asset.createdAt, updatedAt: asset.updatedAt })),
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

export function putFacilityAsset(id: string, assetName: string, input: { name: string; kind: string; contentMode?: 'text' | 'binary'; filename: string; size: number }) {
  const session = facilitySessions.get(id);
  if (!session) return null;
  session.assets = session.assets.filter((asset) => asset.name !== assetName);
  session.assets.push({
    name: input.name,
    kind: input.kind,
    contentMode: input.contentMode ?? 'binary',
    filename: input.filename,
    size: input.size,
    sha256: `sha-${assetName}`,
    createdAt: now,
    updatedAt: now,
  });
  session.revision += 1;
  session.updatedAt = now;
  return session;
}

export function deleteFacilityAsset(id: string, assetName: string) {
  const session = facilitySessions.get(id);
  if (!session) return null;
  session.assets = session.assets.filter((asset) => asset.name !== assetName);
  session.revision += 1;
  session.updatedAt = now;
  return session;
}

export function facilityAssetContent(id: string, assetName: string): { name: string; contentType: string; content: string } | null {
  const asset = facilitySessions.get(id)?.assets.find((item) => item.name === assetName);
  if (!asset) return null;
  return {
    name: asset.filename || asset.name,
    contentType: asset.contentMode === 'text' ? 'text/plain' : 'application/octet-stream',
    content: `mock facility edit asset: ${assetName}\n`,
  };
}

export function facilityStaticAssetContent(assetName: string): { name: string; contentType: string; content: string } | null {
  const asset = mockFacility.staticAssets.find((item) => item.name === assetName);
  if (!asset) return null;
  return {
    name: asset.filename || asset.name,
    contentType: asset.contentMode === 'text' ? 'text/plain' : 'application/octet-stream',
    content: `mock static asset: ${assetName}\n`,
  };
}

export function facilityDiagnostics(session: FacilityEditSession): Diagnostic[] {
  const issues: Diagnostic[] = [];
  if (!session.draft.deploymentServers.length) issues.push({ code: 'facility_gateway_servers_required', severity: 'error', field: 'deploymentServers', message: 'At least one gateway server is required.' });
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
    domains: session.draft.domains,
    staticAssets: session.assets.map((asset) => ({
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
    routes: session.draft.domains.reduce((sum, domain) => sum + domain.paths.length, 0),
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

export let mockStorageShare: StorageShareConfig = {
  id: 'storage-share',
  version: 1,
  servers: [],
  enabled: false,
  partitions: [],
  updatedAt: now,
};

export function saveStorageShare(input: StorageShareSaveInput): StorageShareConfig {
  mockStorageShare = {
    ...mockStorageShare,
    version: mockStorageShare.version + 1,
    servers: (input.servers ?? []).map((item) => ({ serverId: item.serverId, root: item.root })),
    enabled: (input.servers?.length ?? 0) > 0,
    lastError: undefined,
    updatedAt: now,
  };
  return mockStorageShare;
}

export function uninstallStorageShare(): void {
  mockStorageShare = {
    ...mockStorageShare,
    version: mockStorageShare.version + 1,
    servers: [],
    enabled: false,
    updatedAt: now,
  };
}

export function deleteStoragePartition(partitionId: string): boolean {
  const before = mockStorageShare.partitions.length;
  mockStorageShare = {
    ...mockStorageShare,
    partitions: mockStorageShare.partitions.filter((partition) => partition.id !== partitionId),
    updatedAt: now,
  };
  return mockStorageShare.partitions.length !== before;
}
