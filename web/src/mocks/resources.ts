import type { ContainerDto, ImageDto, ImageList, NetworkDto, PackageUpdateList, VolumeDto } from '@/types/resources';

const now = '2026-07-21T03:00:00.000Z';

const packages: Record<string, PackageUpdateList> = {
  'srv-edge-sgp': {
    serverId: 'srv-edge-sgp',
    lastRefreshedAt: now,
    refreshing: false,
    updates: [
      { name: 'openssl', installedVersion: '3.0.13-1', candidateVersion: '3.0.14-1', source: 'debian-security' },
      { name: 'docker-ce', installedVersion: '26.1.2', candidateVersion: '27.0.3', source: 'download.docker.com' },
      { name: 'linux-image-amd64', installedVersion: '6.1.0-21', candidateVersion: '6.1.0-23', source: 'debian' },
    ],
  },
  'srv-core-fra': { serverId: 'srv-core-fra', lastRefreshedAt: now, refreshing: false, updates: [] },
  'srv-api-hkg': {
    serverId: 'srv-api-hkg',
    lastRefreshedAt: now,
    refreshing: false,
    updates: [
      { name: 'curl', installedVersion: '8.5.0-2', candidateVersion: '8.5.0-2+deb12u1', source: 'debian-security' },
      { name: 'containerd.io', installedVersion: '1.6.31', candidateVersion: '1.7.18', source: 'download.docker.com' },
      { name: 'libssl3', installedVersion: '3.0.13-1', candidateVersion: '3.0.14-1', source: 'debian-security' },
    ],
  },
  'srv-api-hkg-02': {
    serverId: 'srv-api-hkg-02',
    lastRefreshedAt: now,
    refreshing: false,
    updates: [{ name: 'ca-certificates', installedVersion: '20230311', candidateVersion: '20240203', source: 'debian' }],
  },
  'srv-worker-nrt': {
    serverId: 'srv-worker-nrt',
    lastRefreshedAt: now,
    refreshing: false,
    updates: Array.from({ length: 12 }, (_, index) => ({ name: `worker-runtime-lib-${index + 1}`, installedVersion: `1.${index}.0`, candidateVersion: `1.${index}.1`, source: index % 2 ? 'debian' : 'debian-security' })),
  },
  'srv-db-fra': { serverId: 'srv-db-fra', lastRefreshedAt: now, refreshing: false, updates: [] },
  'srv-cache-sfo': {
    serverId: 'srv-cache-sfo',
    lastRefreshedAt: now,
    refreshing: false,
    updates: [
      { name: 'redis-server', installedVersion: '7.0.15', candidateVersion: '7.2.5', source: 'packages.redis.io' },
      { name: 'ufw', installedVersion: '0.36.2', candidateVersion: '0.36.2-2', source: 'debian' },
    ],
  },
};

const containers: Record<string, ContainerDto[]> = {
  'srv-edge-sgp': [
    container('ctr-nginx', 'nginx-gateway', 'nginx:1.28-alpine', 'running', false),
    container('ctr-api', 'storefront-api', 'ghcr.io/panel/storefront:2026.07', 'running', true),
    container('ctr-cache', 'old-cache-worker', 'redis:7', 'exited', false),
  ],
  'srv-core-fra': [],
  'srv-api-hkg': [
    container('ctr-public-api-a', 'public-api-a', 'ghcr.io/example/public-api:4.6.1', 'running', true),
    container('ctr-envoy-sidecar', 'envoy-sidecar', 'envoyproxy/envoy:v1.31', 'running', false),
    container('ctr-migration-leftover', 'schema-migration-leftover', 'ghcr.io/example/migrate:4.5.0', 'exited', false),
  ],
  'srv-api-hkg-02': [
    container('ctr-public-api-canary', 'public-api-canary', 'ghcr.io/example/public-api:4.7.0-rc1', 'running', true),
    container('ctr-release-index', 'release-index-static', 'nginx:1.28-alpine', 'running', false),
  ],
  'srv-worker-nrt': [
    container('ctr-analytics-a', 'analytics-pipeline-a', 'ghcr.io/example/analytics:3.2.0', 'exited', true),
    container('ctr-queue-consumer', 'queue-consumer-critical', 'ghcr.io/example/worker:2.0.0', 'running', true),
  ],
  'srv-db-fra': [
    container('ctr-postgres', 'postgres-primary', 'postgres:16', 'running', false),
    container('ctr-backup-sidecar', 'postgres-backup-sidecar', 'ghcr.io/example/backup-agent:1.4.0', 'running', false),
  ],
  'srv-cache-sfo': [container('ctr-redis', 'redis-edge-cache', 'redis:7.2', 'running', false)],
};

