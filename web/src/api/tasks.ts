import { apiClient, type ApiClient } from './client';
import type { TaskDto, TaskListDto, TaskLogsDto, TaskStatus, TaskStepDto } from '@/types/api';

export interface TaskListFilters {
  status?: TaskStatus | 'all';
  serverId?: string;
  type?: string;
  operationId?: string;
  limit?: number;
  page?: number;
  pageSize?: number;
}

export function createTasksApi(client: ApiClient = apiClient) {
  return {
    list(filters: TaskListFilters = {}) {
      const pageSize = filters.pageSize ?? filters.limit ?? 20;
      const params = new URLSearchParams({ page: String(filters.page ?? 1), pageSize: String(pageSize) });
      if (filters.status && filters.status !== 'all') params.set('status', filters.status);
      if (filters.serverId) params.set('serverId', filters.serverId);
      if (filters.type) params.set('type', filters.type);
      if (filters.operationId) params.set('operation_id', filters.operationId);
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
