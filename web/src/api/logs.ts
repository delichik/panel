import { apiClient, type ApiRequestOptions } from './client';
import type { LogLinesPage, LogsQuery } from '@/types/events';

export function logsQuery(params: LogsQuery = {}) {
  const query = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value !== undefined && value !== '') query.set(key, String(value));
  }
  return query.size ? `?${query}` : '';
}

export const logsApi = {
  /** eventId is optional: omit for a global JSONL scan. */
  list: (params: LogsQuery = {}, options?: ApiRequestOptions) =>
    apiClient.get<LogLinesPage>(`/logs${logsQuery(params)}`, options),
};
