import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, it } from 'vitest';

const page = readFileSync(resolve(__dirname, 'index.vue'), 'utf8');

describe('TaskCenterPage', () => {
  it('uses task definition capabilities returned by the API', () => {
    expect(page).toContain('task.allowRunNow');
    expect(page).toContain('task.allowRetry');
    expect(page).not.toContain('runnableTaskTypes');
  });

  it('keeps the operation selector focused on identity and status', () => {
    expect(page).not.toContain('taskCounts');
    expect(page).not.toContain('class="task-kpis"');
    expect(page).not.toContain('class="operation-meta"');
    expect(page).not.toContain(':model-value="group.progress"');
    expect(page).toContain('class="operation-name"');
    expect(page).toContain('class="operation-context"');
    expect(page).toContain('formatTime(group.createdAt)');
    expect(page).toContain('operationObjectLabel(group)');
    expect(page).toContain('statusLabel(group.status)');
  });

  it('normalizes cleared search filters before applying them', () => {
    expect(page).toContain('function normalizeStatusFilter');
    expect(page).toContain('function normalizeOperationFilter');
    expect(page).toContain('statusFilter.value = normalizedStatus');
    expect(page).toContain('operationFilter.value = normalizedOperation');
    expect(page).toContain('appliedStatusFilter.value = [...normalizedStatus]');
    expect(page).toContain('appliedOperationFilter.value = normalizedOperation');
  });
});
