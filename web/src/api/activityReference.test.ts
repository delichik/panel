import { describe, expect, it } from 'vitest';
import { ApiError } from './client';
import { activityPath } from './activityReference';
describe('activity deep links', () => {
  it('prefers streamId, then operation, then execution and encodes untrusted IDs', () => {
    expect(activityPath({ streamId: 's/1', operationId: 'op/x' })).toBe('/logs?streamId=s%2F1&level=warning%2Cerror');
    expect(activityPath({ operationId: 'op/x & y', taskId: 'task-1' })).toBe('/logs?operationId=op%2Fx+%26+y&level=warning%2Cerror');
    expect(activityPath({ taskId: 'task-1' })).toBe('/logs?executionId=task-1&level=warning%2Cerror');
  });
  it('preserves failure and polling-timeout links while rejecting resource IDs and free text', () => {
    expect(activityPath(new ApiError('timeout', 408, 'execution_wait_timeout', { taskId: 'task-1' }))).toBe('/logs?executionId=task-1&level=warning%2Cerror');
    expect(activityPath({ id: 'resource-1' })).toBeUndefined();
    expect(activityPath('task-1')).toBeUndefined();
    const cycle: { details?: unknown } = {}; cycle.details = cycle;
    expect(activityPath(cycle)).toBeUndefined();
  });
});
