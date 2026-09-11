import { apiClient, fetchJson } from './client';
import type { BackupExportResponse, RestoreConfirmResponse, RestorePreflightResponse, RuntimeSettings, RuntimeUpdate, ServerVariableDefinition } from '@/types/settings';

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
  updateRuntime(input: RuntimeUpdate) {
    return apiClient.put<RuntimeSettings>('/settings/runtime', input);
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