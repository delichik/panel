import { apiClient } from './client';
import type { ApiRequestOptions } from './client';
import { fetchDownload, type DownloadResult } from './download';
import { deleteJson, idempotencyKey as key, multipartJson } from './assetRequests';
import type {
  ConfigRecord,
  FacilityEditCommitResult,
  FacilityEditPreviewResult,
  FacilityEditSession,
  FacilityRouteDomain,
  FacilitySettingsSaveInput,
  ProxyImageActionResponse,
  ProxyImageRollbackInput,
  ProxyImageState,
  ProxyImageUpdateInput,
  ReverseProxyConfig,
  ProxyDiagnostics,
  ProxyPlaneSaveInput,
  ProxyPlaneState,
  ReverseProxyReconcileInput,
  StorageShareConfig,
  StorageShareReconcileResult,
  StorageShareSaveInput,
  StorageShareStatus,
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
    return apiClient.post<StorageShareReconcileResult>('/facility-apps/storage-share/reconcile');
  },
  status(options?: ApiRequestOptions) {
    return apiClient.get<StorageShareStatus>('/facility-apps/storage-share/status', options);
  },
  uninstall() {
    return apiClient.delete<StorageShareConfig>('/facility-apps/storage-share');
  },
  downloadPartition(partitionId: string, filename: string): Promise<DownloadResult> {
    return fetchDownload(`/api/v1/facility-apps/storage-share/partitions/${id(partitionId)}/download`, {}, filename);
  },
  deletePartition(partitionId: string) {
    return apiClient.delete<void>(`/facility-apps/storage-share/partitions/${id(partitionId)}`);
  },
};

export const reverseProxyFacilityApi = {
	getDiagnostics(serverId: string, options?: ApiRequestOptions) {
		return apiClient.get<ProxyDiagnostics>(`/facility-apps/reverse-proxy/diagnostics?serverId=${id(serverId)}`, options);
	},
  getConfig(options?: ApiRequestOptions) {
    return apiClient.get<ReverseProxyConfig>('/facility-apps/reverse-proxy', options);
  },
  /** Global entrance parameters (80 policy, HSTS, custom http directives, image auto-update). */
  saveSettings(input: FacilitySettingsSaveInput) {
    return apiClient.put<ReverseProxyConfig>('/facility-apps/reverse-proxy/settings', input);
  },
  /** Manual image check; the backend enqueues a task and projects new state through GET. */
  checkImage() {
    return apiClient.post<ProxyImageActionResponse>('/facility-apps/reverse-proxy/image/check', {});
  },
  /** Manual rolling image update; `confirm` must be explicitly true. */
  updateImage(input: ProxyImageUpdateInput) {
    return apiClient.post<ProxyImageActionResponse>('/facility-apps/reverse-proxy/image/update', {
      serverIds: [...input.serverIds],
      confirm: true,
    });
  },
  /**
   * P1 rollback/resume contract (FAC-RP-014):
   *   POST /facility-apps/reverse-proxy/image/rollback {serverIds?, digest?, confirm:true}
   *   POST /facility-apps/reverse-proxy/image/resume   {serverIds?}
   * `digest` is omitted so the backend rolls each selected gateway back to its own
   * previous digest. Rollback returns 202 + taskId, or 200 + skipped for an
   * idempotent no-op. Resume returns the refreshed image projection synchronously.
   * If the final backend contract exposes "resume" as an update payload flag
   * (clearPinned) instead of this route, only this API block and its callers in
   * the facilities settings tab need to change.
   */
  rollbackImage(input: ProxyImageRollbackInput) {
    return apiClient.post<ProxyImageActionResponse>('/facility-apps/reverse-proxy/image/rollback', {
      ...(input.serverIds?.length ? { serverIds: [...input.serverIds] } : {}),
      ...(input.digest ? { digest: input.digest } : {}),
      confirm: true,
    });
  },
  resumeImage(serverIds: string[] = []) {
    return apiClient.post<ProxyImageState>('/facility-apps/reverse-proxy/image/resume', {
      ...(serverIds.length ? { serverIds: [...serverIds] } : {}),
    });
  },
  // Proxy plane: gateway membership / container deployments. Saving it never
  // touches individual config-record versions.
  getProxyPlane(options?: ApiRequestOptions) {
    return apiClient.get<ProxyPlaneState>('/facility-apps/reverse-proxy/proxy-plane', options);
  },
  saveProxyPlane(input: ProxyPlaneSaveInput) {
    return apiClient.put<ProxyPlaneState>('/facility-apps/reverse-proxy/proxy-plane', { deploymentServers: [...input.deploymentServers] });
  },
  // Independent config records.
  listConfigRecords(options?: ApiRequestOptions) {
    return apiClient.get<ConfigRecord[]>('/facility-apps/reverse-proxy/config-records', options);
  },
  getConfigRecord(recordId: string, options?: ApiRequestOptions) {
    return apiClient.get<ConfigRecord>(`/facility-apps/reverse-proxy/config-records/${id(recordId)}`, options);
  },
  deleteConfigRecord(recordId: string, version: number) {
    return apiClient.delete<void>(`/facility-apps/reverse-proxy/config-records/${id(recordId)}?version=${encodeURIComponent(String(version))}`);
  },
  applyConfigRecord(recordId: string) {
    return apiClient.post<void>(`/facility-apps/reverse-proxy/config-records/${id(recordId)}/apply`);
  },
  reconcile(input?: ReverseProxyReconcileInput) {
    return apiClient.post<{ config: ReverseProxyConfig }>('/facility-apps/reverse-proxy/reconcile', input ?? {});
  },
  syncDNS(input: { domains?: string[]; takeover?: boolean; confirmTakeover?: boolean }) {
    return apiClient.post<{ taskId: string }>('/facility-apps/reverse-proxy/dns-sync', {
      domains: input.domains ?? [],
      takeover: Boolean(input.takeover),
      confirmTakeover: Boolean(input.confirmTakeover),
    });
  },
  beginEdit(input: { recordId: string; draft?: FacilityRouteDomain; clientDraftKey?: string }) {
    return apiClient.post<FacilityEditSession>('/facility-apps/reverse-proxy/edit-sessions', {
      recordId: input.recordId,
      // Omit draft for an existing record so the server seeds from the
      // committed record. Sending a stale client snapshot would silently
      // overwrite newer route fields on the next asset-only commit.
      ...(input.clientDraftKey ? { clientDraftKey: input.clientDraftKey } : {}),
      ...(input.draft ? { draft: input.draft } : {}),
    });
  },
  patchEdit(sessionId: string, revision: number, baseResourceVersion: string, draft: FacilityRouteDomain) {
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
  /** Committed per-record asset; the record scope is mandatory. */
  downloadStaticAsset(recordId: string, assetName: string, filename: string): Promise<DownloadResult> {
    return fetchDownload(`/api/v1/facility-apps/reverse-proxy/static-assets/${id(assetName)}/content?recordId=${id(recordId)}`, {}, filename);
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
