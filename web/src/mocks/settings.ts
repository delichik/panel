import type { BackupExportResponse, RestoreConfirmResponse, RestorePreflightResponse, RuntimeSettings, ServerVariableDefinition } from '@/types/settings';

export let mockRuntimeSettings: RuntimeSettings = {
  listenAddress: '0.0.0.0:8080',
  appDatabase: 'data/app.db',
  metricsDatabase: 'data/metrics.db',
  dataRoot: 'data',
  metricsRetentionDays: 14,
  metricsCollectionIntervalSeconds: 60,
  containerReportIntervalSeconds: 5,
  cleanupSchedule: 'daily',
  runtimeEventRetentionDays: 30,
  runtimeEventDetailRetentionDays: 7,
  runtimeEventCleanupSchedule: 'daily',
  tokenExpiration: '1d',
  language: 'zh-CN',
  logLevel: 'info',
  remoteCommandTimeoutSeconds: 45,
  branding: { loginTitle: 'Seamark', loginSubtitle: 'Demo operations control plane' },
  certificates: { email: 'ops@example.com', dnsPropagationDelaySeconds: 30 },
  jwtSecretConfigured: true,
};

export let mockServerVariables: ServerVariableDefinition[] = [
  { name: 'Public address', key: 'PUBLIC_ADDRESS', required: true },
  { name: 'Availability zone', key: 'AVAILABILITY_ZONE', required: false },
  { name: 'Region code', key: 'REGION_CODE', required: false },
  { name: 'Maintenance window', key: 'MAINTENANCE_WINDOW', required: false },
  { name: 'GPU class', key: 'GPU_CLASS', required: false },
  { name: 'Backup tier', key: 'BACKUP_TIER', required: false },
];

export function saveRuntime(input: Partial<RuntimeSettings>) {
  if (input.logLevel === 'debug' && input.remoteCommandTimeoutSeconds === 13) {
    throw new Error('Runtime settings changed on the server. Refresh and apply this section again.');
  }
  mockRuntimeSettings = { ...mockRuntimeSettings, ...input, branding: { ...mockRuntimeSettings.branding, ...input.branding }, certificates: { ...mockRuntimeSettings.certificates, ...input.certificates } };
  return mockRuntimeSettings;
}

export function saveServerVariables(definitions: ServerVariableDefinition[]) {
  const keys = new Set<string>();
  for (const definition of definitions) {
    if (keys.has(definition.key)) throw new Error('Server variable keys must be unique.');
    keys.add(definition.key);
  }
  mockServerVariables = definitions;
  return mockServerVariables;
}

export function startExport(): BackupExportResponse {
  return { exportId: `export-${Date.now()}`, restartSupported: true };
}

export function restorePreflight(): RestorePreflightResponse {
  return {
    encrypted: true,
    passwordRequired: true,
    manifest: { formatVersion: 1, panelVersion: 'alpha', createdAt: '2026-08-01T07:40:00.000Z', encrypted: true, includes: ['app', 'logs', 'metrics'], files: [{ path: 'app.db', size: 42000, sha256: 'abc123' }] },
  };
}

export function confirmRestore(): RestoreConfirmResponse {
  return { pending: true, restartSupported: true };
}