const imageItems: Record<string, ImageList> = {
  'srv-edge-sgp': {
    serverId: 'srv-edge-sgp',
    lastRefreshedAt: now,
    refreshing: false,
    items: [
      image('img-nginx', 'nginx:1.28-alpine', false, true, []),
      image('img-storefront', 'ghcr.io/panel/storefront:2026.07', true, true, ['app-storefront']),
      image('img-unused', 'busybox:1.36', false, false, []),
    ],
  },
  'srv-core-fra': { serverId: 'srv-core-fra', refreshing: false, items: [] },
  'srv-api-hkg': {
    serverId: 'srv-api-hkg',
    lastRefreshedAt: now,
    refreshing: false,
    items: [
      image('img-public-api', 'ghcr.io/example/public-api:4.6.1', false, true, ['app-api']),
      image('img-envoy', 'envoyproxy/envoy:v1.31', true, true, []),
      image('img-old-api', 'ghcr.io/example/public-api:4.4.0', false, false, []),
    ],
  },
  'srv-api-hkg-02': {
    serverId: 'srv-api-hkg-02',
    lastRefreshedAt: now,
    refreshing: false,
    items: [
      image('img-public-api-canary', 'ghcr.io/example/public-api:4.7.0-rc1', false, true, ['app-api']),
      image('img-release-nginx', 'nginx:1.28-alpine', false, true, []),
    ],
  },
  'srv-worker-nrt': {
    serverId: 'srv-worker-nrt',
    lastRefreshedAt: now,
    refreshing: false,
    items: [
      image('img-analytics', 'ghcr.io/example/analytics:3.2.0', true, true, ['app-analytics']),
      image('img-worker', 'ghcr.io/example/worker:2.0.0', false, true, ['app-worker']),
      image('img-temp-python', 'python:3.12-slim', false, false, []),
    ],
  },
  'srv-db-fra': {
    serverId: 'srv-db-fra',
    lastRefreshedAt: now,
    refreshing: false,
    items: [
      image('img-postgres', 'postgres:16', false, true, []),
      image('img-backup-sidecar', 'ghcr.io/example/backup-agent:1.4.0', true, true, []),
    ],
  },
  'srv-cache-sfo': {
    serverId: 'srv-cache-sfo',
    lastRefreshedAt: now,
    refreshing: false,
    items: [image('img-redis', 'redis:7.2', true, true, [])],
  },
};

const networks: Record<string, NetworkDto[]> = {
  'srv-edge-sgp': [
    { id: 'net-panel', name: 'panel-apps', driver: 'bridge', scope: 'local', internal: false, labels: { 'panel.managed': 'true' } },
    { id: 'net-host', name: 'host', driver: 'host', scope: 'local', internal: false, labels: {} },
    { id: 'net-secure', name: 'backplane-internal', driver: 'bridge', scope: 'local', internal: true, labels: {} },
  ],
  'srv-core-fra': [],
  'srv-api-hkg': [
    { id: 'net-api', name: 'api-public', driver: 'bridge', scope: 'local', internal: false, labels: { 'panel.managed': 'true' } },
    { id: 'net-api-private', name: 'api-private', driver: 'bridge', scope: 'local', internal: true, labels: {} },
  ],
  'srv-api-hkg-02': [
    { id: 'net-canary', name: 'canary', driver: 'bridge', scope: 'local', internal: false, labels: { 'panel.canary': 'true' } },
  ],
  'srv-worker-nrt': [
    { id: 'net-worker', name: 'worker-backplane', driver: 'bridge', scope: 'local', internal: true, labels: {} },
    { id: 'net-observability', name: 'observability-exporters', driver: 'bridge', scope: 'local', internal: false, labels: {} },
  ],
  'srv-db-fra': [
    { id: 'net-db', name: 'database-private', driver: 'bridge', scope: 'local', internal: true, labels: {} },
  ],
  'srv-cache-sfo': [
    { id: 'net-cache', name: 'cache-edge', driver: 'bridge', scope: 'local', internal: false, labels: {} },
  ],
};

