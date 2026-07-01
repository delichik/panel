import { groupTasksByOperation } from './taskOperations';
import type { TaskDto } from '@/types/api';

function task(overrides: Partial<TaskDto>): TaskDto {
  return {
    id: 'task-1',
    operationId: '',
    type: 'service_reconcile',
    serverId: null,
    nodeId: null,
    status: 'queued',
    stage: 'queued',
    percentage: 0,
    summary: '',
    retryCount: 0,
    maxRetries: 0,
    createdAt: '2026-05-23T00:00:00Z',
    startedAt: null,
    finishedAt: null,
    allowRunNow: false,
    allowRetry: false,
    ...overrides,
  };
}

describe('groupTasksByOperation', () => {
  it('groups tasks by operation_id and keeps trigger metadata visible', () => {
    const groups = groupTasksByOperation([
      task({ id: 'task-a', operationId: 'op-1', triggerType: 'user', triggerResourceType: 'application', createdAt: '2026-05-23T00:00:00Z' }),
      task({ id: 'task-b', operationId: 'op-1', triggerType: 'application_sync', status: 'completed', percentage: null, createdAt: '2026-05-23T00:01:00Z', startedAt: '2026-05-23T00:01:10Z', finishedAt: '2026-05-23T00:02:00Z' }),
      task({ id: 'task-c', operationId: '', triggerType: 'application_runtime', createdAt: '2026-05-23T00:02:00Z' }),
    ]);

    expect(groups).toHaveLength(2);
    expect(groups[0].operationId).toBe('task-c');
    expect(groups[0].triggerType).toBe('application_runtime');
    expect(groups[1].operationId).toBe('op-1');
    expect(groups[1].tasks.map((item) => item.id)).toEqual(['task-b', 'task-a']);
    expect(groups[1].triggerType).toBe('user');
    expect(groups[1].triggerResourceType).toBe('application');
    expect(groups[1].progress).toBe(50);
    expect(groups[1].startedAt).toBe('2026-05-23T00:01:10Z');
    expect(groups[1].finishedAt).toBe('2026-05-23T00:02:00Z');
  });

  it('keeps the batch parent visible before child tasks', () => {
    const groups = groupTasksByOperation([
      task({ id: 'task-parent', operationId: 'op-2', childCount: 2, executionMode: 'parallel', status: 'failed', createdAt: '2026-05-23T00:00:00Z' }),
      task({ id: 'task-a', operationId: 'op-2', parentTaskId: 'task-parent', childIndex: 2, childCount: 2, resourceType: 'application', resourceId: 'app-web', status: 'completed', createdAt: '2026-05-23T00:00:01Z' }),
      task({ id: 'task-b', operationId: 'op-2', parentTaskId: 'task-parent', childIndex: 1, childCount: 2, resourceType: 'application', resourceId: 'app-api', status: 'completed', createdAt: '2026-05-23T00:00:02Z' }),
    ]);

    expect(groups).toHaveLength(1);
    expect(groups[0].executionMode).toBe('parallel');
    expect(groups[0].status).toBe('failed');
    expect(groups[0].resourceType).toBe('application');
    expect(groups[0].resourceId).toBe('app-api');
    expect(groups[0].allTasks.map((item) => item.id)).toEqual(['task-b', 'task-a', 'task-parent']);
    expect(groups[0].tasks.map((item) => item.id)).toEqual(['task-parent', 'task-b', 'task-a']);
  });
});
