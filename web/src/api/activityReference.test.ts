import { describe, expect, it } from 'vitest';
import { ApiError } from './client';
import { activityPath } from './activityReference';
describe('activity deep links', () => {
  it('prefers eventId for logs and falls back to events for operation or execution', () => {
    expect(activityPath({ eventId: 'evt-1', operationId: 'op/x' })).toBe('/logs?eventId=evt-1&level=warning%2Cerror');
    expect(activityPath({ streamId: 's/1', operationId: 'op/x' })).toBe('/logs?eventId=s%2F1&level=warning%2Cerror');
    expect(activityPath({ operationId: 'op/x & y', taskId: 'task-1' })).toBe('/events?operationId=op%2Fx+%26+y');
    expect(activityPath({ taskId: 'task-1' })).toBe('/events?executionId=task-1');
  });
  it('preserves failure and polling-timeout links while rejecting resource IDs and free text', () => {
    expect(activityPath(new ApiError('timeout', 408, 'execution_wait_timeout', { taskId: 'task-1' }))).toBe('/events?executionId=task-1');
    expect(activityPath({ id: 'resource-1' })).toBeUndefined();
    expect(activityPath('task-1')).toBeUndefined();
    const cycle: { details?: unknown } = {}; cycle.details = cycle;
    expect(activityPath(cycle)).toBeUndefined();
  });
});
