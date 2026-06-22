import { beforeEach, describe, expect, it, vi } from 'vitest';
import {
  DEFAULT_THEME_PRESET,
  LEGACY_THEME_STORAGE_KEY,
  PANEL_THEME_PRESETS,
  THEME_PREFERENCES_STORAGE_KEY,
  getThemeSemanticColors,
  loadThemePreferences,
  resolveThemeName,
  watchSystemTheme,
} from './theme';

function createMemoryStorage(): Storage {
  const values = new Map<string, string>();
  return {
    get length() { return values.size; },
    clear() { values.clear(); },
    getItem(key) { return values.get(key) ?? null; },
    key(index) { return [...values.keys()][index] ?? null; },
    removeItem(key) { values.delete(key); },
    setItem(key, value) { values.set(key, value); },
  };
}

describe('panel theme preferences', () => {
  let storage: Storage;

  beforeEach(() => {
    storage = createMemoryStorage();
    vi.stubGlobal('window', { localStorage: storage });
  });

  it('defaults to system mode with the shared green preset', () => {
    expect(loadThemePreferences()).toEqual({
      mode: 'system',
      sharedPreset: true,
      preset: DEFAULT_THEME_PRESET,
      lightPreset: DEFAULT_THEME_PRESET,
      darkPreset: DEFAULT_THEME_PRESET,
    });
  });

  it('migrates the legacy theme into a manual mode', () => {
    storage.setItem(LEGACY_THEME_STORAGE_KEY, 'dark');
    const preferences = loadThemePreferences();

    expect(preferences.mode).toBe('dark');
    expect(preferences.sharedPreset).toBe(false);
    expect(storage.getItem(LEGACY_THEME_STORAGE_KEY)).toBeNull();
  });

  it('migrates custom color preferences to the green preset', () => {
    storage.setItem(THEME_PREFERENCES_STORAGE_KEY, JSON.stringify({
      mode: 'light',
      sharedColor: false,
      sharedPrimary: '#123456',
      lightPrimary: '#123456',
      darkPrimary: '#abcdef',
    }));

    expect(loadThemePreferences()).toEqual({
      mode: 'light',
      sharedPreset: false,
      preset: 'green',
      lightPreset: 'green',
      darkPreset: 'green',
    });
  });

  it('falls back when persisted preferences are invalid', () => {
    storage.setItem(THEME_PREFERENCES_STORAGE_KEY, '{"mode":"neon"}');
    expect(loadThemePreferences().mode).toBe('system');
  });

  it('resolves system and manual theme modes', () => {
    expect(resolveThemeName('system', true)).toBe('dark');
    expect(resolveThemeName('system', false)).toBe('light');
    expect(resolveThemeName('light', true)).toBe('light');
    expect(resolveThemeName('dark', false)).toBe('dark');
  });

  it('uses shared or per-mode semantic preset colors', () => {
    const preferences = loadThemePreferences();
    expect(getThemeSemanticColors(preferences, 'light')).toEqual(PANEL_THEME_PRESETS.green.light);
    expect(getThemeSemanticColors(preferences, 'dark')).toEqual(PANEL_THEME_PRESETS.green.dark);

    preferences.sharedPreset = false;
    preferences.lightPreset = 'blue';
    preferences.darkPreset = 'purple';
    expect(getThemeSemanticColors(preferences, 'light')).toEqual(PANEL_THEME_PRESETS.blue.light);
    expect(getThemeSemanticColors(preferences, 'dark')).toEqual(PANEL_THEME_PRESETS.purple.dark);
  });

  it('defines primary, on-primary, and surface-variant for every preset and mode', () => {
    for (const preset of Object.values(PANEL_THEME_PRESETS)) {
      for (const colors of Object.values(preset)) {
        expect(colors.primary).toMatch(/^#[0-9a-f]{6}$/i);
        expect(colors.onPrimary).toMatch(/^#[0-9a-f]{6}$/i);
        expect(colors.surfaceVariant).toMatch(/^#[0-9a-f]{6}$/i);
      }
    }
  });

  it('subscribes to system theme changes and can unsubscribe', () => {
    const addEventListener = vi.fn();
    const removeEventListener = vi.fn();
    vi.stubGlobal('window', {
      localStorage: storage,
      matchMedia: vi.fn(() => ({ matches: false, addEventListener, removeEventListener })),
    });
    const stop = watchSystemTheme(vi.fn());
    expect(addEventListener).toHaveBeenCalledWith('change', expect.any(Function));
    stop();
    expect(removeEventListener).toHaveBeenCalledWith('change', expect.any(Function));
    vi.unstubAllGlobals();
  });
});
