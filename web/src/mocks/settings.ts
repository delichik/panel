import type {
  BackupExportResponse,
  RestoreConfirmResponse,
  RestorePreflightResponse,
  RuntimeAgentUpdate,
  RuntimeBrandingUpdate,
  RuntimeCertificatesUpdate,
  RuntimeGeneralUpdate,
  RuntimeSecurityUpdate,
  RuntimeSettings,
  ServerVariableDefinition,
} from '@/types/settings';

export let mockRuntimeSettings: RuntimeSettings = {
  listenAddress: '0.0.0.0:8080',
  appDatabase: 'data/app.db',
  metricsDatabase: 'data/metrics.db',
  dataRoot: 'data',
  containerReportIntervalSeconds: 30,
  tokenExpiration: '1d',
  language: 'zh-CN',
  logLevel: 'info',
  remoteCommandTimeoutSeconds: 45,
  branding: { loginTitle: 'Seamark', loginSubtitle: 'Demo operations control plane' },
  certificates: { email: 'ops.com', dnsPropagationDelaySeconds: 30 },
  panel: { domain: 'localhost', tlsCertificateId: '' },
  agent: { downloadBaseUrl: '', downloadVerifyTls: false, transferTimeoutSeconds: 120 },
  reconcileTraceEnabled: false,
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

/**
 * 分区保存 mock：每个函数只合入本分区字段，返回完整 RuntimeSettings，
 * 与真实后端“当前内存快照 + 本分区字段”的语义一致。
 */
export function saveRuntimeGeneral(input: RuntimeGeneralUpdate) {
  if (input.logLevel === 'debug' && input.containerReportIntervalSeconds === 13) {
    throw new Error('Runtime settings changed on the server. Refresh and apply this section again.');
  }
  mockRuntimeSettings = { ...mockRuntimeSettings, ...input };
  return mockRuntimeSettings;
}

export function saveRuntimeSecurity(input: RuntimeSecurityUpdate) {
  mockRuntimeSettings = { ...mockRuntimeSettings, ...input };
  return mockRuntimeSettings;
}

export function saveRuntimeCertificates(input: RuntimeCertificatesUpdate) {
  mockRuntimeSettings = {
    ...mockRuntimeSettings,
    panel: { ...mockRuntimeSettings.panel, ...input.panel },
    certificates: { ...mockRuntimeSettings.certificates, ...input.certificates },
  };
  return mockRuntimeSettings;
}

export function saveRuntimeAgent(input: RuntimeAgentUpdate) {
  mockRuntimeSettings = { ...mockRuntimeSettings, agent: { ...mockRuntimeSettings.agent, ...input } };
  return mockRuntimeSettings;
}

export function saveRuntimeBranding(input: RuntimeBrandingUpdate) {
  mockRuntimeSettings = { ...mockRuntimeSettings, branding: { ...mockRuntimeSettings.branding, ...input } };
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
