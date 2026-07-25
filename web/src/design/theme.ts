import { computed, onBeforeUnmount, ref, watchEffect } from 'vue';

export type ThemeMode = 'system' | 'light' | 'dark';
export type ResolvedTheme = 'light' | 'dark';

const STORAGE_KEY = 'panel.theme.mode';
const fallbackMode: ThemeMode = 'system';
const storedMode = localStorage.getItem(STORAGE_KEY) as ThemeMode | null;
const mode = ref<ThemeMode>(storedMode === 'light' || storedMode === 'dark' || storedMode === 'system' ? storedMode : fallbackMode);
const media = window.matchMedia?.('(prefers-color-scheme: dark)');
const prefersDark = ref(media?.matches ?? false);

function handleMediaChange(event: MediaQueryListEvent) {
  prefersDark.value = event.matches;
}

media?.addEventListener('change', handleMediaChange);

export function useThemeMode() {
  const resolved = computed<ResolvedTheme>(() => (mode.value === 'system' ? (prefersDark.value ? 'dark' : 'light') : mode.value));

  function setMode(next: ThemeMode) {
    mode.value = next;
    localStorage.setItem(STORAGE_KEY, next);
  }

  watchEffect(() => {
    document.documentElement.dataset.theme = resolved.value;
    document.documentElement.style.colorScheme = resolved.value;
  });

  onBeforeUnmount(() => media?.removeEventListener('change', handleMediaChange));

  return { mode, resolved, setMode };
}
