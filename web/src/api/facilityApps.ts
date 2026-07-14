import { apiClient, type ApiClient } from './client';
import type { FacilityReverseProxyConfigDto, FacilityReverseProxyOperationDto, FacilityReverseProxySaveDto, FacilitySaveSessionCommitDto, FacilitySaveSessionDto, FacilityStaticAssetDto } from '@/types/api';

export function createFacilityAppsApi(client: ApiClient) {
  return {
  reverseProxy() {
    return client.get<FacilityReverseProxyConfigDto>('/facility-apps/reverse-proxy');
  },
  saveReverseProxy(input: FacilityReverseProxySaveDto) {
    return client.put<FacilityReverseProxyConfigDto>('/facility-apps/reverse-proxy', input);
  },
  reconcileReverseProxy() {
    return client.post<FacilityReverseProxyOperationDto>('/facility-apps/reverse-proxy/reconcile');
  },
  staticAssets() {
    return client.get<FacilityStaticAssetDto[]>('/facility-apps/reverse-proxy/static-assets');
  },
  uploadStaticAsset(input: { name: string; kind: string; file: File }) {
    const form = new FormData();
    form.set('name', input.name);
    form.set('kind', input.kind);
    form.set('file', input.file);
    return client.postForm<FacilityStaticAssetDto>('/facility-apps/reverse-proxy/static-assets', form);
  },
  deleteStaticAsset(assetId: string) {
    return client.delete(`/facility-apps/reverse-proxy/static-assets/${encodeURIComponent(assetId)}`);
  },
  beginSaveSession(baseUpdatedAt: string) {
    return client.post<FacilitySaveSessionDto>('/facility-apps/reverse-proxy/save-sessions', { baseUpdatedAt });
  },
  uploadSaveSessionAsset(sessionId: string, input: { assetId?: string; name: string; kind: string; file: File }) {
    const form = new FormData();
    if (input.assetId) form.set('assetId', input.assetId);
    form.set('name', input.name);
    form.set('kind', input.kind);
    form.set('file', input.file);
    return client.postForm<FacilityStaticAssetDto>(`/facility-apps/reverse-proxy/save-sessions/${encodeURIComponent(sessionId)}/assets`, form);
  },
  deleteSaveSessionAsset(sessionId: string, assetId: string) {
    return client.post(`/facility-apps/reverse-proxy/save-sessions/${encodeURIComponent(sessionId)}/assets/delete`, { assetId });
  },
  commitSaveSession(sessionId: string, input: FacilityReverseProxySaveDto) {
    return client.post<FacilitySaveSessionCommitDto>(`/facility-apps/reverse-proxy/save-sessions/${encodeURIComponent(sessionId)}/commit`, { save: input });
  },
  discardSaveSession(sessionId: string) {
    return client.delete(`/facility-apps/reverse-proxy/save-sessions/${encodeURIComponent(sessionId)}`);
  },
  };
}

export const facilityAppsApi = createFacilityAppsApi(apiClient);
