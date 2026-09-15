/** Only server-provided correlation IDs may create a history link. Never infer one from a generic resource id. */
export function activityPath(value: unknown, depth = 0): string | undefined {
  if (!value || typeof value !== 'object' || depth > 3) return;
  const record = value as Record<string, unknown>;
  const string = (key: string) => typeof record[key] === 'string' && record[key] ? record[key] as string : undefined;
  const operationId = string('operationId');
  const eventId = string('acceptedEventId') || string('eventId');
  const executionId = string('executionId') || string('taskId') || string('initialTaskId');
  const query = new URLSearchParams();
  if (operationId) query.set('operationId', operationId);
  else if (eventId) query.set('eventId', eventId);
  else if (executionId) query.set('executionId', executionId);
  if (query.size) return `/activity?${query}`;
  for (const key of ['activity', 'details', 'data', 'result']) {
    const nested = activityPath(record[key], depth + 1);
    if (nested) return nested;
  }
}
