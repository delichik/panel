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

/**
 * Map legacy `/activity` bookmarks into the split product routes.
 * Historical `view=operations` meant app/facility reconcile intents → `/tasks`
 * (not user `/operations`). Everything else → `/logs` for diagnose.
 */
export function resolveActivityRedirect(query: LocationQuery): { path: string; query: Record<string, string> } {
  const next: Record<string, string> = {};
  for (const key of passthrough) {
    const value = one(query[key]);
    if (value) next[key] = value;
  }
  const view = one(query.view);
  if (view === 'operations') {
    return { path: '/tasks', query: next };
  }
  if (!next.level && (next.operationId || next.executionId || next.eventId)) {
    next.level = 'warning,error';
  }
  return { path: '/logs', query: next };
}
