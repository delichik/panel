import { apiClient, type ApiRequestOptions } from './client';
import type { TaskDto, TaskListResult, TaskLogsResult, TaskStep } from '@/types/tasks';

function id(value: string) {
  return encodeURIComponent(value);
}

export const tasksApi = {
  list(params: { status?: string; type?: string; page?: number; pageSize?: number; operationPage?: boolean; q?: string } = {}) {
    const query = new URLSearchParams();
    Object.entries(params).forEach(([key, value]) => {
      if (value !== undefined && value !== '' && value !== false) query.set(key, String(value));
    });
    return apiClient.get<TaskListResult>(`/tasks${query.size ? `?${query}` : ''}`);
  },
  get(taskId: string, options?: ApiRequestOptions) {
    return apiClient.get<TaskDto>(`/tasks/${id(taskId)}`, options);
  },
  steps(taskId: string) {
    return apiClient.get<TaskStep[]>(`/tasks/${id(taskId)}/steps`);
  },
  logs(taskId: string, after = 0) {
    const query = after ? `?after=${after}` : '';
    return apiClient.get<TaskLogsResult>(`/tasks/${id(taskId)}/logs${query}`);
  },
  retry(taskId: string) {
    return apiClient.post<TaskDto>(`/tasks/${id(taskId)}/retry`);
  },
  runNow(taskId: string) {
    return apiClient.post<TaskDto>(`/tasks/${id(taskId)}/run-now`);
  },
};