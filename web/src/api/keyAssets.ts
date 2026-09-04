import { apiClient, fetchJson } from './client';
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
import type { ListPage } from '@/types/pagination';

export const keyAssetsApi = {
  listPage: (params: { page?: number; pageSize?: number; q?: string } = {}) => {
    const query = new URLSearchParams();
    Object.entries(params).forEach(([name, value]) => { if (value !== undefined && value !== '') query.set(name, String(value)); });
    return apiClient.get<ListPage<KeyAssetDto>>(`/key-assets${query.size ? `?${query}` : ''}`);
  },
  panelTLSCandidates: (domain?: string) => {
    const query = domain?.trim() ? `?domain=${encodeURIComponent(domain.trim())}` : '';
    return apiClient.get<KeyAssetDto[]>(`/key-assets/panel-tls${query}`);
  },
  createCa: (input: CreateCaAssetInput) => apiClient.post<KeyAssetMutationResult>('/key-assets/ca', input),
  createTls: (input: CreateTlsAssetInput) => apiClient.post<KeyAssetMutationResult>('/key-assets/tls', input),
  generateSsh: (input: GenerateSshAssetInput) => apiClient.post<KeyAssetMutationResult>('/key-assets/ssh/generate', input),
  importOne: (input: ImportKeyAssetInput) => apiClient.post<KeyAssetMutationResult>('/key-assets/import', input),
  reissue: (id: string) => apiClient.post<KeyAssetMutationResult>(`/key-assets/${encodeURIComponent(id)}/reissue`),
  regenerate: (id: string) => apiClient.post<KeyAssetMutationResult>(`/key-assets/${encodeURIComponent(id)}/regenerate`),
  delete: (id: string) => apiClient.delete<void>(`/key-assets/${encodeURIComponent(id)}`),
  createExport: (input: ExportKeyAssetsInput) => apiClient.post<ExportKeyAssetsResult>('/key-assets/exports', input),
  preflightImport: (file: File, password: string) => {
    const data = new FormData();
    data.set('file', file);
    data.set('password', password);
    return fetchJson<ImportPreflightDto>('/api/v1/key-assets/imports/preflight', {
      method: 'POST',
      body: data,
    });
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
