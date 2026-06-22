const now = '2026-06-22T08:00:00Z';
export const mockEarlier = '2026-06-22T07:40:00Z';
const earlier = mockEarlier;

export const initialMockState = {
  authenticated: true,
  runtimeSettings: {
    listenAddress: '127.0.0.1:8080', appDatabase: 'data/panel.db', metricsDatabase: 'data/metrics.db', dataRoot: 'data',
    metricsRetentionDays: 30, metricsCollectionIntervalSeconds: 60, cleanupSchedule: '0 3 * * *', tokenExpiration: '5d',
    language: 'zh-CN', logLevel: 'info', remoteCommandTimeoutSeconds: 300,
    branding: { loginTitle: 'Panel Mock', loginSubtitle: 'Frontend test mode' },
    certificates: { email: 'ops@example.test', dnsPropagationDelaySeconds: 30 }, jwtSecretConfigured: true,
  },
  servers: [
    { id: 'srv-edge', name: '新加坡边缘节点', host: '10.20.0.11', port: 22, sshUsername: 'deploy', credentialId: 'cred-deploy', dockerHost: 'unix:///var/run/docker.sock', traits: { region: 'sg', role: 'edge' }, notes: '主要入口节点', os: { id: 'ubuntu', versionId: '24.04', prettyName: 'Ubuntu 24.04 LTS', supported: true }, architecture: { os: 'linux', arch: 'amd64', rawMachine: 'x86_64' }, sudo: { passwordless: true, lastCheckedAt: now }, privilege: { mode: 'passwordless_sudo', privileged: true, lastCheckedAt: now }, reachable: true, loadAverage: '0.42 0.37 0.31', lastCheckedAt: now, createdAt: earlier, updatedAt: now },
    { id: 'srv-db', name: '东京数据节点', host: '10.20.0.21', port: 22, sshUsername: 'root', credentialId: 'cred-root', dockerHost: 'unix:///var/run/docker.sock', traits: { region: 'jp', role: 'database' }, notes: '数据库与备份', os: { id: 'debian', versionId: '12', prettyName: 'Debian GNU/Linux 12', supported: true }, architecture: { os: 'linux', arch: 'arm64', rawMachine: 'aarch64' }, sudo: { passwordless: true, lastCheckedAt: now }, privilege: { mode: 'root', privileged: true, lastCheckedAt: now }, reachable: true, loadAverage: '1.12 0.98 0.83', lastCheckedAt: now, createdAt: earlier, updatedAt: now },
    { id: 'srv-lab', name: '离线实验节点', host: '10.20.0.99', port: 2222, sshUsername: 'tester', credentialId: 'cred-deploy', dockerHost: 'unix:///var/run/docker.sock', traits: { region: 'lab' }, notes: '用于展示异常状态', os: null, architecture: null, sudo: null, privilege: null, reachable: false, loadAverage: null, lastCheckedAt: earlier, lastError: 'connection timed out', createdAt: earlier, updatedAt: earlier },
  ],
  credentials: [
    { id: 'cred-deploy', name: '部署 SSH 密钥', type: 'private_key', username: 'deploy', createdAt: earlier, updatedAt: now },
    { id: 'cred-root', name: '运维 Root 凭据', type: 'password', username: 'root', createdAt: earlier, updatedAt: now },
  ],
  ufw: {
    'srv-edge': { serverId: 'srv-edge', supported: true, installed: true, active: true, status: 'active', defaultPolicy: 'deny (incoming), allow (outgoing)', rules: [{ number: 1, to: '22/tcp', action: 'ALLOW IN', from: '10.0.0.0/8' }, { number: 2, to: '80,443/tcp', action: 'ALLOW IN', from: 'Anywhere' }] },
    'srv-db': { serverId: 'srv-db', supported: true, installed: true, active: false, status: 'inactive', defaultPolicy: 'deny (incoming), allow (outgoing)', rules: [] },
  } as Record<string, { serverId: string; supported: boolean; installed: boolean; active: boolean; status: string; defaultPolicy: string; rules: Array<{ number: number; to: string; action: string; from: string }> }>,
  packageUpdates: [
    { name: 'docker-ce', installedVersion: '27.5.0', candidateVersion: '28.1.1', source: 'Docker CE Stable' },
    { name: 'openssl', installedVersion: '3.0.13-1', candidateVersion: '3.0.14-1', source: 'Ubuntu Security' },
    { name: 'linux-generic', installedVersion: '6.8.0.55', candidateVersion: '6.8.0.60', source: 'Ubuntu Updates' },
  ],
  applications: [
    { id: 'app-web', name: 'website', enabled: true, specYaml: 'name: website\nimage: nginx:1.27\nports:\n  - containerPort: 80\n', variables: { DOMAIN: 'www.example.test' }, deploymentMode: 'selected', deploymentServers: ['srv-edge'], generation: 4, specHash: 'sha256:web', imageReference: 'nginx:1.27', imageDigest: 'sha256:local-nginx', imageLatestDigest: 'sha256:latest-nginx', imageCheckedAt: now, imageUpdateAvailable: true, imageUpdateTargets: [{ serverId: 'srv-edge', serverName: '新加坡边缘节点', reference: 'nginx:1.27', localDigest: 'sha256:local-nginx', latestDigest: 'sha256:latest-nginx', updateAvailable: true, checkedAt: now }], jobId: 'website', namespace: 'default', runtimeStatus: 'running', allocationCount: 1, createdAt: earlier, updatedAt: now },
    { id: 'app-worker', name: 'queue-worker', enabled: false, specYaml: 'name: queue-worker\nimage: alpine:3.20\ncommand: ["sleep", "3600"]\n', variables: {}, deploymentMode: 'all', deploymentServers: [], generation: 2, specHash: 'sha256:worker', imageReference: 'alpine:3.20', imageDigest: 'sha256:alpine', imageLatestDigest: 'sha256:alpine', imageCheckedAt: now, imageUpdateAvailable: false, jobId: 'queue-worker', namespace: 'default', runtimeStatus: 'stopped', allocationCount: 0, createdAt: earlier, updatedAt: now },
  ],
  containers: [
    { id: 'ctr-web', names: ['/website'], image: 'nginx:1.27', imageId: 'sha256:local-nginx', command: 'nginx -g daemon off;', created: 1750575600, state: 'running', status: 'Up 3 hours', ports: [{ privatePort: 80, publicPort: 8080, type: 'tcp', ip: '0.0.0.0' }], labels: { 'panel.application': 'website' }, managed: true, applicationId: 'app-web', instanceId: 'app-web-srv-edge' },
    { id: 'ctr-cache', names: ['/redis-cache'], image: 'redis:7.4', imageId: 'sha256:redis', command: 'redis-server', created: 1750560000, state: 'exited', status: 'Exited (0) 20 minutes ago', ports: [{ privatePort: 6379, type: 'tcp' }], labels: {}, managed: false },
  ],
  images: [
    { id: 'sha256:local-nginx', repoTags: ['nginx:1.27'], repoDigests: ['nginx@sha256:local-nginx'], created: 1750000000, size: 192000000, reference: 'nginx:1.27', localDigest: 'sha256:local-nginx', latestDigest: 'sha256:latest-nginx', checkable: true, updateAvailable: true, checkedAt: now, inUse: true, applicationIds: ['app-web'], upgradeable: true },
    { id: 'sha256:redis', repoTags: ['redis:7.4'], repoDigests: [], created: 1749000000, size: 123000000, reference: 'redis:7.4', localDigest: 'sha256:redis', latestDigest: 'sha256:redis', checkable: true, updateAvailable: false, checkedAt: now, inUse: true, applicationIds: [], upgradeable: false },
    { id: 'sha256:dangling', repoTags: [], repoDigests: [], created: 1740000000, size: 84000000, reference: '<none>', checkable: false, updateAvailable: false, inUse: false, applicationIds: [], upgradeable: false },
  ],
  networks: [{ id: 'net-bridge', name: 'bridge', driver: 'bridge', scope: 'local', created: earlier, internal: false }, { id: 'net-panel', name: 'panel-apps', driver: 'bridge', scope: 'local', created: earlier, internal: true }],
  volumes: [{ name: 'website-data', driver: 'local', mountpoint: '/var/lib/docker/volumes/website-data/_data', createdAt: earlier, inUse: true, containerCount: 1 }, { name: 'old-backup', driver: 'local', mountpoint: '/var/lib/docker/volumes/old-backup/_data', createdAt: earlier, inUse: false, containerCount: 0 }],
  domains: [{ id: 'domain-example', name: 'example.test', provider: 'cloudflare', createdAt: earlier, updatedAt: now }, { id: 'domain-internal', name: 'internal.example.test', provider: 'cloudflare', createdAt: earlier, updatedAt: now }],
  records: {
    'domain-example': [{ id: 'rec-a', name: '@', type: 'A', value: '203.0.113.10', ttl: 300, proxied: true }, { id: 'rec-www', name: 'www', type: 'CNAME', value: 'example.test', ttl: 300, proxied: true }, { id: 'rec-mx', name: '@', type: 'MX', value: '10 mail.example.test', ttl: 3600, proxied: false }],
    'domain-internal': [{ id: 'rec-api', name: 'api', type: 'A', value: '10.20.0.11', ttl: 60, proxied: false }],
  } as Record<string, Array<Record<string, unknown>>>,
  certificates: [{ id: 'cert-web', name: '网站通配符证书', domainId: 'domain-example', domain: 'example.test', prefix: '*', scope: 'wildcard', domains: ['example.test', '*.example.test'], variableName: 'WEB_TLS', certificatePath: '/data/certificates/web.crt', privateKeyPath: '/data/certificates/web.key', issuer: "Let's Encrypt", status: 'issued', autoRenew: true, nextRenewAt: '2026-08-20T00:00:00Z', notBefore: '2026-05-22T00:00:00Z', notAfter: '2026-08-22T00:00:00Z', createdAt: earlier, updatedAt: now }],
  selfSigned: [{ id: 'ca-dev', kind: 'ca', name: '开发环境 CA', commonName: 'Panel Development CA', dnsNames: [], ipAddresses: [], fingerprint: 'SHA256:AA:BB:CC', notBefore: '2026-01-01T00:00:00Z', notAfter: '2031-01-01T00:00:00Z', createdAt: earlier, updatedAt: now }, { id: 'leaf-api', parentCaId: 'ca-dev', kind: 'leaf', name: '内部 API 证书', commonName: 'api.internal.example.test', dnsNames: ['api.internal.example.test'], ipAddresses: ['10.20.0.11'], fingerprint: 'SHA256:DD:EE:FF', notBefore: '2026-06-01T00:00:00Z', notAfter: '2027-06-01T00:00:00Z', createdAt: earlier, updatedAt: now }],
  keyAssets: [
    { id: 'key-ca', type: 'ca_certificate', name: '用户根 CA', algorithm: 'ed25519', commonName: 'Example User CA', dnsNames: [], ipAddresses: [], fingerprint: 'SHA256:11:22:33', notBefore: '2026-01-01T00:00:00Z', notAfter: '2031-01-01T00:00:00Z', hasCertificate: true, hasPrivateKey: true, hasPublicKey: true, downloadKinds: ['certificate', 'private_key', 'public_key'], childCount: 1, referenceCount: 0, references: [], canReissue: false, canRegenerate: true, canDelete: true, createdAt: earlier, updatedAt: now },
    { id: 'key-tls', type: 'tls_certificate', name: '内部服务 TLS', parentAssetId: 'key-ca', algorithm: 'ed25519', commonName: 'service.example.test', dnsNames: ['service.example.test'], ipAddresses: ['10.20.0.11'], fingerprint: 'SHA256:44:55:66', notBefore: '2026-06-01T00:00:00Z', notAfter: '2027-06-01T00:00:00Z', hasCertificate: true, hasPrivateKey: true, hasPublicKey: true, downloadKinds: ['certificate', 'private_key'], childCount: 0, referenceCount: 1, references: [{ resourceType: 'application', resourceId: 'app-web', resourceName: 'website', relation: 'tls' }], canReissue: true, canRegenerate: false, canDelete: true, createdAt: earlier, updatedAt: now },
    { id: 'key-ssh', type: 'ssh_key_pair', name: '自动化 SSH 密钥', algorithm: 'ed25519', dnsNames: [], ipAddresses: [], fingerprint: 'SHA256:77:88:99', hasCertificate: false, hasPrivateKey: true, hasPublicKey: true, downloadKinds: ['private_key', 'ssh_public_key'], childCount: 0, referenceCount: 1, references: [{ resourceType: 'credential', resourceId: 'cred-deploy', resourceName: '部署 SSH 密钥', relation: 'private_key' }], canReissue: false, canRegenerate: true, canDelete: false, createdAt: earlier, updatedAt: now },
  ],
  systemCertificates: [{ id: 'agent-ca', type: 'ca_certificate', name: 'Panel Agent CA', commonName: 'Panel Agent CA', fingerprint: 'SHA256:AGENT:CA', notBefore: '2026-01-01T00:00:00Z', notAfter: '2036-01-01T00:00:00Z', status: 'valid', builtIn: true, canReset: true }, { id: 'agent-client', type: 'tls_certificate', name: 'Panel Agent Client', commonName: 'panel', fingerprint: 'SHA256:AGENT:CLIENT', notBefore: '2026-01-01T00:00:00Z', notAfter: '2027-01-01T00:00:00Z', status: 'valid', builtIn: true, canReset: true }, { id: 'agent-server-srv-edge', type: 'tls_certificate', name: '新加坡边缘节点 Agent', commonName: 'srv-edge', fingerprint: 'SHA256:AGENT:EDGE', notBefore: '2026-01-01T00:00:00Z', notAfter: '2027-01-01T00:00:00Z', serverId: 'srv-edge', serverName: '新加坡边缘节点', status: 'valid', builtIn: true, canReset: true }],
  tasks: [
    { id: 'task-running', operationId: 'op-deploy', type: 'application.deploy', serverId: 'srv-edge', resourceType: 'application', resourceId: 'app-web', status: 'running', stage: 'running', percentage: 65, summary: '正在部署 website', retryCount: 0, maxRetries: 2, createdAt: earlier, startedAt: '2026-06-22T07:58:00Z', finishedAt: null, allowRunNow: false, allowRetry: false },
    { id: 'task-complete', type: 'packages.refresh', serverId: 'srv-edge', status: 'completed', stage: 'finalizing', percentage: 100, summary: '已刷新软件包索引', retryCount: 0, maxRetries: 2, createdAt: earlier, startedAt: earlier, finishedAt: now, allowRunNow: true, allowRetry: false },
    { id: 'task-failed', type: 'server.probe', serverId: 'srv-lab', status: 'failed_retryable', stage: 'connecting', percentage: 10, summary: '实验节点连接失败', error: 'connection timed out', retryCount: 1, maxRetries: 3, createdAt: earlier, startedAt: earlier, finishedAt: now, allowRunNow: false, allowRetry: true },
    { id: 'task-scheduled', type: 'system.cleanup', serverId: null, status: 'scheduled', stage: 'preparing', percentage: null, summary: '等待执行定期清理', retryCount: 0, maxRetries: 1, nextRunAt: '2026-06-23T03:00:00Z', createdAt: earlier, startedAt: null, finishedAt: null, allowRunNow: true, allowRetry: false },
  ],
};

export type MockState = typeof initialMockState;

export function createMockState(): MockState {
  return structuredClone(initialMockState);
}
