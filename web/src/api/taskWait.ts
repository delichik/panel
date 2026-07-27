import { tasksApi } from './tasks';
import type { TaskDto } from '@/types/tasks';

const terminalStatuses = new Set(['completed', 'failed', 'failed_retryable', 'blocked', 'cancelled']);

export async function waitForTask(taskId: string, timeoutMs = 90_000): Promise<TaskDto> {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    const task = await tasksApi.get(taskId);
    if (terminalStatuses.has(task.status)) {
      if (task.status !== 'completed') throw new Error(task.error || `Task ended with status ${task.status}.`);
      return task;
    }
    await new Promise((resolve) => setTimeout(resolve, 750));
  }
  throw new Error('Task did not finish before the refresh timeout.');
}
