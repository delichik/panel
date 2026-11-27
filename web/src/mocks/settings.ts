import type {
  BackupExportResponse,
  RestoreConfirmResponse,
  RestorePreflightResponse,
  RuntimeAgentUpdate,
  RuntimeAutoUpdateRunResponse,
  RuntimeAutoUpdateSettings,
  RuntimeAutoUpdateUpdate,
  RuntimeBrandingUpdate,
  RuntimeCertificatesUpdate,
  RuntimeGeneralUpdate,
  RuntimeImageRegistryUpdate,
  RuntimeSecurityUpdate,
  RuntimeSettings,
  ServerVariableDefinition,
} from '@/types/settings';
import { isImageRegistryScheme, normalizeImageRegistryEndpoint } from '@/utils/imageRegistry';

/** 镜像仓库 mock 校验失败：由 browser.ts 转换成契约错误码响应。 */
export class MockImageRegistryError extends Error {
  constructor(readonly code: string, message: string) {
    super(message);
    this.name = 'MockImageRegistryError';
  }
}

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
  autoUpdate: {
    appsEnabled: false,
    packagesEnabled: false,
    packagesMode: 'security',
    scheduleTime: '04:00',
    lastRunAt: '2026-08-01T04:00:00.000Z',
    lastAutoRunAt: '2026-08-01T04:00:00.000Z',
    lastRunStatus: 'completed',
    lastRunSummary: 'Mock coordinator completed: 1 application updated, 0 software packages upgraded.',
    lastRunTaskId: 'task-auto-update-1',
  },
  imageRegistry: { endpoint: '', scheme: 'https' },
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

/** 自动更新分区：只合入四个可编辑字段，保留只读 lastRun 投影。 */
export function saveRuntimeAutoUpdate(input: RuntimeAutoUpdateUpdate): RuntimeSettings {
  const current: RuntimeAutoUpdateSettings = mockRuntimeSettings.autoUpdate ?? {
    appsEnabled: false,
    packagesEnabled: false,
    packagesMode: 'security',
    scheduleTime: '04:00',
  };
  mockRuntimeSettings = { ...mockRuntimeSettings, autoUpdate: { ...current, ...input } };
  return mockRuntimeSettings;
}

/**
 * 镜像仓库分区：指针语义，省略字段保留当前值，显式空串 endpoint 表示官方仓库。
 * endpoint 必须是裸 host[:port]，scheme 只能是 http/https；失败整笔不生效。
 */
export function saveRuntimeImageRegistry(input: RuntimeImageRegistryUpdate): RuntimeSettings {
  const current = mockRuntimeSettings.imageRegistry ?? { endpoint: '', scheme: 'https' };
  let endpoint = current.endpoint;
  let scheme = current.scheme;
  if (input.endpoint !== undefined) {
    const normalized = normalizeImageRegistryEndpoint(input.endpoint);
    if (normalized === null) {
      throw new MockImageRegistryError('invalid_image_registry_endpoint', 'Image registry endpoint must be a bare host or host:port with port 1-65535.');
    }
    endpoint = normalized;
  }
  if (input.scheme !== undefined) {
    if (!isImageRegistryScheme(input.scheme)) {
      throw new MockImageRegistryError('invalid_image_registry_scheme', 'Image registry scheme must be http or https.');
    }
    scheme = input.scheme;
  }
  mockRuntimeSettings = { ...mockRuntimeSettings, imageRegistry: { endpoint, scheme } };
  return mockRuntimeSettings;
}

/**
 * POST /settings/runtime/auto-update/run：已有 running 任务时幂等返回其 taskId，
 * 否则标记 running 并返回新的 taskId（真实任务由协调器异步结束）。
 */
export function runRuntimeAutoUpdate(): RuntimeAutoUpdateRunResponse {
  const current: RuntimeAutoUpdateSettings = mockRuntimeSettings.autoUpdate ?? {
    appsEnabled: false,
    packagesEnabled: false,
    packagesMode: 'security',
    scheduleTime: '04:00',
  };
  if (current.lastRunStatus === 'running' && current.lastRunTaskId) {
    return { taskId: current.lastRunTaskId };
  }
  const taskId = `task-auto-update-manual-${Date.now()}`;
  mockRuntimeSettings = {
    ...mockRuntimeSettings,
    autoUpdate: {
      ...current,
      lastRunAt: new Date().toISOString(),
      lastRunStatus: 'running',
      lastRunSummary: '',
      lastRunTaskId: taskId,
    },
  };
  return { taskId };
}

/**
 * Mock 没有真实协调器：手动运行启动约 8 秒后写回完成状态与摘要，
 * 让本地 Mock 模式也能验证“运行中 → 完成 → 显示摘要”的刷新链路。
 */
const mockAutoUpdateRunDurationMs = 8000;

export function settleMockRuntimeAutoUpdate(now = Date.now()) {
  const current = mockRuntimeSettings.autoUpdate;
  if (!current || current.lastRunStatus !== 'running') return mockRuntimeSettings;
  const startedAt = current.lastRunAt ? Date.parse(current.lastRunAt) : Number.NaN;
  if (Number.isNaN(startedAt) || now - startedAt < mockAutoUpdateRunDurationMs) return mockRuntimeSettings;
  mockRuntimeSettings = {
    ...mockRuntimeSettings,
    autoUpdate: {
      ...current,
      lastRunStatus: 'completed',
      lastRunSummary: 'Mock coordinator completed: checked 2 applications and upgraded 1 software package.',
    },
  };
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