const volumes: Record<string, VolumeDto[]> = {
  'srv-edge-sgp': [
    { name: 'postgres-data', driver: 'local', mountpoint: '/var/lib/docker/volumes/postgres-data/_data', labels: {}, inUse: true, containerCount: 1, usageData: { size: 19327352832, refCount: 1 } },
    { name: 'gateway-cache', driver: 'local', mountpoint: '/var/lib/docker/volumes/gateway-cache/_data', labels: {}, inUse: false, containerCount: 0, usageData: { size: 268435456, refCount: 0 } },
  ],
  'srv-core-fra': [],
  'srv-api-hkg': [
    { name: 'api-cache', driver: 'local', mountpoint: '/var/lib/docker/volumes/api-cache/_data', labels: {}, inUse: true, containerCount: 1, usageData: { size: 2147483648, refCount: 1 } },
    { name: 'old-api-upload-staging', driver: 'local', mountpoint: '/var/lib/docker/volumes/old-api-upload-staging/_data', labels: {}, inUse: false, containerCount: 0, usageData: { size: 134217728, refCount: 0 } },
  ],
  'srv-api-hkg-02': [
    { name: 'release-index-content', driver: 'local', mountpoint: '/var/lib/docker/volumes/release-index-content/_data', labels: {}, inUse: true, containerCount: 1, usageData: { size: 805306368, refCount: 1 } },
  ],
  'srv-worker-nrt': [
    { name: 'analytics-spool', driver: 'local', mountpoint: '/var/lib/docker/volumes/analytics-spool/_data', labels: {}, inUse: true, containerCount: 1, usageData: { size: 51539607552, refCount: 1 } },
    { name: 'failed-job-artifacts', driver: 'local', mountpoint: '/var/lib/docker/volumes/failed-job-artifacts/_data', labels: {}, inUse: false, containerCount: 0, usageData: { size: 6442450944, refCount: 0 } },
  ],
  'srv-db-fra': [
    { name: 'postgres-data', driver: 'local', mountpoint: '/var/lib/docker/volumes/postgres-data/_data', labels: {}, inUse: true, containerCount: 1, usageData: { size: 858993459200, refCount: 1 } },
    { name: 'wal-archive', driver: 'local', mountpoint: '/var/lib/docker/volumes/wal-archive/_data', labels: {}, inUse: true, containerCount: 1, usageData: { size: 257698037760, refCount: 1 } },
  ],
  'srv-cache-sfo': [
    { name: 'redis-data', driver: 'local', mountpoint: '/var/lib/docker/volumes/redis-data/_data', labels: {}, inUse: true, containerCount: 1, usageData: { size: 3221225472, refCount: 1 } },
  ],
};

export function mockPackages(serverId: string) {
  if (serverId.includes('dead') || serverId.includes('timeout')) throw new Error('Server is unreachable.');
  if (!packages[serverId]) packages[serverId] = defaultPackages(serverId);
  return packages[serverId];
}

export function mockRefreshPackages(serverId: string) {
  const state = packages[serverId];
  if (!state) return null;
  state.refreshing = true;
  return { serverId, refreshing: true, taskId: `package-refresh-${Date.now()}` };
}

export function mockUpgradePackages(serverId: string, names?: string[]) {
  const state = packages[serverId];
  if (!state) return null;
  if (names && names.length) state.updates = state.updates.filter((item) => !names.includes(item.name));
  else state.updates = [];
  return { taskId: `package-upgrade-${Date.now()}` };
}

