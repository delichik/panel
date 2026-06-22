import { reactive } from 'vue';

export const LEGACY_THEME_STORAGE_KEY = 'linux-panel-theme';
export const THEME_PREFERENCES_STORAGE_KEY = 'linux-panel-theme-preferences';

export type PanelThemeName = 'light' | 'dark';
export type PanelThemeMode = 'system' | PanelThemeName;
export type PanelThemePreset = 'blue' | 'green' | 'red' | 'orange' | 'purple' | 'pink' | 'yellow';

export interface PanelThemeSemanticColors {
  primary: string;
  onPrimary: string;
  surfaceVariant: string;
}

export interface PanelThemePreferences {
  mode: PanelThemeMode;
  sharedPreset: boolean;
  preset: PanelThemePreset;
  lightPreset: PanelThemePreset;
  darkPreset: PanelThemePreset;
}

export const DEFAULT_THEME_PRESET: PanelThemePreset = 'green';

export const PANEL_THEME_PRESETS: Record<PanelThemePreset, Record<PanelThemeName, PanelThemeSemanticColors>> = {
  blue: {
    light: { primary: '#2563eb', onPrimary: '#ffffff', surfaceVariant: '#eaf1ff' },
    dark: { primary: '#60a5fa', onPrimary: '#071a33', surfaceVariant: '#1b2a3d' },
  },
  green: {
    light: { primary: '#0f766e', onPrimary: '#ffffff', surfaceVariant: '#eef3ed' },
    dark: { primary: '#2dd4bf', onPrimary: '#052e2a', surfaceVariant: '#202720' },
  },
  red: {
    light: { primary: '#dc2626', onPrimary: '#ffffff', surfaceVariant: '#fbeaea' },
    dark: { primary: '#fb7185', onPrimary: '#3b0711', surfaceVariant: '#382126' },
  },
  orange: {
    light: { primary: '#c2410c', onPrimary: '#ffffff', surfaceVariant: '#fff0e6' },
    dark: { primary: '#fb923c', onPrimary: '#351307', surfaceVariant: '#38271e' },
  },
  purple: {
    light: { primary: '#7c3aed', onPrimary: '#ffffff', surfaceVariant: '#f1eafd' },
    dark: { primary: '#c084fc', onPrimary: '#27103d', surfaceVariant: '#2d2438' },
  },
  pink: {
    light: { primary: '#db2777', onPrimary: '#ffffff', surfaceVariant: '#fceaf2' },
    dark: { primary: '#f472b6', onPrimary: '#3b0924', surfaceVariant: '#38232f' },
  },
  yellow: {
    light: { primary: '#ca8a04', onPrimary: '#211600', surfaceVariant: '#fff7d6' },
    dark: { primary: '#facc15', onPrimary: '#2b2100', surfaceVariant: '#36301d' },
  },
};

const PANEL_THEMES: PanelThemeName[] = ['light', 'dark'];
const PANEL_THEME_MODES: PanelThemeMode[] = ['system', ...PANEL_THEMES];
export const PANEL_THEME_PRESET_NAMES = Object.keys(PANEL_THEME_PRESETS) as PanelThemePreset[];

let themeChangeTimer: number | undefined;
let systemThemeQuery: MediaQueryList | undefined;
let systemThemeListener: ((event: MediaQueryListEvent) => void) | undefined;

function defaultThemePreferences(): PanelThemePreferences {
  return {
    mode: 'system',
    sharedPreset: true,
    preset: DEFAULT_THEME_PRESET,
    lightPreset: DEFAULT_THEME_PRESET,
    darkPreset: DEFAULT_THEME_PRESET,
  };
}

export function isPanelThemeName(value: unknown): value is PanelThemeName {
  return typeof value === 'string' && PANEL_THEMES.includes(value as PanelThemeName);
}

export function isPanelThemeMode(value: unknown): value is PanelThemeMode {
  return typeof value === 'string' && PANEL_THEME_MODES.includes(value as PanelThemeMode);
}

export function isPanelThemePreset(value: unknown): value is PanelThemePreset {
  return typeof value === 'string' && PANEL_THEME_PRESET_NAMES.includes(value as PanelThemePreset);
}

function normalizeThemePreferences(value: unknown): PanelThemePreferences | null {
  if (!value || typeof value !== 'object') return null;

  const input = value as Partial<PanelThemePreferences>;
  if (
    !isPanelThemeMode(input.mode)
    || typeof input.sharedPreset !== 'boolean'
    || !isPanelThemePreset(input.preset)
    || !isPanelThemePreset(input.lightPreset)
    || !isPanelThemePreset(input.darkPreset)
  ) {
    return null;
  }

  return {
    mode: input.mode,
    sharedPreset: input.sharedPreset,
    preset: input.preset,
    lightPreset: input.lightPreset,
    darkPreset: input.darkPreset,
  };
}

