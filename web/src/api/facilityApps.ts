import { apiClient } from './client';
import type { FacilityReverseProxyConfigDto, FacilityReverseProxyOperationDto, FacilityReverseProxySaveDto, FacilityStaticAssetDto } from '@/types/api';

export const facilityAppsApi = {
  reverseProxy() {
    return apiClient.get<FacilityReverseProxyConfigDto>('/facility-apps/reverse-proxy');
  },
  saveReverseProxy(input: FacilityReverseProxySaveDto) {
    return apiClient.put<FacilityReverseProxyConfigDto>('/facility-apps/reverse-proxy', input);
  },
  reconcileReverseProxy() {
    return apiClient.post<FacilityReverseProxyOperationDto>('/facility-apps/reverse-proxy/reconcile');
  },
  staticAssets() {
    return apiClient.get<FacilityStaticAssetDto[]>('/facility-apps/reverse-proxy/static-assets');
  },
  uploadStaticAsset(input: { name: string; kind: string; file: File }) {
    const form = new FormData();
    form.set('name', input.name);
    form.set('kind', input.kind);
    form.set('file', input.file);
    return apiClient.postForm<FacilityStaticAssetDto>('/facility-apps/reverse-proxy/static-assets', form);
  },
  deleteStaticAsset(assetId: string) {
    return apiClient.delete(`/facility-apps/reverse-proxy/static-assets/${encodeURIComponent(assetId)}`);
  },
};
