import type { LocationQuery, LocationQueryValue } from 'vue-router';

function one(value: LocationQueryValue | LocationQueryValue[]): string | undefined {
  if (typeof value === 'string' && value) return value;
  return undefined;
}

const passthrough = [
  'q', 'domain', 'level', 'trigger', 'resourceType', 'resourceId', 'executionId',
  'action', 'phase', 'result', 'attention', 'actorId', 'kind', 'stepId', 'from', 'to',
  'operationId', 'eventId', 'cursor', 'snapshotSeq', 'cursorTrail',
] as const;

/** Map legacy `/activity` query into `/operations`, `/tasks`, or `/logs`. */
export function resolveActivityRedirect(query: LocationQuery): { path: string; query: Record<string, string> } {
  const next: Record<string, string> = {};
  for (const key of passthrough) {
    const value = one(query[key]);
    if (value) next[key] = value;
  }
  const view = one(query.view);
  // Historical "by operation" deep links from apps/facilities targeted reconcile intents.
  if (view === 'operations') {
    return { path: '/tasks', query: next };
  }
  return { path: '/logs', query: next };
}
