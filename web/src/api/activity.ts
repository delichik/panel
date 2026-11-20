/**
 * @deprecated Product HTTP `/api/v1/activity/*` is no longer registered.
 * Use `eventsApi` from `@/api/events` and `logsApi` from `@/api/logs` for process events and log lines.
 * `ActivityReceipt` types remain in `@/types/activity` for command acceptances.
 */
import type { ActivityQuery } from '@/types/activity';

const retired = (name: string): never => {
  throw new Error(`activityApi.${name} is retired; use eventsApi from @/api/events`);
};

export function activityQuery(params: ActivityQuery = {}) {
  const query = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value !== undefined && value !== '') query.set(key, String(value));
  }
  return query.size ? `?${query}` : '';
}

export const activityApi = {
  events: () => retired('events'),
  event: () => retired('event'),
  operations: () => retired('operations'),
  operation: () => retired('operation'),
  export: () => retired('export'),
  context: () => retired('context'),
  tail: () => retired('tail'),
  summary: () => retired('summary'),
  evidence: () => retired('evidence'),
};
