import { describe, expect, it } from 'vitest';
import type { TaskDto } from '@/types/tasks';
import { groupTasksByOperation, operationStatus } from './model';

function task(id: string, operationId: string, status: string): TaskDto {
  return {
    id,
    operationId,
    type: 'demo_task',
    status,
    stage: status,
    summary: id,
    retryCount: 0,
    maxRetries: 3,
    createdAt: id.endsWith('2') ? '2026-07-21T08:02:00.000Z' : '2026-07-21T08:01:00.000Z',
    allowRunNow: status === 'queued',
    allowRetry: status === 'failed',
    allowCancel: true,
  };
}

describe('task center model', () => {
  it('groups concrete tasks by operation and exposes actionable state', () => {
    const groups = groupTasksByOperation([task('task-1', 'op-a', 'completed'), task('task-2', 'op-a', 'failed'), task('task-3', 'op-b', 'running')]);

    expect(groups).toHaveLength(2);
    expect(groups.find((group) => group.operationId === 'op-a')?.failed).toBe(1);
    expect(groups.find((group) => group.operationId === 'op-a')?.actionable).toBe(true);
    expect(groups[0].latestAt).toBe('2026-07-21T08:02:00.000Z');
  });

  it('prioritizes running and failed statuses for operation status', () => {
    expect(operationStatus([task('task-1', 'op-a', 'completed'), task('task-2', 'op-a', 'running')])).toBe('running');
    expect(operationStatus([task('task-1', 'op-a', 'completed'), task('task-2', 'op-a', 'failed')])).toBe('failed');
    expect(operationStatus([task('task-1', 'op-a', 'completed')])).toBe('completed');
  });
});
