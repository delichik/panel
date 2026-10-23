import { apiClient, fetchBlob, type ApiRequestOptions } from './client';
import type { LogContext, LogContextLinesPage, LogContextPage, LogContextQuery } from '@/types/logContexts';

export function logContextsQuery(params: LogContextQuery = {}) {
  const query = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value !== undefined && value !== '') query.set(key, String(value));
  }
  return query.size ? `?${query}` : '';
}

const id = encodeURIComponent;

export const logContextsApi = {
  list: (params: LogContextQuery = {}, options?: ApiRequestOptions) =>
    apiClient.get<LogContextPage>(`/log-contexts${logContextsQuery(params)}`, options),
  get: (streamId: string, options?: ApiRequestOptions) =>
    apiClient.get<LogContext>(`/log-contexts/${id(streamId)}`, options),
  lines: (streamId: string, params: LogContextQuery = {}, options?: ApiRequestOptions) =>
    apiClient.get<LogContextLinesPage>(`/log-contexts/${id(streamId)}/lines${logContextsQuery(params)}`, options),
  export: (params: LogContextQuery = {}) =>
    fetchBlob(`/api/v1/log-contexts/export${logContextsQuery(params)}`, { fallbackFilename: 'log-contexts.jsonl' }),
};
