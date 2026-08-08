import { computed, ref } from 'vue';

export type AutoRefreshMode = 'off' | '5' | '10';

const STORAGE_KEY = 'panel.autoRefresh';
const fallback: AutoRefreshMode = '5';
const stored = (globalThis.localStorage?.getItem(STORAGE_KEY) as AutoRefreshMode | null) ?? null;
const mode = ref<AutoRefreshMode>(stored === 'off' || stored === '5' || stored === '10' ? stored : fallback);
const enabled = computed(() => mode.value !== 'off');
const intervalMs = computed(() => (mode.value === 'off' ? 0 : Number(mode.value) * 1000));

export function useAutoRefresh() {
  function setMode(next: AutoRefreshMode) {
    mode.value = next;
    globalThis.localStorage?.setItem(STORAGE_KEY, next);
  }
  return { mode, enabled, intervalMs, setMode };
}
