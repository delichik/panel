import type { TaskDto } from '@/types/api';

export interface TaskOperationGroup {
  operationId: string;
  triggerType: string;
  triggerResourceType: string;
  createdAt: string;
  status: TaskDto['status'];
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
      const trigger = sorted.find((task) => task.triggerType === 'user') ?? sorted.find((task) => task.triggerType) ?? sorted[0];
      return {
        operationId,
        triggerType: trigger?.triggerType || '',
        triggerResourceType: trigger?.triggerResourceType || '',
        createdAt: sorted[0]?.createdAt || '',
        status: summarizeStatus(sorted),
        tasks: sorted,
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
