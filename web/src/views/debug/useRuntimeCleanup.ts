import { computed, onBeforeUnmount, onMounted, ref } from 'vue';
import { debugApi, type ClearRuntimeDataStatus } from '@/api/debug';

const pendingKey = 'panel.debug.pendingCleanup';
const pollDelay = 1500;
const retryDelay = 5000;

export function useRuntimeCleanup(options: {
  onTerminal: (result: ClearRuntimeDataStatus) => void;
  onRequestError: (error: unknown) => void;
}) {
  const status = ref<ClearRuntimeDataStatus | null>(null);
  const checking = ref(false);
  const submitting = ref(false);
  const queryFailed = ref(false);
  const resultMissing = ref(false);
  const checked = ref(false);
  let disposed = false;
  let pollTimer: ReturnType<typeof setTimeout> | undefined;
  let controller: AbortController | undefined;
  let consecutiveFailures = 0;
  let pendingRun = '';
  try { pendingRun = sessionStorage.getItem(pendingKey) || ''; } catch { /* Storage is optional. */ }

  const running = computed(() => status.value?.running === true);
  const canStart = computed(() => checked.value && !queryFailed.value && !running.value && !submitting.value && !checking.value);

  function remember(runId: string) {
    pendingRun = runId;
    try {
      if (runId) sessionStorage.setItem(pendingKey, runId);
      else sessionStorage.removeItem(pendingKey);
    } catch { /* Status queries remain authoritative without browser storage. */ }
  }

  function stopTimer() {
    if (pollTimer !== undefined) clearTimeout(pollTimer);
    pollTimer = undefined;
  }

  function schedule(delay: number) {
    stopTimer();
    if (disposed || document.visibilityState === 'hidden' || consecutiveFailures >= 3) return;
    pollTimer = setTimeout(() => { void refresh(); }, delay);
  }

  function accept(next: ClearRuntimeDataStatus) {
    const wasPending = Boolean(pendingRun);
    const sameRun = pendingRun === 'pending' || pendingRun === next.runId;
    status.value = next;
    checked.value = true;
    queryFailed.value = false;
    consecutiveFailures = 0;
    resultMissing.value = next.status === 'idle' && wasPending;
    if (next.running) {
      remember(next.runId || 'pending');
      schedule(pollDelay);
    } else {
      stopTimer();
      if (next.status === 'succeeded' || next.status === 'failed') {
        remember('');
        if (wasPending && sameRun) options.onTerminal(next);
      }
    }
  }

  async function request<T>(action: (signal: AbortSignal) => Promise<T>) {
    const current = new AbortController();
    controller = current;
    // ApiClient leaves deadlines to callers when an external signal is used.
    const deadline = setTimeout(() => current.abort(), 15000);
    try { return await action(current.signal); }
    finally {
      clearTimeout(deadline);
      if (controller === current) controller = undefined;
    }
  }

  async function refresh() {
    if (disposed || checking.value || submitting.value) return;
    checking.value = true;
    stopTimer();
    try {
      const next = await request(signal => debugApi.clearRuntimeDataStatus(signal));
      if (!disposed) accept(next);
    } catch {
      if (disposed) return;
      // A failed status request says nothing about whether the server cleared
      // data. Keep the last snapshot and offer a read-only status retry.
      queryFailed.value = true;
      consecutiveFailures++;
      schedule(retryDelay);
    } finally {
      if (!disposed) checking.value = false;
    }
  }

  async function start() {
    if (!canStart.value) return false;
    stopTimer();
    submitting.value = true;
    remember('pending');
    resultMissing.value = false;
    let accepted = false;
    try {
      const next = await request(signal => debugApi.clearRuntimeData(signal));
      if (disposed) return false;
      accept(next);
      accepted = true;
    } catch (error) {
      if (disposed) return false;
      queryFailed.value = true;
      options.onRequestError(error);
    } finally {
      if (!disposed) submitting.value = false;
    }
    // Never repeat POST after a lost response; establish server state via GET.
    if (!accepted && !disposed) await refresh();
    return accepted;
  }

  function onVisibilityChange() {
    if (document.visibilityState === 'hidden') stopTimer();
    else { consecutiveFailures = 0; void refresh(); }
  }

  onMounted(() => {
    void refresh();
    document.addEventListener('visibilitychange', onVisibilityChange);
  });
  onBeforeUnmount(() => {
    disposed = true;
    stopTimer();
    controller?.abort();
    document.removeEventListener('visibilitychange', onVisibilityChange);
  });

  return { status, checking, submitting, running, canStart, queryFailed, resultMissing, refresh, start };
}
