import { apiClient } from './client';
import type { ApiRequestOptions } from './client';
import { fetchDownload, type DownloadResult } from './download';
import { deleteJson, idempotencyKey as key, multipartJson } from './assetRequests';
import type {
  FacilityEditCommitResult,
  FacilityEditPreviewResult,
  FacilityEditSession,
  ReverseProxyConfig,
  ReverseProxySaveInput,
  StorageShareConfig,
  StorageShareSaveInput,
} from '@/types/facilityApps';

function id(value: string) {
  return encodeURIComponent(value);
}

export const storageShareFacilityApi = {
  get(options?: ApiRequestOptions) {
    return apiClient.get<StorageShareConfig>('/facility-apps/storage-share', options);
  },
  save(input: StorageShareSaveInput) {
    return apiClient.put<StorageShareConfig>('/facility-apps/storage-share', input);
  },
  reconcile() {
    return apiClient.post<StorageShareConfig>('/facility-apps/storage-share/reconcile');
  },
  uninstall() {
    return apiClient.delete<void>('/facility-apps/storage-share');
  },
  downloadPartition(partitionId: string, filename: string): Promise<DownloadResult> {
    return fetchDownload(`/api/v1/facility-apps/storage-share/partitions/${id(partitionId)}/download`, {}, filename);
  },
  deletePartition(partitionId: string) {
    return apiClient.delete<void>(`/facility-apps/storage-share/partitions/${id(partitionId)}`);
  },
};

export const reverseProxyFacilityApi = {
  getConfig(options?: ApiRequestOptions) {
    return apiClient.get<ReverseProxyConfig>('/facility-apps/reverse-proxy', options);
  },
  reconcile() {
    return apiClient.post<{ config: ReverseProxyConfig }>('/facility-apps/reverse-proxy/reconcile');
  },
  beginEdit(draft?: ReverseProxySaveInput) {
    return apiClient.post<FacilityEditSession>('/facility-apps/reverse-proxy/edit-sessions', {
      clientDraftKey: 'facility:reverse-proxy',
      draft,
    });
  },
  patchEdit(sessionId: string, revision: number, baseResourceVersion: string, draft: ReverseProxySaveInput) {
    return apiClient.patch<FacilityEditSession>(`/facility-apps/reverse-proxy/edit-sessions/${id(sessionId)}/draft`, { revision, baseResourceVersion, draft });
  },
  previewEdit(sessionId: string, revision: number) {
    return apiClient.post<FacilityEditPreviewResult>(`/facility-apps/reverse-proxy/edit-sessions/${id(sessionId)}/preview`, { revision });
  },
  commitEdit(session: FacilityEditSession, preview: FacilityEditPreviewResult) {
    return apiClient.post<FacilityEditCommitResult>(`/facility-apps/reverse-proxy/edit-sessions/${id(session.id)}/commit`, {
      revision: session.revision,
      baseResourceVersion: session.baseResourceVersion.value,
      previewToken: preview.token.value,
    }, { headers: { 'Idempotency-Key': key() } });
  },
  putEditAsset(sessionId: string, assetName: string, revision: number, input: { file: File; name: string; kind: string; contentMode?: 'text' | 'binary' }) {
    const clientOperationId = key();
    const form = new FormData();
    form.set('file', input.file);
    form.set('revision', String(revision));
    form.set('clientOperationId', clientOperationId);
    form.set('name', input.name);
    form.set('kind', input.kind);
    form.set('contentMode', input.contentMode ?? 'binary');
    return multipartJson<FacilityEditSession>(`/facility-apps/reverse-proxy/edit-sessions/${id(sessionId)}/assets/${id(assetName)}`, form, 'PUT', clientOperationId);
  },
  downloadEditAsset(sessionId: string, assetName: string, filename: string): Promise<DownloadResult> {
    return fetchDownload(`/api/v1/facility-apps/reverse-proxy/edit-sessions/${id(sessionId)}/assets/${id(assetName)}/content`, {}, filename);
  },
  downloadStaticAsset(assetName: string, filename: string): Promise<DownloadResult> {
    return fetchDownload(`/api/v1/facility-apps/reverse-proxy/static-assets/${id(assetName)}/content`, {}, filename);
  },
  deleteEditAsset(sessionId: string, assetName: string, revision: number) {
    const clientOperationId = key();
    return deleteJson<FacilityEditSession>(`/facility-apps/reverse-proxy/edit-sessions/${id(sessionId)}/assets/${id(assetName)}`, {
      revision,
      clientOperationId,
    }, clientOperationId);
  },
  discardEdit(sessionId: string) {
    return apiClient.delete<void>(`/facility-apps/reverse-proxy/edit-sessions/${id(sessionId)}`);
  },
};
