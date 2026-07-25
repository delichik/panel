import { ApiError, apiClient, authHeaders } from './client';
import { fetchDownload, type DownloadResult } from './download';
import type {
  CreateCaAssetInput,
  CreateTlsAssetInput,
  ExportKeyAssetsInput,
  ExportKeyAssetsResult,
  GenerateSshAssetInput,
  ImportExecuteInput,
  ImportExecuteResult,
  ImportKeyAssetInput,
  ImportPreflightDto,
  KeyAssetDto,
  KeyAssetMutationResult,
  SystemCertificateDto,
  SystemCertificateResetResult,
} from '@/types/keyAssets';

export const keyAssetsApi = {
  list: () => apiClient.get<KeyAssetDto[]>('/key-assets'),
  get: (id: string) => apiClient.get<KeyAssetDto>(`/key-assets/${encodeURIComponent(id)}`),
  createCa: (input: CreateCaAssetInput) => apiClient.post<KeyAssetMutationResult>('/key-assets/ca', input),
  createTls: (input: CreateTlsAssetInput) => apiClient.post<KeyAssetMutationResult>('/key-assets/tls', input),
  generateSsh: (input: GenerateSshAssetInput) => apiClient.post<KeyAssetMutationResult>('/key-assets/ssh/generate', input),
  importOne: (input: ImportKeyAssetInput) => apiClient.post<KeyAssetMutationResult>('/key-assets/import', input),
  reissue: (id: string) => apiClient.post<KeyAssetMutationResult>(`/key-assets/${encodeURIComponent(id)}/reissue`),
  regenerate: (id: string) => apiClient.post<KeyAssetMutationResult>(`/key-assets/${encodeURIComponent(id)}/regenerate`),
  delete: (id: string) => apiClient.delete<void>(`/key-assets/${encodeURIComponent(id)}`),
  createExport: (input: ExportKeyAssetsInput) => apiClient.post<ExportKeyAssetsResult>('/key-assets/exports', input),
  preflightImport: async (file: File, password: string) => {
    const data = new FormData();
    data.set('file', file);
    data.set('password', password);
    const response = await fetch('/api/v1/key-assets/imports/preflight', {
      method: 'POST',
      headers: authHeaders({ Accept: 'application/json' }),
      body: data,
    });
    const envelope = await response.json().catch((error: unknown) => {
      throw new ApiError('Unable to parse JSON response.', response.status, 'invalid_json_response', error);
    }) as { data?: ImportPreflightDto; error?: { code?: string; message?: string; details?: unknown } };
    if (!response.ok || envelope.error) {
      throw new ApiError(envelope.error?.message ?? `Request failed with status ${response.status}.`, response.status, envelope.error?.code ?? 'api_error', envelope.error?.details);
    }
    if (!envelope.data) throw new ApiError('API response is missing the data envelope.', response.status, 'missing_data_envelope');
    return envelope.data;
  },
  executeImport: (planId: string, input: ImportExecuteInput) => apiClient.post<ImportExecuteResult>(`/key-assets/imports/${encodeURIComponent(planId)}/execute`, input),
  systemCertificates: () => apiClient.get<SystemCertificateDto[]>('/key-assets/system'),
  resetSystemCertificate: (id: string) => apiClient.post<SystemCertificateResetResult>(`/key-assets/system/${encodeURIComponent(id)}/reset`),
  downloadFile: (assetId: string, kind: string): Promise<DownloadResult> => fetchDownload(keyAssetFileUrl(assetId, kind), {}, `${assetId}-${kind}.pem`),
  downloadExport: (taskId: string): Promise<DownloadResult> => fetchDownload(keyAssetExportUrl(taskId), {}, `${taskId}.panel-key-assets`),
};

export function keyAssetFileUrl(assetId: string, kind: string) {
  return `/api/v1/key-assets/${encodeURIComponent(assetId)}/files/${encodeURIComponent(kind)}`;
}

export function keyAssetExportUrl(taskId: string) {
  return `/api/v1/key-assets/exports/${encodeURIComponent(taskId)}/download`;
}
