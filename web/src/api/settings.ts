import { ApiError, type ApiEnvelope, apiClient, authHeaders } from './client';
import type { BackupExportResponse, RestoreConfirmResponse, RestorePreflightResponse, RuntimeSettings, RuntimeUpdate, ServerVariableDefinition } from '@/types/settings';

async function multipart<T>(path: string, form: FormData): Promise<T> {
  const response = await fetch(`/api/v1${path}`, { method: 'POST', headers: authHeaders({ Accept: 'application/json' }), body: form });
  const envelope = await response.json().catch((error: unknown) => {
    throw new ApiError('Unable to parse JSON response.', response.status, 'invalid_json_response', error);
  }) as ApiEnvelope<T>;
  if (!response.ok || envelope.error) {
    const payload = envelope.error ?? {};
    throw new ApiError(payload.message ?? `Request failed with status ${response.status}.`, response.status, payload.code ?? 'api_error', payload.details);
  }
  if (!('data' in envelope)) throw new ApiError('API response is missing the data envelope.', response.status, 'missing_data_envelope');
  return envelope.data as T;
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
