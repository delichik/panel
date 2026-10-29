import { apiClient, fetchJson } from './client';
import type { BackupExportResponse, RestoreConfirmResponse, RestorePreflightResponse, RuntimeSettings, RuntimeTailscaleContainerState, RuntimeTailscaleUpdate, RuntimeUpdate, ServerVariableDefinition } from '@/types/settings';

async function multipart<T>(path: string, form: FormData): Promise<T> {
  return fetchJson<T>(`/api/v1${path}`, { method: 'POST', body: form });
}

/** PUT /settings/runtime 是整体替换语义：改一个分区时其余标量仍需按当前值回填。 */
function runtimeScalars(current: RuntimeSettings): RuntimeUpdate {
  return {
    metricsRetentionDays: current.metricsRetentionDays,
    metricsCollectionIntervalSeconds: current.metricsCollectionIntervalSeconds,
    containerReportIntervalSeconds: current.containerReportIntervalSeconds,
    cleanupSchedule: current.cleanupSchedule,
    tokenExpiration: current.tokenExpiration,
    language: current.language,
    logLevel: current.logLevel,
    remoteCommandTimeoutSeconds: current.remoteCommandTimeoutSeconds,
  };
}

/**
 * 语言切换只改 language；tailscale 分组按“缺省即保持”的契约原样保留，
 * 因此这里不发送该分组，避免用本地旧值覆盖后端的密钥与容器状态。
 */
export function runtimeLanguageUpdate(current: RuntimeSettings, language: 'en' | 'zh-CN'): RuntimeUpdate {
  return { ...runtimeScalars(current), language };
}

export interface TailscaleSettingsInput {
  /** 只写字段。空字符串表示保留已存密钥。 */
  authKey?: string;
  /** true 表示删除已存密钥（同时停止容器内 tailscale）。authKey 非空时忽略。 */
  clearAuthKey?: boolean;
  /** 完整替换列表，[] 表示清空。 */
  tags?: string[];
}

/**
 * 组装 tailscale 子对象。清空密钥必须由调用方显式确认（clearAuthKey），
 * 且认证密钥非空时不再发送 clearAuthKey —— 与后端“authKey 优先”的规则一致。
 */
export function tailscaleUpdate(input: TailscaleSettingsInput): RuntimeTailscaleUpdate {
  const update: RuntimeTailscaleUpdate = {};
  const authKey = input.authKey?.trim() ?? '';
  if (authKey) update.authKey = authKey;
  else if (input.clearAuthKey) update.clearAuthKey = true;
  if (input.tags) update.tags = [...input.tags];
  return update;
}

/** 只改 tailscale 分组时的完整请求体。 */
export function runtimeTailscaleUpdate(current: RuntimeSettings, input: TailscaleSettingsInput): RuntimeUpdate {
  return { ...runtimeScalars(current), tailscale: tailscaleUpdate(input) };
}

export const settingsApi = {
  publicBranding() {
    return apiClient.get<RuntimeSettings['branding']>('/settings/public-branding', { skipAuth: true });
  },
  runtime() {
    return apiClient.get<RuntimeSettings>('/settings/runtime');
  },
  updateRuntime(input: RuntimeUpdate) {
    return apiClient.put<RuntimeSettings>('/settings/runtime', input);
  },
  async updateLanguage(language: 'en' | 'zh-CN') {
    const current = await apiClient.get<RuntimeSettings>('/settings/runtime');
    return apiClient.put<RuntimeSettings>('/settings/runtime', runtimeLanguageUpdate(current, language));
  },
  /** 请求协调（202 语义）：返回的是本次协调开始时的容器状态，不代表执行成功。 */
  applyTailscale() {
    return apiClient.post<RuntimeTailscaleContainerState>('/settings/tailscale/apply');
  },
  serverVariables() {
    return apiClient.get<ServerVariableDefinition[]>('/settings/server-variables');
  },
  updateServerVariables(definitions: ServerVariableDefinition[]) {
    return apiClient.put<ServerVariableDefinition[]>('/settings/server-variables', { definitions });
  },
  startBackupExport(input: { encrypt: boolean; password?: string }) {
    return apiClient.post<BackupExportResponse>('/backups/export', input);
  },
  preflightRestore(file: File, password = '') {
    const form = new FormData();
    form.set('file', file);
    if (password) form.set('password', password);
    return multipart<RestorePreflightResponse>('/backups/restore/preflight', form);
  },
  confirmRestore(file: File, password: string, confirmOverwrite: boolean) {
    const form = new FormData();
    form.set('file', file);
    form.set('password', password);
    form.set('confirmOverwrite', String(confirmOverwrite));
    return multipart<RestoreConfirmResponse>('/backups/restore/confirm', form);
  },
};
