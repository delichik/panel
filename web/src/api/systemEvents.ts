import { apiClient } from './client';
import type { SystemEventListParams, SystemEventListResult } from '@/types/systemEvents';

function query(params: SystemEventListParams) {
  const search = new URLSearchParams();
  Object.entries(params).forEach(([key, value]) => {
    if (value !== undefined && value !== '') search.set(key, String(value));
  });
  return search.size ? `?${search}` : '';
}

export const systemEventsApi = {
  list(params: SystemEventListParams = {}) {
    return apiClient.get<SystemEventListResult>(`/system-events${query(params)}`);
  },
};