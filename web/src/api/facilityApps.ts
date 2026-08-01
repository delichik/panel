import { apiClient } from './client';
import type { ApiRequestOptions } from './client';
import { fetchDownload, type DownloadResult } from './download';
import { deleteJson, idempotencyKey as key, multipartJson } from './assetRequests';
import type {
  FacilityEditCommitResult,
  FacilityEditPreviewResult,
  FacilityEditSession,
  FacilityEditValidationResult,
  ReverseProxyConfig,
  ReverseProxySaveInput,
} from '@/types/facilityApps';

function id(value: string) {
  return encodeURIComponent(value);
}

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
  recoverableEditSessions(options?: ApiRequestOptions) {
    return apiClient.get<FacilityEditSession[]>('/facility-apps/reverse-proxy/edit-sessions/recoverable?clientDraftKey=facility%3Areverse-proxy', options);
  },
  patchEdit(sessionId: string, revision: number, baseResourceVersion: string, draft: ReverseProxySaveInput) {
    return apiClient.patch<FacilityEditSession>(`/facility-apps/reverse-proxy/edit-sessions/${id(sessionId)}/draft`, { revision, baseResourceVersion, draft });
  },
  validateEdit(sessionId: string, revision: number) {
    return apiClient.post<FacilityEditValidationResult>(`/facility-apps/reverse-proxy/edit-sessions/${id(sessionId)}/validate`, { revision });
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
    const form = new FormData();
    form.set('file', input.file);
    form.set('revision', String(revision));
    form.set('clientOperationId', key());
    form.set('name', input.name);
    form.set('kind', input.kind);
    form.set('contentMode', input.contentMode ?? 'binary');
    return multipartJson<FacilityEditSession>(`/facility-apps/reverse-proxy/edit-sessions/${id(sessionId)}/assets/${id(assetName)}`, form, 'PUT');
  },
  downloadEditAsset(sessionId: string, assetName: string, filename: string): Promise<DownloadResult> {
    return fetchDownload(`/api/v1/facility-apps/reverse-proxy/edit-sessions/${id(sessionId)}/assets/${id(assetName)}/content`, {}, filename);
  },
  downloadStaticAsset(assetName: string, filename: string): Promise<DownloadResult> {
    return fetchDownload(`/api/v1/facility-apps/reverse-proxy/static-assets/${id(assetName)}/content`, {}, filename);
  },
  deleteEditAsset(sessionId: string, assetName: string, revision: number) {
    return deleteJson<FacilityEditSession>(`/facility-apps/reverse-proxy/edit-sessions/${id(sessionId)}/assets/${id(assetName)}`, {
      revision,
      clientOperationId: key(),
    });
  },
  discardEdit(sessionId: string) {
    return apiClient.delete<void>(`/facility-apps/reverse-proxy/edit-sessions/${id(sessionId)}`);
  },
};
