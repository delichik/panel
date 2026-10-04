import { computed, onBeforeUnmount, ref, watch } from 'vue';

export type AutoRefreshMode = 'off' | '5' | '10';

const STORAGE_KEY = 'panel.autoRefresh';
const fallback: AutoRefreshMode = '5';
const stored = (globalThis.localStorage?.getItem(STORAGE_KEY) as AutoRefreshMode | null) ?? null;
const mode = ref<AutoRefreshMode>(stored === 'off' || stored === '5' || stored === '10' ? stored : fallback);
const enabled = computed(() => mode.value !== 'off');
const intervalMs = computed(() => (mode.value === 'off' ? 0 : Number(mode.value) * 1000));

/**
 * Shared auto-refresh composable. `setMode` persists the global setting;
 * `start(callback)` / `stop()` drive one polling loop per consumer with
 * reentrancy protection and automatic pause/resume when the tab becomes
 * hidden/visible.
 */
export function useAutoRefresh() {
  let timer: ReturnType<typeof setInterval> | undefined;
  let inFlight = false;
  let callback: (() => void | Promise<void>) | null = null;
  let started = false;

  function clearTimer() {
    if (timer !== undefined) {
      clearInterval(timer);
      timer = undefined;
    }
  }

  function schedule() {
    clearTimer();
    if (!started || !enabled.value || document.visibilityState !== 'visible') return;
    timer = setInterval(() => {
      void tick();
    }, intervalMs.value);
  }

  async function tick() {
    if (inFlight || document.visibilityState !== 'visible' || !callback) return;
    inFlight = true;
    try {
      await callback();
    } finally {
      inFlight = false;
    }
  }

  function handleVisibilityChange() {
    if (document.visibilityState === 'visible') {
      schedule();
      void tick();
    } else {
      clearTimer();
    }
  }

  function start(nextCallback: () => void | Promise<void>) {
    callback = nextCallback;
    started = true;
    schedule();
  }

  function stop() {
    started = false;
    callback = null;
    clearTimer();
  }

  function setMode(next: AutoRefreshMode) {
    mode.value = next;
    globalThis.localStorage?.setItem(STORAGE_KEY, next);
  }

  watch(mode, schedule);
  document.addEventListener('visibilitychange', handleVisibilityChange);
  onBeforeUnmount(() => {
    stop();
    document.removeEventListener('visibilitychange', handleVisibilityChange);
  });

  return { mode, enabled, intervalMs, setMode, start, stop };
}