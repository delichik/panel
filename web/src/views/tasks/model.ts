import type { TaskDto, TaskOperationGroup } from '@/types/tasks';

const terminal = new Set(['completed', 'failed', 'blocked', 'cancelled']);

export function groupTasksByOperation(tasks: TaskDto[]): TaskOperationGroup[] {
  const map = new Map<string, TaskDto[]>();
  tasks.forEach((task) => {
    const key = task.operationId || task.id;
    map.set(key, [...(map.get(key) ?? []), task]);
  });
  return [...map.entries()].map(([operationId, items]) => {
    const ordered = [...items].sort((a, b) => Date.parse(b.createdAt) - Date.parse(a.createdAt));
    const status = operationStatus(ordered);
    return {
      operationId,
      type: ordered[0]?.type ?? '',
      title: operationTitle(ordered),
      status,
      latestAt: ordered[0]?.createdAt ?? '',
      tasks: ordered,
      failed: ordered.filter((item) => ['failed', 'failed_retryable', 'blocked'].includes(item.status)).length,
      running: ordered.filter((item) => item.status === 'running').length,
      completed: ordered.filter((item) => item.status === 'completed').length,
      actionable: ordered.some((item) => item.allowRetry || item.allowRunNow),
    };
  }).sort((a, b) => Date.parse(b.latestAt) - Date.parse(a.latestAt));
}

export function operationStatus(tasks: TaskDto[]) {
  if (tasks.some((item) => item.status === 'running')) return 'running';
  if (tasks.some((item) => item.status === 'failed' || item.status === 'failed_retryable' || item.status === 'blocked')) return 'failed';
  if (tasks.some((item) => item.status === 'queued' || item.status === 'scheduled')) return 'queued';
  if (tasks.every((item) => terminal.has(item.status))) return 'completed';
  return 'unknown';
}

export function operationTitle(tasks: TaskDto[]) {
  const first = tasks[0];
  if (!first) return '';
  return first.summary || first.type.replace(/_/g, ' ');
}

export function taskTone(status: string): 'success' | 'warning' | 'danger' | 'info' | 'neutral' {
  if (status === 'completed') return 'success';
  if (status === 'running') return 'info';
  if (status === 'queued' || status === 'scheduled' || status === 'failed_retryable') return 'warning';
  if (status === 'failed' || status === 'blocked' || status === 'cancelled') return 'danger';
  return 'neutral';
}
