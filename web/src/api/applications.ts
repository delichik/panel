import { apiClient, type ApiClient } from './client';
import type {
  ApplicationDto,
  ApplicationFileDeleteDto,
  ApplicationFileDto,
  ApplicationFileSaveDto,
  ApplicationLogsDto,
  ApplicationOperationDto,
  ApplicationPlanDto,
  ApplicationRuntimeDto,
  ApplicationSaveDto,
  ApplicationSaveSessionBeginDto,
  ApplicationSaveSessionDto,
  ApplicationTemplateCatalogDto,
  ApplicationValidationDto,
} from '@/types/api';

export interface ApplicationLogsInput {
  instanceId: string;
  containerName?: string;
  type?: string;
  tail?: number;
}

function applicationPath(applicationId: string) {
  return `/applications/${encodeURIComponent(applicationId)}`;
}

function logsPath(applicationId: string, input: ApplicationLogsInput) {
  const params = new URLSearchParams();
  params.set('instanceId', input.instanceId);
  if (input.containerName) params.set('containerName', input.containerName);
  if (input.type) params.set('type', input.type);
  if (input.tail) params.set('tail', String(input.tail));
  return `${applicationPath(applicationId)}/logs?${params.toString()}`;
}

export function createApplicationsApi(client: ApiClient = apiClient) {
  return {
    list() {
      return client.get<ApplicationDto[]>('/applications');
    },
    templateCatalog() {
      return client.get<ApplicationTemplateCatalogDto>('/application-template-catalog');
    },
    create(input: ApplicationSaveDto) {
      return client.post<ApplicationDto>('/applications', input);
    },
    get(applicationId: string) {
      return client.get<ApplicationDto>(applicationPath(applicationId));
    },
    update(applicationId: string, input: ApplicationSaveDto) {
      return client.put<ApplicationDto>(applicationPath(applicationId), input);
    },
    delete(applicationId: string) {
      return client.delete(applicationPath(applicationId));
    },
    files(applicationId: string) {
      return client.get<ApplicationFileDto[]>(`${applicationPath(applicationId)}/files`);
    },
    getFile(applicationId: string, fileId: string) {
      return client.get<ApplicationFileDto>(`${applicationPath(applicationId)}/files/${encodeURIComponent(fileId)}`);
    },
    saveFile(applicationId: string, input: ApplicationFileSaveDto) {
      return client.post<ApplicationFileDto>(`${applicationPath(applicationId)}/files`, input);
    },
    deleteFile(applicationId: string, fileId: string) {
      return client.delete(`${applicationPath(applicationId)}/files/${encodeURIComponent(fileId)}`);
    },
    beginSaveSession(input: ApplicationSaveSessionBeginDto) {
      return client.post<ApplicationSaveSessionDto>('/application-save-sessions', input);
    },
    uploadSaveSessionFile(sessionId: string, input: ApplicationFileSaveDto) {
      return client.post<ApplicationFileDto>(`/application-save-sessions/${encodeURIComponent(sessionId)}/files`, input);
    },
    uploadSaveSessionArchive(sessionId: string, input: { basePath: string; kind: string; file: File }) {
      const form = new FormData();
      form.set('basePath', input.basePath);
      form.set('kind', input.kind);
      form.set('file', input.file);
      return client.postForm<ApplicationFileDto[]>(`/application-save-sessions/${encodeURIComponent(sessionId)}/files/archive`, form);
    },
    deleteSaveSessionFile(sessionId: string, input: ApplicationFileDeleteDto) {
      return client.post<void>(`/application-save-sessions/${encodeURIComponent(sessionId)}/files/delete`, input);
    },
    commitSaveSession(sessionId: string) {
      return client.post<ApplicationDto>(`/application-save-sessions/${encodeURIComponent(sessionId)}/commit`);
    },
    package(applicationId: string) {
      return client.download(`${applicationPath(applicationId)}/package`);
    },
    persistentData(applicationId: string) {
      return client.download(`${applicationPath(applicationId)}/persistent-data`);
    },
    restorePersistentData(applicationId: string, file: File) {
      const form = new FormData();
      form.set('file', file);
      return client.postForm<ApplicationOperationDto>(`${applicationPath(applicationId)}/persistent-data`, form);
    },
    validate(applicationId: string) {
      return client.post<ApplicationValidationDto>(`${applicationPath(applicationId)}/validate`);
    },
    plan(applicationId: string) {
      return client.post<ApplicationPlanDto>(`${applicationPath(applicationId)}/plan`);
    },
    checkImage(applicationId: string) {
      return client.post<ApplicationDto>(`${applicationPath(applicationId)}/image/check`);
    },
    updateImage(applicationId: string) {
      return client.post<ApplicationOperationDto>(`${applicationPath(applicationId)}/image/update`);
    },
    deploy(applicationId: string) {
      return client.post<ApplicationOperationDto>(`${applicationPath(applicationId)}/deploy`);
    },
    migrate(applicationId: string, sourceServerId: string, targetServerId: string) {
      return client.post<ApplicationOperationDto>(`${applicationPath(applicationId)}/migrate`, { sourceServerId, targetServerId });
    },
    stop(applicationId: string) {
      return client.post<ApplicationOperationDto>(`${applicationPath(applicationId)}/stop`);
    },
    restart(applicationId: string) {
      return client.post<ApplicationOperationDto>(`${applicationPath(applicationId)}/restart`);
    },
    runtime(applicationId: string) {
      return client.get<ApplicationRuntimeDto>(`${applicationPath(applicationId)}/runtime`);
    },
    logs(applicationId: string, input: ApplicationLogsInput) {
      return client.get<ApplicationLogsDto>(logsPath(applicationId, input));
    },
  };
}

export const applicationsApi = createApplicationsApi();
