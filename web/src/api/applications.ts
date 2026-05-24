import { apiClient, type ApiClient } from './client';
import type {
  ApplicationDto,
  ApplicationLogsDto,
  ApplicationOperationDto,
  ApplicationPlanDto,
  ApplicationRuntimeDto,
  ApplicationSaveDto,
  ApplicationValidationDto,
} from '@/types/api';

export interface ApplicationLogsInput {
  allocId: string;
  task: string;
  type?: string;
  tail?: number;
}

function applicationPath(applicationId: string) {
  return `/applications/${encodeURIComponent(applicationId)}`;
}

function logsPath(applicationId: string, input: ApplicationLogsInput) {
  const params = new URLSearchParams();
  params.set('allocId', input.allocId);
  params.set('task', input.task);
  if (input.type) params.set('type', input.type);
  if (input.tail) params.set('tail', String(input.tail));
  return `${applicationPath(applicationId)}/logs?${params.toString()}`;
}

export function createApplicationsApi(client: ApiClient = apiClient) {
  return {
    list() {
      return client.get<ApplicationDto[]>('/applications');
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
    validate(applicationId: string) {
      return client.post<ApplicationValidationDto>(`${applicationPath(applicationId)}/validate`);
    },
    plan(applicationId: string) {
      return client.post<ApplicationPlanDto>(`${applicationPath(applicationId)}/plan`);
    },
    deploy(applicationId: string) {
      return client.post<ApplicationOperationDto>(`${applicationPath(applicationId)}/deploy`);
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
