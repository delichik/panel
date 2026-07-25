import type { CredentialDto } from '@/types/credentials';
import type { OperationAccepted, ServerDto, ServerProbeResult, ServerSaveInput } from '@/types/servers';
import type { ServerMetricsSeries, AgentCertificateBundle } from '@/api/servers';

export let mockCredentials: CredentialDto[] = [
  { id: 'cred-root-key', name: 'Root deploy key', type: 'private_key', username: 'root', createdAt: '2026-07-20T10:00:00.000Z', updatedAt: '2026-07-20T10:00:00.000Z' },
  { id: 'cred-ops-password', name: 'Ops password fallback', type: 'password', username: 'ops', createdAt: '2026-07-19T08:00:00.000Z', updatedAt: '2026-07-19T08:00:00.000Z' },
  { id: 'cred-empty-long', name: 'Long audit credential with rotation note', type: 'private_key', username: 'deploy-audit', createdAt: '2026-07-18T08:00:00.000Z', updatedAt: '2026-07-18T08:00:00.000Z' },
  { id: 'cred-readonly', name: 'Read-only breakglass', type: 'password', username: 'readonly', createdAt: '2026-07-17T08:00:00.000Z', updatedAt: '2026-07-20T08:00:00.000Z' },
  { id: 'cred-ci-runner', name: 'CI runner deploy key', type: 'private_key', username: 'deploy', createdAt: '2026-07-16T08:00:00.000Z', updatedAt: '2026-07-20T09:00:00.000Z' },
  { id: 'cred-legacy-sudo', name: 'Legacy sudo password', type: 'password', username: 'ubuntu', createdAt: '2026-07-14T08:00:00.000Z', updatedAt: '2026-07-18T09:00:00.000Z' },
  { id: 'cred-db-admin', name: 'Database admin key', type: 'private_key', username: 'postgres-admin', createdAt: '2026-07-13T08:00:00.000Z', updatedAt: '2026-07-20T09:30:00.000Z' },
  { id: 'cred-cache-ops', name: 'Cache operations password', type: 'password', username: 'cacheops', createdAt: '2026-07-12T08:00:00.000Z', updatedAt: '2026-07-19T09:00:00.000Z' },
  { id: 'cred-observability', name: 'Observability collector key', type: 'private_key', username: 'collector', createdAt: '2026-07-11T08:00:00.000Z', updatedAt: '2026-07-18T09:00:00.000Z' },
  { id: 'cred-staging', name: 'Staging shared password', type: 'password', username: 'staging', createdAt: '2026-07-10T08:00:00.000Z', updatedAt: '2026-07-17T09:00:00.000Z' },
  { id: 'cred-unused-imported', name: 'Imported unused key pending review', type: 'private_key', username: 'imported', createdAt: '2026-07-09T08:00:00.000Z', updatedAt: '2026-07-16T09:00:00.000Z' },
  { id: 'cred-rotating-long-name', name: 'Rotating credential with intentionally long display name', type: 'password', username: 'rotation-user', createdAt: '2026-07-08T08:00:00.000Z', updatedAt: '2026-07-15T09:00:00.000Z' },
];

