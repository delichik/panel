import { apiClient, fetchBlob, type ApiRequestOptions } from './client';
import type { ActivityContext, ActivityEvent, ActivityOperation, ActivityOperationDetail, ActivityPage, ActivityQuery, ActivitySummary } from '@/types/activity';

export function activityQuery(params: ActivityQuery = {}) {
  const query = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value !== undefined && value !== '') query.set(key, String(value));
  }
  return query.size ? `?${query}` : '';
}
const id = encodeURIComponent;
export const activityApi = {
  events: (params: ActivityQuery = {}, options?: ApiRequestOptions) => apiClient.get<ActivityPage<ActivityEvent>>(`/activity/events${activityQuery(params)}`, options),
  event: (eventId: string, options?: ApiRequestOptions) => apiClient.get<ActivityEvent>(`/activity/events/${id(eventId)}`, options),
  context: (eventId: string, params: ActivityQuery = {}) => apiClient.get<ActivityContext>(`/activity/events/${id(eventId)}/context${activityQuery(params)}`),
  operations: (params: ActivityQuery = {}) => apiClient.get<ActivityPage<ActivityOperation>>(`/activity/operations${activityQuery(params)}`),
  operation: (operationId: string, params: ActivityQuery = {}) => apiClient.get<ActivityOperationDetail>(`/activity/operations/${id(operationId)}${activityQuery(params)}`),
  tail: (params: ActivityQuery = {}) => apiClient.get<ActivityPage<ActivityEvent>>(`/activity/tail${activityQuery(params)}`),
  summary: (params: ActivityQuery = {}) => apiClient.get<ActivitySummary>(`/activity/summary${activityQuery(params)}`),
  export: (params: ActivityQuery = {}) => fetchBlob(`/api/v1/activity/export${activityQuery(params)}`, { fallbackFilename: 'activity.jsonl' }),
  evidence: (evidenceId: string) => fetchBlob(`/api/v1/activity/evidence/${id(evidenceId)}`, { fallbackFilename: 'evidence.bin' }),
};
