import { apiClient, type ApiClient } from './client';
import type { TaskDto, TaskListDto, TaskLogsDto, TaskStatus, TaskStepDto } from '@/types/api';

export interface TaskListFilters {
  status?: TaskStatus | 'all';
  statuses?: Array<TaskStatus | 'all'> | null;
  serverId?: string | null;
  type?: string | null;
  types?: string[] | null;
  includeInternal?: boolean;
  commonOnly?: boolean;
  operationPage?: boolean;
  operationId?: string | null;
  limit?: number;
  page?: number;
  pageSize?: number;
}

function setTrimmedParam(params: URLSearchParams, key: string, value?: string | null) {
  const trimmed = value?.trim();
  if (trimmed) params.set(key, trimmed);
}

function appendTrimmedParams(params: URLSearchParams, key: string, values?: Array<string | null | undefined> | null) {
  const seen = new Set<string>();
  for (const value of values ?? []) {
    const trimmed = value?.trim();
    if (!trimmed || trimmed === 'all' || seen.has(trimmed)) continue;
    seen.add(trimmed);
    params.append(key, trimmed);
  }
}

export function createTasksApi(client: ApiClient = apiClient) {
  return {
    list(filters: TaskListFilters = {}) {
      const pageSize = filters.pageSize ?? filters.limit ?? 20;
      const params = new URLSearchParams({ page: String(filters.page ?? 1), pageSize: String(pageSize) });
      appendTrimmedParams(params, 'status', filters.statuses ?? (filters.status ? [filters.status] : null));
      setTrimmedParam(params, 'serverId', filters.serverId);
      appendTrimmedParams(params, 'type', filters.types ?? (filters.type ? [filters.type] : null));
      if (filters.includeInternal) params.set('includeInternal', 'true');
      if (filters.commonOnly) params.set('commonOnly', 'true');
      if (filters.operationPage) params.set('operationPage', 'true');
      setTrimmedParam(params, 'operation_id', filters.operationId);
      return client.get<TaskListDto>(`/tasks?${params.toString()}`);
    },
    get(taskId: string) {
      return client.get<TaskDto>(`/tasks/${taskId}`);
    },
    logs(taskId: string, after = 0) {
      return client.get<TaskLogsDto>(`/tasks/${taskId}/logs?after=${after}`);
    },
    steps(taskId: string) {
      return client.get<TaskStepDto[]>(`/tasks/${taskId}/steps`);
    },
    retry(taskId: string) {
      return client.post<TaskDto>(`/tasks/${taskId}/retry`);
    },
    runNow(taskId: string) {
      return client.post<TaskDto>(`/tasks/${taskId}/run-now`);
    },
  };
}

export const tasksApi = createTasksApi();
