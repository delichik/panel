import type { TaskDto } from '@/types/api';

export interface TaskOperationGroup {
  operationId: string;
  executionMode: string;
  triggerType: string;
  triggerResourceType: string;
  triggerResourceId: string;
  resourceType: string;
  resourceId: string;
  summary: string;
  createdAt: string;
  startedAt: string | null;
  finishedAt: string | null;
  nextRunAt: string | null;
  status: TaskDto['status'];
  progress: number;
  activeCount: number;
  failedCount: number;
  allTasks: TaskDto[];
  tasks: TaskDto[];
}

export function groupTasksByOperation(tasks: TaskDto[]): TaskOperationGroup[] {
  const groups = new Map<string, TaskDto[]>();
  for (const task of tasks) {
    const key = task.operationId || task.id;
    groups.set(key, [...(groups.get(key) ?? []), task]);
  }

  return Array.from(groups.entries())
    .map(([operationId, rows]) => {
      const sorted = rows.slice().sort((a, b) => b.createdAt.localeCompare(a.createdAt));
      const childRows = sorted
        .filter((task) => task.parentTaskId)
        .sort((a, b) => (a.childIndex || 0) - (b.childIndex || 0) || b.createdAt.localeCompare(a.createdAt));
      const parent = sorted.find((task) => !task.parentTaskId && task.childCount && task.childCount > 0);
      const taskRows = childRows.length > 0 ? (parent ? [parent, ...childRows] : childRows) : sorted;
      const trigger = sorted.find((task) => task.triggerType === 'user') ?? sorted.find((task) => task.triggerType) ?? sorted[0];
      const contextTask = childRows[0] ?? trigger ?? sorted[0];
      return {
        operationId,
        executionMode: parent?.executionMode || trigger?.executionMode || '',
        triggerType: trigger?.triggerType || '',
        triggerResourceType: trigger?.triggerResourceType || '',
        triggerResourceId: trigger?.triggerResourceId || '',
        resourceType: contextTask?.resourceType || trigger?.resourceType || '',
        resourceId: contextTask?.resourceId || trigger?.resourceId || '',
        summary: trigger?.summary || sorted[0]?.summary || '',
        createdAt: sorted[0]?.createdAt || '',
        startedAt: earliest(sorted.map((task) => task.startedAt).filter(Boolean) as string[]),
        finishedAt: latest(sorted.map((task) => task.finishedAt).filter(Boolean) as string[]),
        nextRunAt: earliest(sorted.map((task) => task.nextRunAt).filter(Boolean) as string[]),
        status: summarizeStatus(sorted),
        progress: summarizeProgress(sorted),
        activeCount: taskRows.filter((task) => ['queued', 'scheduled', 'running', 'failed_retryable'].includes(task.status)).length,
        failedCount: taskRows.filter((task) => ['failed', 'blocked'].includes(task.status)).length,
        allTasks: sorted,
        tasks: taskRows,
      };
    })
    .sort((a, b) => b.createdAt.localeCompare(a.createdAt));
}

function summarizeStatus(tasks: TaskDto[]): TaskDto['status'] {
  if (tasks.some((task) => task.status === 'running')) return 'running';
  if (tasks.some((task) => task.status === 'failed')) return 'failed';
  if (tasks.some((task) => task.status === 'failed_retryable')) return 'failed_retryable';
  if (tasks.some((task) => task.status === 'blocked')) return 'blocked';
  if (tasks.some((task) => task.status === 'queued' || task.status === 'scheduled')) return 'queued';
  if (tasks.every((task) => task.status === 'completed')) return 'completed';
  return tasks[0]?.status ?? 'queued';
}

function summarizeProgress(tasks: TaskDto[]): number {
  if (tasks.length === 0) return 0;
  const values = tasks.map((task) => (task.status === 'completed' ? 100 : task.percentage ?? 0));
  return Math.round(values.reduce((sum, value) => sum + value, 0) / values.length);
}

function earliest(values: string[]): string | null {
  if (values.length === 0) return null;
  return values.slice().sort((a, b) => a.localeCompare(b))[0];
}

function latest(values: string[]): string | null {
  if (values.length === 0) return null;
  return values.slice().sort((a, b) => b.localeCompare(a))[0];
}