export let mockServers: ServerDto[] = [
  server('srv-edge-sgp', 'edge-sgp-01', '10.8.0.12', 'cred-root-key', true, {
    'agent.enabled': 'true',
    'agent.status': 'compatible',
    'agent.version': '0.9.7',
    'sys.ufw_supported': 'true',
    'sys.ufw_installed': 'true',
    'sys.ufw_active': 'true',
    'sys.memory_total_mb': '16384',
    'sys.disk_total_gb': '240',
    'sys.network_interfaces': 'eth0|inet|10.8.0.12, eth0|inet6|fd00::12',
  }),
  server('srv-core-fra', 'core-fra-02', '10.12.4.22', 'cred-root-key', true, {
    'agent.enabled': 'true',
    'agent.status': 'unavailable',
    'agent.last_error': 'agent health timeout after 5s',
    'sys.ufw_supported': 'true',
    'sys.ufw_installed': 'false',
    'sys.memory_total_mb': '8192',
    'sys.disk_total_gb': '120',
    'mock.package_updates': '14',
  }),
  server('srv-lab-dead', 'lab-unreachable-with-a-very-long-hostname-for-layout-testing', '198.51.100.31', 'cred-ops-password', false, {
    'agent.enabled': 'false',
    'sys.ufw_supported': 'false',
    'mock.package_updates': '0',
  }, 'SSH dial timeout. Last known note is intentionally long to verify wrapped detail content stays inside the panel.'),
  server('srv-api-hkg', 'api-hkg-01', '10.22.0.41', 'cred-ci-runner', true, {
    'agent.enabled': 'true',
    'agent.status': 'compatible',
    'agent.version': '0.9.7',
    'sys.ufw_supported': 'true',
    'sys.ufw_installed': 'true',
    'sys.ufw_active': 'true',
    'sys.memory_total_mb': '32768',
    'sys.disk_total_gb': '480',
    'mock.package_updates': '3',
  }),
  server('srv-api-hkg-02', 'api-hkg-02-canary', '10.22.0.42', 'cred-ci-runner', true, {
    'agent.enabled': 'true',
    'agent.status': 'compatible',
    'agent.version': '0.9.8-canary',
    'sys.ufw_supported': 'true',
    'sys.ufw_installed': 'true',
    'sys.ufw_active': 'true',
    'sys.memory_total_mb': '32768',
    'sys.disk_total_gb': '480',
    'mock.package_updates': '1',
  }),
  server('srv-worker-nrt', 'worker-nrt-queue-a', '10.31.4.9', 'cred-ci-runner', true, {
    'agent.enabled': 'true',
    'agent.status': 'compatible',
    'sys.ufw_supported': 'true',
    'sys.ufw_installed': 'false',
    'sys.memory_total_mb': '65536',
    'sys.disk_total_gb': '960',
    'mock.package_updates': '22',
  }),
  server('srv-worker-nrt-02', 'worker-nrt-queue-b', '10.31.4.10', 'cred-ci-runner', true, {
    'agent.enabled': 'true',
    'agent.status': 'unavailable',
    'agent.last_error': 'agent socket refused during rolling restart',
    'sys.ufw_supported': 'true',
    'sys.ufw_installed': 'true',
    'mock.package_updates': '8',
  }),
  server('srv-db-fra', 'db-fra-primary', '10.12.9.11', 'cred-db-admin', true, {
    'agent.enabled': 'true',
    'agent.status': 'compatible',
    'sys.ufw_supported': 'true',
    'sys.ufw_installed': 'true',
    'sys.ufw_active': 'true',
    'sys.memory_total_mb': '131072',
    'sys.disk_total_gb': '2048',
    'mock.package_updates': '0',
  }),
  server('srv-db-fra-ro', 'db-fra-readonly-replica-with-long-maintenance-note', '10.12.9.12', 'cred-readonly', true, {
    'agent.enabled': 'true',
    'agent.status': 'compatible',
    'sys.ufw_supported': 'true',
    'sys.ufw_installed': 'true',
    'mock.package_updates': '5',
  }),
  server('srv-cache-sfo', 'cache-sfo-edge', '10.44.1.8', 'cred-cache-ops', true, {
    'agent.enabled': 'false',
    'sys.ufw_supported': 'true',
    'sys.ufw_installed': 'false',
    'mock.package_updates': '11',
  }),
  server('srv-media-syd', 'media-syd-transcode', '10.52.3.19', 'cred-ci-runner', true, {
    'agent.enabled': 'true',
    'agent.status': 'undeployable',
    'agent.last_error': 'unsupported cgroup layout detected',
    'sys.ufw_supported': 'true',
    'mock.package_updates': '17',
  }),
  server('srv-batch-iad', 'batch-iad-nightly', '10.63.7.31', 'cred-ci-runner', true, {
    'agent.enabled': 'true',
    'agent.status': 'compatible',
    'sys.ufw_supported': 'true',
    'sys.ufw_installed': 'true',
    'mock.package_updates': '0',
  }),
  server('srv-legacy-lon', 'legacy-lon-ubuntu-20', '10.71.2.20', 'cred-legacy-sudo', true, {
    'agent.enabled': 'true',
    'agent.status': 'compatible',
    'sys.ufw_supported': 'true',
    'sys.ufw_installed': 'false',
    'mock.package_updates': '31',
  }),
  server('srv-lab-timeout-2', 'lab-timeout-secondary', '198.51.100.42', 'cred-ops-password', false, {
    'agent.enabled': 'false',
    'sys.ufw_supported': 'false',
    'mock.package_updates': '0',
  }, 'Connection refused after VPN route flap; verify inventory before retrying destructive operations.'),
  server('srv-observability-ams', 'observability-ams', '10.82.8.18', 'cred-observability', true, {
    'agent.enabled': 'true',
    'agent.status': 'compatible',
    'sys.ufw_supported': 'true',
    'sys.ufw_installed': 'true',
    'mock.package_updates': '2',
  }),
  server('srv-staging-mad', 'staging-mad-shared', '10.93.5.44', 'cred-staging', true, {
    'agent.enabled': 'false',
    'sys.ufw_supported': 'true',
    'sys.ufw_installed': 'false',
    'mock.package_updates': '4',
  }),
];

