import { apiClient } from './client';
import { ApiError, type ApiEnvelope, authHeaders, type ApiRequestOptions } from './client';
import { fetchDownload, type DownloadResult } from './download';
import type {
  FacilityEditCommitResult,
  FacilityEditPreviewResult,
  FacilityEditSession,
  FacilityEditValidationResult,
  FacilityAppDetail,
  FacilityAppKind,
  FacilityAppSummary,
  ReverseProxyConfig,
  ReverseProxySaveInput,
} from '@/types/facilityApps';

function id(value: string) {
  return encodeURIComponent(value);
}

function key() {
  return `${Date.now()}-${Math.random().toString(16).slice(2)}`;
}

function reverseProxySummary(config: ReverseProxyConfig): FacilityAppSummary {
  return {
    kind: 'reverse-proxy',
    titleKey: 'applicationsPage.entranceProxyFacility',
    descriptionKey: 'applicationsPage.entranceProxyFacilityDescription',
    categoryKey: 'applicationsPage.facilityCategoryTraffic',
    status: config.lastError ? 'degraded' : 'available',
    updatedAt: config.updatedAt,
    operationStatus: config.operation?.status,
    lastError: config.lastError,
  };
}

function assertSupported(kind: string): asserts kind is FacilityAppKind {
  if (kind !== 'reverse-proxy') {
    throw new ApiError(`Unsupported facility app kind: ${kind}.`, 404, 'facility_app_kind_unsupported');
  }
}

async function deleteJson<T>(path: string, body: unknown, idempotencyKey = key()): Promise<T> {
  const response = await fetch(`/api/v1${path}`, {
    method: 'DELETE',
    headers: authHeaders({ Accept: 'application/json', 'Content-Type': 'application/json', 'Idempotency-Key': idempotencyKey }),
    body: JSON.stringify(body),
  });
  if (response.status === 204) return undefined as T;
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

async function multipartJson<T>(method: string, path: string, form: FormData, idempotencyKey = key()): Promise<T> {
  const response = await fetch(`/api/v1${path}`, {
    method,
    headers: authHeaders({ Accept: 'application/json', 'Idempotency-Key': idempotencyKey }),
    body: form,
  });
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

export const facilityAppsApi = {
  listFacilities(options?: ApiRequestOptions) {
    return apiClient.get<FacilityAppSummary[]>('/facility-apps', options);
  },
  async getFacility(kind: string, options?: ApiRequestOptions): Promise<FacilityAppDetail> {
    assertSupported(kind);
    const config = await apiClient.get<ReverseProxyConfig>('/facility-apps/reverse-proxy', options);
    return { kind, summary: reverseProxySummary(config), reverseProxy: config };
  },
  async reconcileFacility(kind: string) {
    assertSupported(kind);
    const result = await apiClient.post<{ config: ReverseProxyConfig }>('/facility-apps/reverse-proxy/reconcile');
    return { ...result, facility: { kind, summary: reverseProxySummary(result.config), reverseProxy: result.config } };
  },
  beginFacilityEdit(kind: string, draft?: ReverseProxySaveInput) {
    assertSupported(kind);
    return apiClient.post<FacilityEditSession>('/facility-apps/reverse-proxy/edit-sessions', {
      clientDraftKey: 'facility:reverse-proxy',
      draft,
    });
  },
  recoverableFacilityEditSessions(kind: string, options?: ApiRequestOptions) {
    assertSupported(kind);
    return apiClient.get<FacilityEditSession[]>('/facility-apps/reverse-proxy/edit-sessions/recoverable?clientDraftKey=facility%3Areverse-proxy', options);
  },
  patchFacilityEdit(kind: string, sessionId: string, revision: number, baseResourceVersion: string, draft: ReverseProxySaveInput) {
    assertSupported(kind);
    return apiClient.patch<FacilityEditSession>(`/facility-apps/reverse-proxy/edit-sessions/${id(sessionId)}/draft`, { revision, baseResourceVersion, draft });
  },
  validateFacilityEdit(kind: string, sessionId: string, revision: number) {
    assertSupported(kind);
    return apiClient.post<FacilityEditValidationResult>(`/facility-apps/reverse-proxy/edit-sessions/${id(sessionId)}/validate`, { revision });
  },
  previewFacilityEdit(kind: string, sessionId: string, revision: number) {
    assertSupported(kind);
    return apiClient.post<FacilityEditPreviewResult>(`/facility-apps/reverse-proxy/edit-sessions/${id(sessionId)}/preview`, { revision });
  },
  commitFacilityEdit(kind: string, session: FacilityEditSession, preview: FacilityEditPreviewResult) {
    assertSupported(kind);
    return apiClient.post<FacilityEditCommitResult>(`/facility-apps/reverse-proxy/edit-sessions/${id(session.id)}/commit`, {
      revision: session.revision,
      baseResourceVersion: session.baseResourceVersion.value,
      previewToken: preview.token.value,
    }, { headers: { 'Idempotency-Key': key() } });
  },
  putFacilityEditAsset(kind: string, sessionId: string, assetKey: string, revision: number, input: { file: File; name: string; kind: string; contentMode?: 'text' | 'binary' }) {
    assertSupported(kind);
    const form = new FormData();
    form.set('file', input.file);
    form.set('revision', String(revision));
    form.set('clientOperationId', key());
    form.set('name', input.name);
    form.set('kind', input.kind);
    form.set('contentMode', input.contentMode ?? 'binary');
    return multipartJson<FacilityEditSession>('PUT', `/facility-apps/reverse-proxy/edit-sessions/${id(sessionId)}/assets/${id(assetKey)}`, form);
  },
  downloadFacilityEditAsset(kind: string, sessionId: string, assetKey: string, filename: string): Promise<DownloadResult> {
    assertSupported(kind);
    return fetchDownload(`/api/v1/facility-apps/reverse-proxy/edit-sessions/${id(sessionId)}/assets/${id(assetKey)}/content`, {}, filename);
  },
  downloadStaticAsset(kind: string, assetId: string, filename: string): Promise<DownloadResult> {
    assertSupported(kind);
    return fetchDownload(`/api/v1/facility-apps/reverse-proxy/static-assets/${id(assetId)}/content`, {}, filename);
  },
  deleteFacilityEditAsset(kind: string, sessionId: string, assetKey: string, revision: number) {
    assertSupported(kind);
    return deleteJson<FacilityEditSession>(`/facility-apps/reverse-proxy/edit-sessions/${id(sessionId)}/assets/${id(assetKey)}`, {
      revision,
      clientOperationId: key(),
    });
  },
  discardFacilityEdit(kind: string, sessionId: string) {
    assertSupported(kind);
    return apiClient.delete<void>(`/facility-apps/reverse-proxy/edit-sessions/${id(sessionId)}`);
  },
};
