import { apiClient } from './client';
import type { SystemEventDetailDto, SystemEventListParams, SystemEventListResult } from '@/types/systemEvents';

function id(value: string) {
  return encodeURIComponent(value);
}

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
  get(eventId: string) {
    return apiClient.get<SystemEventDetailDto>(`/system-events/${id(eventId)}`);
  },
};
