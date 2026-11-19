import { apiClient, fetchJson } from './client';
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

async function multipart<T>(path: string, form: FormData): Promise<T> {
  return fetchJson<T>(`/api/v1${path}`, { method: 'POST', body: form });
}

export const settingsApi = {
  publicBranding() {
    return apiClient.get<RuntimeSettings['branding']>('/settings/public-branding', { skipAuth: true });
  },
  runtime() {
    return apiClient.get<RuntimeSettings>('/settings/runtime');
  },
  /** 常规/运行时分区：PUT /settings/runtime/general。 */
  updateGeneral(input: RuntimeGeneralUpdate) {
    return apiClient.put<RuntimeSettings>('/settings/runtime/general', input);
  },
  /** 安全分区：PUT /settings/runtime/security。 */
  updateSecurity(input: RuntimeSecurityUpdate) {
    return apiClient.put<RuntimeSettings>('/settings/runtime/security', input);
  },
  /** 证书/Panel HTTPS 分区：PUT /settings/runtime/certificates。 */
  updateCertificates(input: RuntimeCertificatesUpdate) {
    return apiClient.put<RuntimeSettings>('/settings/runtime/certificates', input);
  },
  /** Agent 投递分区：PUT /settings/runtime/agent。 */
  updateAgent(input: RuntimeAgentUpdate) {
    return apiClient.put<RuntimeSettings>('/settings/runtime/agent', input);
  },
  /** 登录品牌分区：PUT /settings/runtime/branding。 */
  updateBranding(input: RuntimeBrandingUpdate) {
    return apiClient.put<RuntimeSettings>('/settings/runtime/branding', input);
  },
  /**
   * 顶栏语言切换：先读取完整 runtime，再只提交 general 分区字段。
   * 语言属于 general 子资源，不再回传整套 runtime。
   */
  async updateLanguage(language: 'en' | 'zh-CN') {
    const current = await apiClient.get<RuntimeSettings>('/settings/runtime');
    const input: RuntimeGeneralUpdate = {
      containerReportIntervalSeconds: current.containerReportIntervalSeconds,
      language,
      logLevel: current.logLevel,
      reconcileTraceEnabled: current.reconcileTraceEnabled,
    };
    return apiClient.put<RuntimeSettings>('/settings/runtime/general', input);
  },
  /** 请求协调（202 语义）：返回的是本次协调开始时的容器状态，不代表执行成功。 */
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
