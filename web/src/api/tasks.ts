import { apiClient } from './client';
import type { TaskDto, TaskLogsDto, TaskStatus } from '@/types/api';

export interface TaskListFilters {
  status?: TaskStatus | 'all';
  serverId?: string;
  type?: string;
  limit?: number;
}

export const tasksApi = {
  list(filters: TaskListFilters = {}) {
    const params = new URLSearchParams({ limit: String(filters.limit ?? 50) });
    if (filters.status && filters.status !== 'all') params.set('status', filters.status);
    if (filters.serverId) params.set('serverId', filters.serverId);
    if (filters.type) params.set('type', filters.type);
    return apiClient.get<TaskDto[]>(`/tasks?${params.toString()}`);
  },
  get(taskId: string) {
    return apiClient.get<TaskDto>(`/tasks/${taskId}`);
  },
  logs(taskId: string, after = 0) {
    return apiClient.get<TaskLogsDto>(`/tasks/${taskId}/logs?after=${after}`);
  },
};