function migrateCustomColorPreferences(value: unknown): PanelThemePreferences | null {
  if (!value || typeof value !== 'object') return null;

  const input = value as { mode?: unknown; sharedColor?: unknown };
  if (!isPanelThemeMode(input.mode) || typeof input.sharedColor !== 'boolean') return null;

  return {
    ...defaultThemePreferences(),
    mode: input.mode,
    sharedPreset: input.sharedColor,
  };
}

export function loadThemePreferences(): PanelThemePreferences {
  if (typeof window === 'undefined') return defaultThemePreferences();

  const stored = window.localStorage.getItem(THEME_PREFERENCES_STORAGE_KEY);
  if (stored) {
    try {
      const parsed = JSON.parse(stored);
      const normalized = normalizeThemePreferences(parsed);
      if (normalized) return normalized;

      const migrated = migrateCustomColorPreferences(parsed);
      if (migrated) {
        persistThemePreferences(migrated);
        return migrated;
      }
    } catch {
      // Invalid local preferences fall through to safe defaults.
    }
  }

  const legacyTheme = window.localStorage.getItem(LEGACY_THEME_STORAGE_KEY);
  if (isPanelThemeName(legacyTheme)) {
    const migrated: PanelThemePreferences = {
      ...defaultThemePreferences(),
      mode: legacyTheme,
      sharedPreset: false,
    };
    persistThemePreferences(migrated);
    window.localStorage.removeItem(LEGACY_THEME_STORAGE_KEY);
    return migrated;
  }

  return defaultThemePreferences();
}

export function persistThemePreferences(preferences: PanelThemePreferences) {
  if (typeof window === 'undefined') return;
  window.localStorage.setItem(THEME_PREFERENCES_STORAGE_KEY, JSON.stringify(preferences));
}

export function systemPrefersDark(): boolean {
  return typeof window !== 'undefined'
    && typeof window.matchMedia === 'function'
    && window.matchMedia('(prefers-color-scheme: dark)').matches;
}

export function resolveThemeName(mode: PanelThemeMode, prefersDark = systemPrefersDark()): PanelThemeName {
  if (mode === 'system') return prefersDark ? 'dark' : 'light';
  return mode;
}

export function getThemePreset(preferences: PanelThemePreferences, themeName: PanelThemeName): PanelThemePreset {
  if (preferences.sharedPreset) return preferences.preset;
  return themeName === 'dark' ? preferences.darkPreset : preferences.lightPreset;
}

export function getThemeSemanticColors(
  preferences: PanelThemePreferences,
  themeName: PanelThemeName,
): PanelThemeSemanticColors {
  return PANEL_THEME_PRESETS[getThemePreset(preferences, themeName)][themeName];
}

const themePreferences = reactive<PanelThemePreferences>(loadThemePreferences());

export function usePanelThemePreferences() {
  function setMode(mode: PanelThemeMode) {
    if (isPanelThemeMode(mode)) themePreferences.mode = mode;
  }

  function setSharedPreset(shared: boolean) {
    themePreferences.sharedPreset = shared;
  }

  function setPreset(target: 'shared' | PanelThemeName, preset: PanelThemePreset) {
    if (!isPanelThemePreset(preset)) return;
    if (target === 'shared') themePreferences.preset = preset;
    if (target === 'light') themePreferences.lightPreset = preset;
    if (target === 'dark') themePreferences.darkPreset = preset;
  }

  function resetPresets() {
    themePreferences.preset = DEFAULT_THEME_PRESET;
    themePreferences.lightPreset = DEFAULT_THEME_PRESET;
    themePreferences.darkPreset = DEFAULT_THEME_PRESET;
  }

  return {
    preferences: themePreferences,
    setMode,
    setSharedPreset,
    setPreset,
    resetPresets,
  };
}

export function getInitialTheme(): PanelThemeName {
  return resolveThemeName(themePreferences.mode);
}

export function getInitialThemeColors() {
  return {
    light: getThemeSemanticColors(themePreferences, 'light'),
    dark: getThemeSemanticColors(themePreferences, 'dark'),
  };
}

export function watchSystemTheme(onChange: (isDark: boolean) => void): () => void {
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return () => undefined;

  systemThemeQuery = window.matchMedia('(prefers-color-scheme: dark)');
  systemThemeListener = (event) => onChange(event.matches);
  systemThemeQuery.addEventListener('change', systemThemeListener);

  return () => {
    if (systemThemeQuery && systemThemeListener) {
      systemThemeQuery.removeEventListener('change', systemThemeListener);
    }
    systemThemeQuery = undefined;
    systemThemeListener = undefined;
  };
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
