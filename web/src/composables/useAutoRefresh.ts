import { computed, onBeforeUnmount, ref, watch } from 'vue';

export type AutoRefreshMode = 'off' | '10m' | '30m' | '1h';

const STORAGE_KEY = 'panel.autoRefresh';
const fallback: AutoRefreshMode = '10m';
const validModes: AutoRefreshMode[] = ['off', '10m', '30m', '1h'];
const intervalByMode: Record<Exclude<AutoRefreshMode, 'off'>, number> = {
  '10m': 10 * 60 * 1000,
  '30m': 30 * 60 * 1000,
  '1h': 60 * 60 * 1000,
};
const stored = (globalThis.localStorage?.getItem(STORAGE_KEY) as AutoRefreshMode | null) ?? null;
// 旧版本可能存过 '5'/'10'（秒级），不再兼容，直接回落到默认 10 分钟。
const mode = ref<AutoRefreshMode>(stored && validModes.includes(stored) ? stored : fallback);
const enabled = computed(() => mode.value !== 'off');
const intervalMs = computed(() => (mode.value === 'off' ? 0 : intervalByMode[mode.value]));

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
