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
    ...overrides,
  };
}

describe('groupTasksByOperation', () => {
  it('groups tasks by operation_id and keeps trigger metadata visible', () => {
    const groups = groupTasksByOperation([
      task({ id: 'task-a', operationId: 'op-1', triggerType: 'user', triggerResourceType: 'container_service', createdAt: '2026-05-23T00:00:00Z' }),
      task({ id: 'task-b', operationId: 'op-1', triggerType: 'service_enable', createdAt: '2026-05-23T00:01:00Z' }),
      task({ id: 'task-c', operationId: '', triggerType: 'runtime_explorer', createdAt: '2026-05-23T00:02:00Z' }),
    ]);

    expect(groups).toHaveLength(2);
    expect(groups[0].operationId).toBe('task-c');
    expect(groups[0].triggerType).toBe('runtime_explorer');
    expect(groups[1].operationId).toBe('op-1');
    expect(groups[1].tasks.map((item) => item.id)).toEqual(['task-b', 'task-a']);
    expect(groups[1].triggerType).toBe('user');
    expect(groups[1].triggerResourceType).toBe('container_service');
  });
});
