import { eventsApi, eventsQuery } from './events';
import type { Event, EventPage, EventQuery, LogLinesPage } from '@/types/events';

/** @deprecated Use eventsApi from @/api/events */
export const logContextsQuery = eventsQuery;

/** @deprecated Use eventsApi from @/api/events */
export const logContextsApi = {
  list: (params: EventQuery = {}, options?: Parameters<typeof eventsApi.list>[1]) =>
    eventsApi.list(params, options) as Promise<EventPage>,
  get: (streamId: string, options?: Parameters<typeof eventsApi.get>[1]) =>
    eventsApi.get(streamId, options) as Promise<Event>,
  lines: (streamId: string, params: EventQuery = {}, options?: Parameters<typeof eventsApi.logs>[2]) =>
    eventsApi.logs(streamId, params, options) as Promise<LogLinesPage>,
  export: eventsApi.export,
};
