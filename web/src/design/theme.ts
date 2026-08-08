import { computed, onBeforeUnmount, ref, watchEffect } from 'vue';

export type ThemeMode = 'system' | 'light' | 'dark';
export type ResolvedTheme = 'light' | 'dark';
export type ThemeScheme = 'lighthouse' | 'ocean';

const STORAGE_KEY = 'panel.theme.mode';
const SCHEME_STORAGE_KEY = 'panel.theme.scheme';
const fallbackMode: ThemeMode = 'system';
const fallbackScheme: ThemeScheme = 'lighthouse';
const storedMode = localStorage.getItem(STORAGE_KEY) as ThemeMode | null;
const storedScheme = localStorage.getItem(SCHEME_STORAGE_KEY) as ThemeScheme | null;
const mode = ref<ThemeMode>(storedMode === 'light' || storedMode === 'dark' || storedMode === 'system' ? storedMode : fallbackMode);
const scheme = ref<ThemeScheme>(storedScheme === 'lighthouse' || storedScheme === 'ocean' ? storedScheme : fallbackScheme);
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

  function setScheme(next: ThemeScheme) {
    scheme.value = next;
    localStorage.setItem(SCHEME_STORAGE_KEY, next);
  }

  watchEffect(() => {
    document.documentElement.dataset.theme = resolved.value;
    document.documentElement.dataset.scheme = scheme.value;
    document.documentElement.style.colorScheme = resolved.value;
  });

  onBeforeUnmount(() => media?.removeEventListener('change', handleMediaChange));

  return { mode, resolved, setMode, scheme, setScheme };
}