export function createServer(input: ServerSaveInput): ServerDto {
  const item = {
    ...server(`srv-${Date.now()}`, input.name, input.host, input.credentialId, true, input.traits),
    port: input.port,
    sshUsername: input.sshUsername,
    dockerHost: input.dockerHost,
    variables: input.variables,
    notes: input.notes,
    initialTaskId: `task-initial-${Date.now()}`,
  };
  mockServers = [item, ...mockServers];
  return item;
}

export function updateServer(id: string, input: ServerSaveInput): ServerDto | null {
  let saved: ServerDto | null = null;
  mockServers = mockServers.map((item) => {
    if (item.id !== id) return item;
    saved = { ...item, ...input, updatedAt: new Date().toISOString() };
    return saved;
  });
  return saved;
}

export function deleteServer(id: string): boolean {
  const before = mockServers.length;
  mockServers = mockServers.filter((item) => item.id !== id);
  return mockServers.length !== before;
}

export function probeServer(input: ServerSaveInput): ServerProbeResult {
  const unreachable = input.host.includes('198.51.100') || input.host.includes('timeout');
  return {
    reachable: !unreachable,
    passwordlessSudo: !unreachable && input.sshUsername !== 'readonly',
    root: input.sshUsername === 'root',
    privileged: !unreachable && input.sshUsername !== 'readonly',
    privilegeMode: input.sshUsername === 'root' ? 'root' : input.sshUsername === 'readonly' ? 'none' : 'passwordless_sudo',
    os: { id: 'debian', versionId: '13', prettyName: 'Debian GNU/Linux 13', supported: !unreachable },
    architecture: { os: 'linux', arch: 'amd64', rawMachine: 'x86_64' },
    traits: { 'sys.cpu_model': 'Mock vCPU', 'sys.ufw_supported': unreachable ? 'false' : 'true' },
    variables: {},
    error: unreachable ? 'dial tcp: i/o timeout' : '',
  };
}

export function testServer(id: string): ServerDto | null {
  const target = mockServers.find((item) => item.id === id);
  if (!target) return null;
  if (!target.reachable) throw new Error('SSH dial timeout.');
  target.lastCheckedAt = new Date().toISOString();
  return { ...target };
}

export function accepted(prefix: string): OperationAccepted {
  return { taskId: `${prefix}-${Date.now()}` };
}

