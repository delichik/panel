import { apiClient } from './client';
import { ApiError, type ApiEnvelope, authHeaders, type ApiRequestOptions } from './client';
import { fetchDownload, type DownloadResult } from './download';
import type {
  ApplicationDto,
  ApplicationEditCommitResult,
  ApplicationEditPreviewResult,
  ApplicationEditSession,
  ApplicationEditSessionFileContent,
  ApplicationEditValidationResult,
  ApplicationFile,
  ApplicationRuntime,
  ApplicationSaveInput,
  ApplicationSummaryDto,
  LogResult,
  OperationResult,
} from '@/types/applications';
import type { ListPage } from '@/types/pagination';

function id(value: string) {
  return encodeURIComponent(value);
}

function key() {
  return `${Date.now()}-${Math.random().toString(16).slice(2)}`;
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

async function multipartJson<T>(path: string, form: FormData, idempotencyKey = key(), method = 'POST'): Promise<T> {
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

export const applicationsApi = {
  list(params: { page?: number; pageSize?: number; q?: string } = {}, options?: ApiRequestOptions) {
    const query = new URLSearchParams();
    Object.entries(params).forEach(([name, value]) => {
      if (value !== undefined && value !== '') query.set(name, String(value));
    });
    return apiClient.get<ListPage<ApplicationSummaryDto>>(`/applications${query.size ? `?${query}` : ''}`, options);
  },
  get(applicationId: string, options?: ApiRequestOptions) {
    return apiClient.get<ApplicationDto>(`/applications/${id(applicationId)}`, options);
  },
  listFiles(applicationId: string, options?: ApiRequestOptions) {
    return apiClient.get<ApplicationFile[]>(`/applications/${id(applicationId)}/files`, options);
  },
  downloadFile(applicationId: string, fileId: string, filename: string): Promise<DownloadResult> {
    return fetchDownload(`/api/v1/applications/${id(applicationId)}/files/${id(fileId)}/content`, {}, filename);
  },
  delete(applicationId: string) {
    return apiClient.delete<void>(`/applications/${id(applicationId)}`);
  },
  checkImage(applicationId: string) {
    return apiClient.post<ApplicationDto>(`/applications/${id(applicationId)}/image/check`);
  },
  updateImage(applicationId: string) {
    return apiClient.post<OperationResult>(`/applications/${id(applicationId)}/image/update`);
  },
  deploy(applicationId: string) {
    return apiClient.post<OperationResult>(`/applications/${id(applicationId)}/deploy`);
  },
  stop(applicationId: string, purge = false) {
    return apiClient.post<OperationResult>(`/applications/${id(applicationId)}/stop${purge ? '?purge=true' : ''}`);
  },
  restart(applicationId: string) {
    return apiClient.post<OperationResult>(`/applications/${id(applicationId)}/restart`);
  },
  runtime(applicationId: string, options?: ApiRequestOptions) {
    return apiClient.get<ApplicationRuntime>(`/applications/${id(applicationId)}/runtime`, options);
  },
  logs(applicationId: string, params: { instanceId?: string; containerName?: string; type?: string; tail?: number } = {}) {
    const query = new URLSearchParams();
    Object.entries(params).forEach(([name, value]) => {
      if (value !== undefined && value !== '') query.set(name, String(value));
    });
    return apiClient.get<LogResult>(`/applications/${id(applicationId)}/logs${query.size ? `?${query}` : ''}`);
  },
  downloadPersistentData(applicationId: string): Promise<DownloadResult> {
    return fetchDownload(`/api/v1/applications/${id(applicationId)}/persistent-data`, {}, `${applicationId}-persistent.zip`);
  },
  restorePersistentData(applicationId: string, file: File) {
    const form = new FormData();
    form.set('file', file);
    return multipartJson<OperationResult>(`/applications/${id(applicationId)}/persistent-data`, form);
  },
  beginEditSession(applicationId?: string, draft?: ApplicationSaveInput) {
    return apiClient.post<ApplicationEditSession>('/application-edit-sessions', {
      applicationId,
      clientDraftKey: applicationId ? `application:${applicationId}` : 'application:create',
      draft,
    });
  },
  recoverableEditSessions(applicationId?: string, options?: ApiRequestOptions) {
    const query = new URLSearchParams();
    query.set('clientDraftKey', applicationId ? `application:${applicationId}` : 'application:create');
    if (applicationId) query.set('applicationId', applicationId);
    return apiClient.get<ApplicationEditSession[]>(`/application-edit-sessions/recoverable?${query}`, options);
  },
  patchEditSession(sessionId: string, revision: number, draft: ApplicationSaveInput) {
    return apiClient.patch<ApplicationEditSession>(`/application-edit-sessions/${id(sessionId)}/draft`, { revision, draft });
  },
  getEditSessionFile(sessionId: string, fileKey: string) {
    return apiClient.get<ApplicationEditSessionFileContent>(`/application-edit-sessions/${id(sessionId)}/files/${id(fileKey)}`);
  },
  putEditSessionFile(sessionId: string, fileKey: string, revision: number, input: { path: string; contentBase64: string }) {
    return apiClient.put<ApplicationEditSession>(`/application-edit-sessions/${id(sessionId)}/files/${id(fileKey)}`, {
      revision,
      clientOperationId: key(),
      ...input,
    }, { headers: { 'Idempotency-Key': key() } });
  },
  uploadEditSessionFile(sessionId: string, fileKey: string, revision: number, input: { file: File; path: string }) {
    const form = new FormData();
    form.set('file', input.file);
    form.set('revision', String(revision));
    form.set('clientOperationId', key());
    form.set('path', input.path);
    return multipartJson<ApplicationEditSession>(`/application-edit-sessions/${id(sessionId)}/uploads/${id(fileKey)}`, form, key(), 'PUT');
  },
  uploadEditSessionArchive(sessionId: string, revision: number, input: { file: File; fileKey: string; basePath: string; kind: string }) {
    const form = new FormData();
    form.set('file', input.file);
    form.set('revision', String(revision));
    form.set('clientOperationId', key());
    form.set('fileKey', input.fileKey);
    form.set('basePath', input.basePath);
    form.set('kind', input.kind);
    return multipartJson<ApplicationEditSession>(`/application-edit-sessions/${id(sessionId)}/archives`, form);
  },
  downloadEditSessionFile(sessionId: string, fileKey: string, filename: string): Promise<DownloadResult> {
    return fetchDownload(`/api/v1/application-edit-sessions/${id(sessionId)}/files/${id(fileKey)}/content`, {}, filename);
  },
  deleteEditSessionFile(sessionId: string, fileKey: string, revision: number) {
    return deleteJson<ApplicationEditSession>(`/application-edit-sessions/${id(sessionId)}/files/${id(fileKey)}`, {
      revision,
      clientOperationId: key(),
    });
  },
  validateEditSession(sessionId: string, revision: number) {
    return apiClient.post<ApplicationEditValidationResult>(`/application-edit-sessions/${id(sessionId)}/validate`, { revision });
  },
  previewEditSession(sessionId: string, revision: number) {
    return apiClient.post<ApplicationEditPreviewResult>(`/application-edit-sessions/${id(sessionId)}/preview`, { revision });
  },
  commitEditSession(session: ApplicationEditSession, preview: ApplicationEditPreviewResult) {
    return apiClient.post<ApplicationEditCommitResult>(`/application-edit-sessions/${id(session.id)}/commit`, {
      revision: session.revision,
      baseResourceVersion: session.baseResourceVersion.value,
      previewToken: preview.token.value,
    }, { headers: { 'Idempotency-Key': key() } });
  },
  discardEditSession(sessionId: string) {
    return apiClient.delete<void>(`/application-edit-sessions/${id(sessionId)}`);
  },
};
