import { apiClient, fetchBlob, type ApiRequestOptions } from './client';
import type { Event, EventPage, EventQuery, LogLinesPage } from '@/types/events';

export function eventsQuery(params: EventQuery = {}) {
  const query = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value !== undefined && value !== '') query.set(key, String(value));
  }
  return query.size ? `?${query}` : '';
}

const id = encodeURIComponent;

export const eventsApi = {
  list: (params: EventQuery = {}, options?: ApiRequestOptions) =>
    apiClient.get<EventPage>(`/events${eventsQuery(params)}`, options),
  get: (eventId: string, options?: ApiRequestOptions) =>
    apiClient.get<Event>(`/events/${id(eventId)}`, options),
  logs: (eventId: string, params: EventQuery = {}, options?: ApiRequestOptions) =>
    apiClient.get<LogLinesPage>(`/events/${id(eventId)}/logs${eventsQuery(params)}`, options),
  clear: (options?: ApiRequestOptions) =>
    apiClient.post<{ cleared: boolean }>('/events/clear', { confirmation: 'CLEAR EVENTS AND LOGS' }, options),
  export: (params: EventQuery = {}) =>
    fetchBlob(`/api/v1/events/export${eventsQuery(params)}`, { fallbackFilename: 'events.jsonl' }),
};