export function mockServerMetrics(id: string): ServerMetricsSeries | null {
  const serverIndex = mockServers.findIndex((server) => server.id === id);
  if (serverIndex < 0) return null;
  const now = Date.now();
  const points = Array.from({ length: 24 }, (_, index) => new Date(now - (23 - index) * 5 * 60_000).toISOString());
  const seed = serverIndex + 1;
  return {
    range: '1h',
    cpu: points.map((time, index) => ({ time, usagePercent: Math.min(96, 12 + seed * 3 + index * 1.7) })),
    memory: points.map((time, index) => ({ time, usedBytes: (3.5 + seed * 0.7 + index * 0.09) * 1024 ** 3, totalBytes: (8 + seed * 2) * 1024 ** 3 })),
    disk: points.map((time, index) => ({ time, usedBytes: (34 + seed * 7 + index * 0.35) * 1024 ** 3, totalBytes: (120 + seed * 40) * 1024 ** 3 })),
    network: points.map((time, index) => ({ time, rxBytesPerSecond: 64_000 + seed * 19_000 + index * 4_200, txBytesPerSecond: 42_000 + seed * 13_000 + index * 3_100 })),
    load: points.map((time, index) => ({ time, load1: 0.25 + seed * 0.11 + index * 0.018, load5: 0.22 + seed * 0.09 + index * 0.014, load15: 0.2 + seed * 0.07 + index * 0.01 })),
  };
}

export function mockAgentCertificate(id: string): AgentCertificateBundle | null {
  const server = mockServers.find((item) => item.id === id);
  if (!server) return null;
  return {
    ca: '-----BEGIN CERTIFICATE-----\\nMOCK-CA\\n-----END CERTIFICATE-----',
    certificate: '-----BEGIN CERTIFICATE-----\\nMOCK-CERT\\n-----END CERTIFICATE-----',
    privateKey: '-----BEGIN PRIVATE KEY-----\\nMOCK-KEY\\n-----END PRIVATE KEY-----',
    listenAddress: ':9786',
    agentUrl: `https://${server.host}:9786`,
    dockerHost: server.dockerHost || 'unix:///var/run/docker.sock',
  };
}

export function createCredential(input: CredentialDto): CredentialDto {
  mockCredentials = [input, ...mockCredentials];
  return input;
}

export function updateCredential(id: string, input: Omit<CredentialDto, 'id'>): CredentialDto | null {
  let saved: CredentialDto | null = null;
  mockCredentials = mockCredentials.map((item) => {
    if (item.id !== id) return item;
    saved = { ...item, ...input, updatedAt: new Date().toISOString() };
    return saved;
  });
  return saved;
}

export function deleteCredential(id: string): 'deleted' | 'in_use' | 'missing' {
  if (mockServers.some((server) => server.credentialId === id)) return 'in_use';
  const before = mockCredentials.length;
  mockCredentials = mockCredentials.filter((item) => item.id !== id);
  return mockCredentials.length === before ? 'missing' : 'deleted';
}

function server(id: string, name: string, host: string, credentialId: string, reachable: boolean, traits: Record<string, string>, lastError = ''): ServerDto {
  const readonly = credentialId === 'cred-readonly';
  const privileged = reachable && !readonly;
  const packageUpdates = Number(traits['mock.package_updates'] ?? 0);
  return {
    id,
    name,
    host,
    port: 22,
    sshUsername: '',
    credentialId,
    dockerHost: 'unix:///var/run/docker.sock',
    traits,
    variables: {},
    notes: id === 'srv-edge-sgp' ? 'Primary ingress node for Singapore workloads.' : readonly ? 'Read-only credential intentionally blocks privileged task buttons in the demo.' : '',
    os: { id: 'debian', versionId: '13', prettyName: 'Debian GNU/Linux 13', supported: reachable },
    architecture: { os: 'linux', arch: 'amd64', rawMachine: 'x86_64' },
    sudo: { passwordless: privileged },
    privilege: { mode: privileged ? 'passwordless_sudo' : 'none', privileged },
    reachable,
    loadAverage: reachable ? `${(0.34 + packageUpdates / 20).toFixed(2)} ${(0.28 + packageUpdates / 25).toFixed(2)} ${(0.24 + packageUpdates / 30).toFixed(2)}` : '',
    lastCheckedAt: reachable ? '2026-07-21T02:58:00.000Z' : '2026-07-20T22:12:00.000Z',
    lastError,
    createdAt: '2026-07-18T08:00:00.000Z',
    updatedAt: '2026-07-21T03:00:00.000Z',
  };
}
