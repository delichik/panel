import { apiClient } from './client';
import { useI18n } from '@/i18n';
import { ApiError, type ApiRequestOptions } from './client';
import { fetchDownload, type DownloadResult } from './download';
import { deleteJson, idempotencyKey as key, multipartJson } from './assetRequests';
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

const { t } = useI18n();

function id(value: string) {
  return encodeURIComponent(value);
}

function normalizeApplicationList(
  value: unknown,
  params: { page?: number; pageSize?: number },
): ListPage<ApplicationSummaryDto> {
  if (Array.isArray(value)) {
    return {
      items: value as ApplicationSummaryDto[],
      total: value.length,
      page: params.page ?? 1,
      pageSize: params.pageSize ?? 50,
    };
  }

  if (value && typeof value === 'object') {
    const page = value as Partial<ListPage<ApplicationSummaryDto>>;
    if (
      Array.isArray(page.items)
      && Number.isInteger(page.total) && (page.total ?? -1) >= 0
      && Number.isInteger(page.page) && (page.page ?? 0) >= 1
      && Number.isInteger(page.pageSize) && (page.pageSize ?? 0) >= 1
    ) {
      return page as ListPage<ApplicationSummaryDto>;
    }
  }

  throw new ApiError(t('api.invalidApplicationsList'), 200, 'invalid_api_response', value);
}

export const applicationsApi = {
  list(params: { page?: number; pageSize?: number; q?: string } = {}, options?: ApiRequestOptions) {
    const query = new URLSearchParams();
    Object.entries(params).forEach(([name, value]) => {
      if (value !== undefined && value !== '') query.set(name, String(value));
    });
    return apiClient.get<unknown>(`/applications${query.size ? `?${query}` : ''}`, options)
      .then((result) => normalizeApplicationList(result, params));
  },
  get(applicationId: string, options?: ApiRequestOptions) {
    return apiClient.get<ApplicationDto>(`/applications/${id(applicationId)}`, options);
  },
  listFiles(applicationId: string, options?: ApiRequestOptions) {
    return apiClient.get<ApplicationFile[]>(`/applications/${id(applicationId)}/files`, options);
  },
  downloadFile(applicationId: string, fileName: string, filename: string): Promise<DownloadResult> {
    return fetchDownload(`/api/v1/applications/${id(applicationId)}/files/${id(fileName)}/content`, {}, filename);
  },
  delete(applicationId: string) {
    return apiClient.delete<void>(`/applications/${id(applicationId)}`);
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
  logs(applicationId: string, params: { instanceId?: string; type?: string; tail?: number } = {}) {
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
  patchEditSession(sessionId: string, revision: number, draft: ApplicationSaveInput) {
    return apiClient.patch<ApplicationEditSession>(`/application-edit-sessions/${id(sessionId)}/draft`, { revision, draft });
  },
  getEditSessionFile(sessionId: string, fileName: string) {
    return apiClient.get<ApplicationEditSessionFileContent>(`/application-edit-sessions/${id(sessionId)}/files/${id(fileName)}`);
  },
  putEditSessionFile(sessionId: string, fileName: string, revision: number, input: { name: string; contentBase64: string }) {
    const clientOperationId = key();
    return apiClient.put<ApplicationEditSession>(`/application-edit-sessions/${id(sessionId)}/files/${id(fileName)}`, {
      revision,
      clientOperationId,
      ...input,
    }, { headers: { 'Idempotency-Key': clientOperationId } });
  },
  uploadEditSessionFile(sessionId: string, fileName: string, revision: number, input: { file: File; name: string }) {
    const clientOperationId = key();
    const form = new FormData();
    form.set('file', input.file);
    form.set('revision', String(revision));
    form.set('clientOperationId', clientOperationId);
    form.set('name', input.name);
    return multipartJson<ApplicationEditSession>(`/application-edit-sessions/${id(sessionId)}/uploads/${id(fileName)}`, form, 'PUT', clientOperationId);
  },
  uploadEditSessionArchive(sessionId: string, revision: number, input: { file: File; name: string; kind: string }) {
    const clientOperationId = key();
    const form = new FormData();
    form.set('file', input.file);
    form.set('revision', String(revision));
    form.set('clientOperationId', clientOperationId);
    form.set('name', input.name);
    form.set('kind', input.kind);
    return multipartJson<ApplicationEditSession>(`/application-edit-sessions/${id(sessionId)}/archives`, form, 'POST', clientOperationId);
  },
  downloadEditSessionFile(sessionId: string, fileName: string, filename: string): Promise<DownloadResult> {
    return fetchDownload(`/api/v1/application-edit-sessions/${id(sessionId)}/files/${id(fileName)}/content`, {}, filename);
  },
  deleteEditSessionFile(sessionId: string, fileName: string, revision: number) {
    const clientOperationId = key();
    return deleteJson<ApplicationEditSession>(`/application-edit-sessions/${id(sessionId)}/files/${id(fileName)}`, {
      revision,
      clientOperationId,
    }, clientOperationId);
  },
  validateEditSession(sessionId: string, revision: number) {
    return apiClient.post<ApplicationEditValidationResult>(`/application-edit-sessions/${id(sessionId)}/validate`, { revision });
  },
  previewEditSession(sessionId: string, revision: number) {
    return apiClient.post<ApplicationEditPreviewResult>(`/application-edit-sessions/${id(sessionId)}/preview`, { revision });
  },
  commitEditSession(session: ApplicationEditSession, preview: ApplicationEditPreviewResult) {
    // The commit endpoint keys deduplication on the Idempotency-Key header
    // only; the body intentionally carries no clientOperationId.
    const clientOperationId = key();
    return apiClient.post<ApplicationEditCommitResult>(`/application-edit-sessions/${id(session.id)}/commit`, {
      revision: session.revision,
      baseResourceVersion: session.baseResourceVersion.value,
      previewToken: preview.token.value,
    }, { headers: { 'Idempotency-Key': clientOperationId } });
  },
  discardEditSession(sessionId: string) {
    return apiClient.delete<void>(`/application-edit-sessions/${id(sessionId)}`);
  },
};
