import { ApiError } from './client';
import { useI18n } from '@/i18n';
import { tasksApi } from './tasks';
import type { TaskDto } from '@/types/tasks';

const { t } = useI18n();

const terminalStatuses = new Set(['completed', 'failed', 'failed_retryable', 'blocked', 'cancelled']);
const POLL_INTERVAL_MS = 750;
const RETRY_DELAY_MS = 250;
const MAX_TRANSIENT_RETRIES = 2;

function sleep(ms: number, signal?: AbortSignal): Promise<void> {
  return new Promise((resolve) => {
    if (signal?.aborted) {
      resolve();
      return;
    }
    const timer = setTimeout(resolve, ms);
    signal?.addEventListener('abort', () => {
      clearTimeout(timer);
      resolve();
    }, { once: true });
  });
}

function abortedError(): ApiError {
  return new ApiError(t('api.taskWaitAborted'), 0, 'request_aborted');
}

/**
 * Polls a task until it reaches a terminal status. Pass an AbortSignal to stop
 * polling early (the promise rejects with code `request_aborted`). Transient
 * network failures are retried a limited number of times; other errors and
 * terminal failures propagate immediately.
 */
export async function waitForTask(taskId: string, timeoutMs = 90_000, signal?: AbortSignal): Promise<TaskDto> {
  const deadline = Date.now() + timeoutMs;
  let transientFailures = 0;
  while (Date.now() < deadline) {
    if (signal?.aborted) throw abortedError();
    try {
      const task = await tasksApi.get(taskId, { signal });
      transientFailures = 0;
      if (terminalStatuses.has(task.status)) {
        if (task.status !== 'completed') throw new Error(task.error || t('api.taskEndedStatus', { status: task.status }));
        return task;
      }
    } catch (error) {
      if (signal?.aborted || (error instanceof ApiError && error.code === 'request_aborted')) throw abortedError();
      const transient = error instanceof ApiError && (error.code === 'network_error' || error.code === 'request_timeout');
      if (!transient || transientFailures >= MAX_TRANSIENT_RETRIES) throw error;
      transientFailures += 1;
      await sleep(RETRY_DELAY_MS, signal);
      continue;
    }
    await sleep(POLL_INTERVAL_MS, signal);
  }
  throw new Error(t('api.taskTimeout'));
}