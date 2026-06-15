import { apiClient, type ApiClient } from './client';
import type {
  KeyAssetCaGenerateInput,
  KeyAssetDetailDto,
  KeyAssetExportDto,
  KeyAssetExportInput,
  KeyAssetFileKind,
  KeyAssetImportExecuteDto,
  KeyAssetImportExecuteInput,
  KeyAssetImportInput,
  KeyAssetImportPreflightDto,
  KeyAssetMutationDto,
  KeyAssetSshGenerateInput,
  KeyAssetSummaryDto,
  KeyAssetTlsGenerateInput,
  SystemCertificateDto,
} from '@/types/api';

export function createKeyAssetsApi(client: ApiClient = apiClient) {
  return {
    list() {
      return client.get<KeyAssetSummaryDto[]>('/key-assets');
    },
    listSystemCertificates() {
      return client.get<SystemCertificateDto[]>('/key-assets/system');
    },
    resetSystemCertificate(certificateId: string) {
      return client.post<{ taskId: string }>(`/key-assets/system/${encodeURIComponent(certificateId)}/reset`);
    },
    get(assetId: string) {
      return client.get<KeyAssetDetailDto>(`/key-assets/${assetId}`);
    },
    createCa(input: KeyAssetCaGenerateInput) {
      return client.post<KeyAssetMutationDto>('/key-assets/ca', input);
    },
    createTls(input: KeyAssetTlsGenerateInput) {
      return client.post<KeyAssetMutationDto>('/key-assets/tls', input);
    },
    generateSsh(input: KeyAssetSshGenerateInput) {
      return client.post<KeyAssetMutationDto>('/key-assets/ssh/generate', input);
    },
    importAsset(input: KeyAssetImportInput) {
      return client.post<KeyAssetMutationDto>('/key-assets/import', input);
    },
    reissue(assetId: string) {
      return client.post<KeyAssetMutationDto>(`/key-assets/${assetId}/reissue`);
    },
    regenerate(assetId: string) {
      return client.post<KeyAssetMutationDto>(`/key-assets/${assetId}/regenerate`);
    },
    downloadFile(assetId: string, kind: KeyAssetFileKind) {
      return client.download(`/key-assets/${assetId}/files/${kind}`);
    },
    delete(assetId: string) {
      return client.delete(`/key-assets/${assetId}`);
    },
    exportSelected(input: KeyAssetExportInput) {
      return client.post<KeyAssetExportDto>('/key-assets/exports', input);
    },
    downloadExport(taskId: string) {
      return client.download(`/key-assets/exports/${taskId}/download`);
    },
    preflightImportArchive(file: File, password: string) {
      const formData = new FormData();
      formData.set('file', file);
      formData.set('password', password);
      return client.postForm<KeyAssetImportPreflightDto>('/key-assets/imports/preflight', formData);
    },
    executeImport(planId: string, input: KeyAssetImportExecuteInput) {
      return client.post<KeyAssetImportExecuteDto>(`/key-assets/imports/${planId}/execute`, input);
    },
  };
}

export const keyAssetsApi = createKeyAssetsApi();
