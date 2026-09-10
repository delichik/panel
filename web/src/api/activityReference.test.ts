import { describe, expect, it } from 'vitest';
import { ApiError } from './client';
import { activityPath } from './activityReference';
describe('activity deep links', () => {
  it('prefers the complete operation and encodes untrusted IDs as query values', () => {
    expect(activityPath({ operationId: 'op/x & y', taskId: 'task-1' })).toBe('/activity?operationId=op%2Fx+%26+y');
    expect(activityPath({ taskId: 'task-1' })).toBe('/activity?executionId=task-1');
    expect(activityPath({ acceptedEventId: 'event-1' })).toBe('/activity?eventId=event-1');
  });
  it('preserves failure and polling-timeout links while rejecting resource IDs and free text', () => {
    expect(activityPath(new ApiError('timeout', 408, 'execution_wait_timeout', { taskId: 'task-1' }))).toBe('/activity?executionId=task-1');
    expect(activityPath({ id: 'resource-1' })).toBeUndefined();
    expect(activityPath('task-1')).toBeUndefined();
    const cycle: { details?: unknown } = {}; cycle.details = cycle;
    expect(activityPath(cycle)).toBeUndefined();
  });
});
