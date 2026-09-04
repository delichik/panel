import { describe, expect, it } from 'vitest';
import { resolveStatusTone, statusToneMaps } from './statusTone';
import type { TaskStatus } from '@/types/tasks';
import type { ApplicationOperationStatus } from '@/types/applicationOperations';

const taskStatuses: TaskStatus[] = ['queued', 'scheduled', 'running', 'completed', 'failed', 'failed_retryable', 'blocked', 'cancelled'];
const operationStatuses: ApplicationOperationStatus[] = ['queued', 'running', 'succeeded', 'failed', 'partial_failed', 'cancelled', 'superseded', 'consistent'];

describe('statusToneMaps', () => {
  it('covers every task status from types/tasks', () => {
    for (const status of taskStatuses) {
      expect(statusToneMaps.task, `task.${status}`).toHaveProperty(status);
    }
  });

  it('covers every application operation status from types/applicationOperations', () => {
    for (const status of operationStatuses) {
      expect(statusToneMaps.operation, `operation.${status}`).toHaveProperty(status);
    }
  });

  it('falls back to the generic map and then neutral for unknown statuses', () => {
    expect(statusToneMaps.task.pending).toBe('warning');
    expect(resolveStatusTone('task', 'active')).toBe('success');
    expect(resolveStatusTone('task', 'completely_unknown')).toBe('neutral');
  });
});