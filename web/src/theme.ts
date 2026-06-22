export const THEME_STORAGE_KEY = 'linux-panel-theme';

export type PanelThemeName = 'light' | 'dark';

const PANEL_THEMES: PanelThemeName[] = ['light', 'dark'];

let themeChangeTimer: number | undefined;

export function isPanelThemeName(value: unknown): value is PanelThemeName {
  return typeof value === 'string' && PANEL_THEMES.includes(value as PanelThemeName);
}

export function getInitialTheme(): PanelThemeName {
  if (typeof window === 'undefined') return 'light';

  const stored = window.localStorage.getItem(THEME_STORAGE_KEY);
  return isPanelThemeName(stored) ? stored : 'light';
}

export function persistTheme(name: PanelThemeName) {
  if (typeof window === 'undefined') return;

  window.localStorage.setItem(THEME_STORAGE_KEY, name);
}

export function syncThemeAttribute(name: PanelThemeName) {
  if (typeof document === 'undefined') return;

  document.documentElement.dataset.theme = name;
  document.documentElement.style.colorScheme = name;
}

export function markThemeChanging() {
  if (typeof document === 'undefined' || typeof window === 'undefined') return;

  document.documentElement.classList.add('theme-changing');
  if (themeChangeTimer) window.clearTimeout(themeChangeTimer);
  themeChangeTimer = window.setTimeout(() => {
    document.documentElement.classList.remove('theme-changing');
  }, 220);
}