export function mockContainers(serverId: string) {
  if (serverId.includes('dead') || serverId.includes('timeout')) throw new Error('Agent is required for Docker resources.');
  if (!containers[serverId]) containers[serverId] = defaultContainers(serverId);
  return containers[serverId];
}

export function mockContainerAction(serverId: string, containerId: string, action: string) {
  const item = containers[serverId]?.find((candidate) => candidate.id === containerId);
  if (!item) return 'missing';
  if (item.managed) return 'managed';
  if (action === 'start') item.state = 'running';
  if (action === 'stop') item.state = 'exited';
  if (action === 'restart') item.status = 'Restarted less than a second ago';
  return 'ok';
}

export function mockDeleteContainer(serverId: string, containerId: string) {
  const item = containers[serverId]?.find((candidate) => candidate.id === containerId);
  if (!item) return 'missing';
  if (item.managed) return 'managed';
  containers[serverId] = containers[serverId].filter((candidate) => candidate.id !== containerId);
  return 'ok';
}

export function mockContainerLogs(serverId: string, containerId: string) {
  if (!containers[serverId]?.some((item) => item.id === containerId)) return null;
  return { containerId, logs: Array.from({ length: 80 }, (_, index) => `[2026-07-21T03:${String(index).padStart(2, '0')}:00Z] request completed from edge gateway with trace=${containerId}-${index}`).join('\n') };
}

export function mockImages(serverId: string) {
  if (serverId.includes('dead') || serverId.includes('timeout')) throw new Error('Agent is not compatible with Docker resources.');
  if (!imageItems[serverId]) imageItems[serverId] = defaultImages(serverId);
  return imageItems[serverId];
}

export function mockPullImage(serverId: string, reference: string) {
  const list = imageItems[serverId];
  if (!list) return null;
  list.items = [image(`img-${Date.now()}`, reference, false, false, []), ...list.items];
  return { refreshTaskId: `image-refresh-${Date.now()}` };
}

export function mockDeleteImage(serverId: string, id: string) {
  const list = imageItems[serverId];
  if (!list) return 'missing';
  const item = list.items.find((candidate) => candidate.id === id);
  if (!item) return 'missing';
  if (item.inUse) return 'in_use';
  list.items = list.items.filter((candidate) => candidate.id !== id);
  return 'ok';
}

export function mockPruneImages(serverId: string) {
  const list = imageItems[serverId];
  if (!list) return null;
  list.items = list.items.filter((item) => item.inUse);
  return { refreshTaskId: `image-prune-${Date.now()}` };
}

export function mockNetworks(serverId: string) {
  if (!networks[serverId]) networks[serverId] = defaultNetworks(serverId);
  return networks[serverId];
}

export function mockVolumes(serverId: string) {
  if (!volumes[serverId]) volumes[serverId] = defaultVolumes(serverId);
  return volumes[serverId];
}

export function mockDeleteVolume(serverId: string, name: string) {
  const item = volumes[serverId]?.find((candidate) => candidate.name === name);
  if (!item) return 'missing';
  if (item.inUse) return 'in_use';
  volumes[serverId] = volumes[serverId].filter((candidate) => candidate.name !== name);
  return 'ok';
}

export function mockPruneVolumes(serverId: string) {
  if (!volumes[serverId]) return null;
  volumes[serverId] = volumes[serverId].filter((item) => item.inUse);
  return { refreshTaskId: `volume-prune-${Date.now()}` };
}

function container(id: string, name: string, image: string, state: string, managed: boolean): ContainerDto {
  return {
    id,
    names: [`/${name}`],
    image,
    imageId: `sha256:${id}`,
    command: 'docker-entrypoint.sh',
    created: 1784610000,
    state,
    status: state === 'running' ? 'Up 3 hours' : 'Exited (0) 2 days ago',
    ports: [{ privatePort: 80, publicPort: name.includes('nginx') ? 80 : 0, type: 'tcp' }],
    labels: managed ? { 'panel.application.managed': 'true', 'panel.application.id': 'app-storefront', 'panel.application.instance.id': id } : {},
    mounts: [],
    managed,
    applicationId: managed ? 'app-storefront' : undefined,
    instanceId: managed ? id : undefined,
  };
}

