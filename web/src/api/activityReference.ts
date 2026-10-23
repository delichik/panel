/** Only server-provided correlation IDs may create a history link. Never infer one from a generic resource id. */
export function activityPath(value: unknown, depth = 0): string | undefined {
  if (!value || typeof value !== 'object' || depth > 3) return;
  const record = value as Record<string, unknown>;
  const string = (key: string) => typeof record[key] === 'string' && record[key] ? record[key] as string : undefined;
  const streamId = string('streamId');
  const operationId = string('operationId');
  const executionId = string('executionId') || string('taskId') || string('initialTaskId');
  const query = new URLSearchParams();
  if (streamId) query.set('streamId', streamId);
  else if (operationId) query.set('operationId', operationId);
  else if (executionId) query.set('executionId', executionId);
  // Toast / receipt opens diagnostic log contexts / lines for the correlated intent/execution.
  if (query.size) {
    if (!query.has('level')) query.set('level', 'warning,error');
    return `/logs?${query}`;
  }
  for (const key of ['activity', 'details', 'data', 'result']) {
    const nested = activityPath(record[key], depth + 1);
    if (nested) return nested;
  }
}

export function logsPath(query: Record<string, string | undefined>): string {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(query)) {
    if (value) params.set(key, value);
  }
  return params.size ? `/logs?${params}` : '/logs';
}

export function operationsPath(query: Record<string, string | undefined> = {}): string {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(query)) {
    if (value) params.set(key, value);
  }
  return params.size ? `/operations?${params}` : '/operations';
}

export function tasksPath(query: Record<string, string | undefined> = {}): string {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(query)) {
    if (value) params.set(key, value);
  }
  return params.size ? `/tasks?${params}` : '/tasks';
}
