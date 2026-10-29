import type { LocationQuery, LocationQueryValue } from 'vue-router';

function one(value: LocationQueryValue | LocationQueryValue[]): string | undefined {
  if (typeof value === 'string' && value) return value;
  return undefined;
}

const passthrough = [
  'q', 'domain', 'level', 'trigger', 'resourceType', 'resourceId', 'executionId',
  'action', 'phase', 'result', 'attention', 'actorId', 'kind', 'stepId', 'from', 'to',
  'operationId', 'eventId', 'streamId', 'module', 'state', 'hadError', 'cursor', 'cursorTrail',
] as const;

/**
 * Map legacy `/activity` bookmarks into the events + logs product routes.
 * Historical `view=operations` meant reconcile intents → `/events` with filters preserved.
 */
export function resolveActivityRedirect(query: LocationQuery): { path: string; query: Record<string, string> } {
  const next: Record<string, string> = {};
  for (const key of passthrough) {
    const value = one(query[key]);
    if (value) next[key] = value;
  }
  if (next.streamId && !next.eventId) {
    next.eventId = next.streamId;
    delete next.streamId;
  }
  const view = one(query.view);
  if (view === 'operations') {
    return { path: '/events', query: next };
  }
  if (!next.level && next.eventId) {
    next.level = 'warning,error';
  }
  if (next.eventId) {
    return { path: '/logs', query: next };
  }
  if (next.operationId || next.executionId) {
    return { path: '/events', query: next };
  }
  return { path: '/logs', query: next };
}
