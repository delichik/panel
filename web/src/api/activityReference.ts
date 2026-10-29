/** Only server-provided correlation IDs may create a history link. Never infer one from a generic resource id. */
export function activityPath(value: unknown, depth = 0): string | undefined {
  if (!value || typeof value !== 'object' || depth > 3) return;
  const record = value as Record<string, unknown>;
  const string = (key: string) => typeof record[key] === 'string' && record[key] ? record[key] as string : undefined;
  const eventId = string('eventId') || string('streamId');
  const operationId = string('operationId');
  const executionId = string('executionId') || string('taskId') || string('initialTaskId');
  const query = new URLSearchParams();
  if (eventId) {
    query.set('eventId', eventId);
    if (!query.has('level')) query.set('level', 'warning,error');
    return `/logs?${query}`;
  }
  if (operationId) query.set('operationId', operationId);
  else if (executionId) query.set('executionId', executionId);
  if (query.size) {
    return `/events?${query}`;
  }
  for (const key of ['activity', 'details', 'data', 'result']) {
    const nested = activityPath(record[key], depth + 1);
    if (nested) return nested;
  }
}

export function logsPath(query: Record<string, string | undefined>): string {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(query)) {
    if (value === undefined || value === '') continue;
    if (key === 'streamId') params.set('eventId', value);
    else params.set(key, value);
  }
  return params.size ? `/logs?${params}` : '/logs';
}

export function eventsPath(query: Record<string, string | undefined> = {}): string {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(query)) {
    if (value) params.set(key, value);
  }
  return params.size ? `/events?${params}` : '/events';
}

/** @deprecated Use eventsPath */
export const operationsPath = eventsPath;

/** @deprecated Use eventsPath */
export const tasksPath = eventsPath;