function image(id: string, reference: string, updateAvailable: boolean, inUse: boolean, applicationIds: string[]): ImageDto {
  return {
    id,
    repoTags: [reference],
    repoDigests: [`${reference}@sha256:${id}`],
    created: 1784610000,
    size: 256 * 1024 * 1024,
    containers: inUse ? 1 : 0,
    reference,
    localDigest: `sha256:${id}`,
    latestDigest: updateAvailable ? `sha256:${id}-new` : `sha256:${id}`,
    checkable: true,
    updateAvailable,
    checkedAt: now,
    inUse,
    applicationIds,
    upgradeable: applicationIds.length > 0,
  };
}

function defaultPackages(serverId: string): PackageUpdateList {
  const count = serverId.includes('legacy') ? 9 : serverId.includes('media') ? 6 : serverId.includes('staging') ? 4 : 2;
  return {
    serverId,
    lastRefreshedAt: now,
    refreshing: false,
    updates: Array.from({ length: count }, (_, index) => ({
      name: `${serverId.replace(/^srv-/, '')}-package-${index + 1}`,
      installedVersion: `1.${index}.0`,
      candidateVersion: `1.${index}.1`,
      source: index % 2 ? 'debian' : 'debian-security',
    })),
  };
}

function defaultContainers(serverId: string): ContainerDto[] {
  if (serverId.includes('media')) return [
    container(`ctr-${serverId}-transcoder`, 'transcoder-worker', 'ghcr.io/example/transcoder:2.4.0', 'running', false),
    container(`ctr-${serverId}-failed-job`, 'failed-transcode-job', 'ghcr.io/example/transcoder:2.3.1', 'exited', false),
  ];
  if (serverId.includes('staging')) return [
    container(`ctr-${serverId}-preview`, 'preview-service', 'nginx:1.28-alpine', 'running', false),
  ];
  return [
    container(`ctr-${serverId}-agent`, 'panel-agent-helper', 'ghcr.io/example/panel-agent-helper:0.9.7', 'running', false),
    container(`ctr-${serverId}-old`, 'old-maintenance-shell', 'debian:12-slim', 'exited', false),
  ];
}

function defaultImages(serverId: string): ImageList {
  return {
    serverId,
    lastRefreshedAt: now,
    refreshing: false,
    items: [
      image(`img-${serverId}-agent`, 'ghcr.io/example/panel-agent-helper:0.9.7', false, true, []),
      image(`img-${serverId}-base`, 'debian:12-slim', true, true, []),
      image(`img-${serverId}-unused`, 'alpine:3.20', false, false, []),
    ],
  };
}

function defaultNetworks(serverId: string): NetworkDto[] {
  return [
    { id: `net-${serverId}-apps`, name: `${serverId.replace(/^srv-/, '')}-apps`, driver: 'bridge', scope: 'local', internal: false, labels: { 'panel.managed': 'true' } },
    { id: `net-${serverId}-private`, name: `${serverId.replace(/^srv-/, '')}-private`, driver: 'bridge', scope: 'local', internal: true, labels: {} },
  ];
}

function defaultVolumes(serverId: string): VolumeDto[] {
  return [
    { name: `${serverId.replace(/^srv-/, '')}-data`, driver: 'local', mountpoint: `/var/lib/docker/volumes/${serverId}-data/_data`, labels: {}, inUse: true, containerCount: 1, usageData: { size: 4 * 1024 ** 3, refCount: 1 } },
    { name: `${serverId.replace(/^srv-/, '')}-unused-cache`, driver: 'local', mountpoint: `/var/lib/docker/volumes/${serverId}-unused-cache/_data`, labels: {}, inUse: false, containerCount: 0, usageData: { size: 256 * 1024 ** 2, refCount: 0 } },
  ];
}
