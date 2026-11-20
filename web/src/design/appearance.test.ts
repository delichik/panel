// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import type { Appearance } from '@/types/appearance';

function memoryStorage(): Storage {
  let store = new Map<string, string>();
  return {
    get length() { return store.size; },
    clear() { store = new Map(); },
    getItem(key) { return store.has(key) ? store.get(key)! : null; },
    key(index) { return Array.from(store.keys())[index] ?? null; },
    removeItem(key) { store.delete(key); },
    setItem(key, value) { store.set(key, String(value)); },
  };
}

vi.stubGlobal('localStorage', memoryStorage());
vi.stubGlobal('matchMedia', (query: string) => ({
  matches: false,
  media: query,
  onchange: null,
  addEventListener: () => undefined,
  removeEventListener: () => undefined,
  addListener: () => undefined,
  removeListener: () => undefined,
  dispatchEvent: () => false,
}));

vi.mock('@/api/appearance', () => ({
  appearanceApi: {
    get: vi.fn(),
    update: vi.fn(),
    uploadBackground: vi.fn(),
    removeBackground: vi.fn(),
    backgroundContent: vi.fn(),
    publicAppearance: vi.fn(),
  },
}));

const appStyles = readFileSync(resolve(__dirname, '../styles/main.css'), 'utf8');
const appearanceModule = await import('./appearance');
const { appearanceApi } = await import('@/api/appearance');

function sampleAppearance(overrides: Partial<Appearance> = {}): Appearance {
  return {
    themeMode: 'light',
    mica: true,
    accent: 'teal',
    background: { kind: 'none', presetId: null, assetId: null, revision: 0, hasImage: false },
    thumbDataUrl: null,
    initialized: true,
    updatedAt: '2026-11-17T00:00:00.000Z',
    ...overrides,
  };
}

beforeEach(() => {
  localStorage.clear();
  document.documentElement.removeAttribute('data-theme');
  document.documentElement.removeAttribute('data-accent');
  document.documentElement.removeAttribute('data-background');
  document.documentElement.removeAttribute('data-mica');
  document.documentElement.removeAttribute('data-preset');
  document.documentElement.style.removeProperty('--panel-mica-thumb');
  vi.mocked(appearanceApi.get).mockReset();
  vi.mocked(appearanceApi.update).mockReset();
});

describe('appearance cache and document application', () => {
  it('round-trips the first-paint cache and rejects malformed values', () => {
    const appearance = sampleAppearance();
    appearanceModule.writeAppearanceCache(appearance);
    expect(appearanceModule.readAppearanceCache()).toEqual(appearance);

    localStorage.setItem(appearanceModule.APPEARANCE_CACHE_KEY, '{"themeMode":"neon"}');
    expect(appearanceModule.readAppearanceCache()).toBeNull();
  });

  it('writes the WinUI data attributes and the Mica thumbnail variable', () => {
    const thumb = 'data:image/png;base64,AAA';
    appearanceModule.applyAppearanceToDocument(sampleAppearance({
      themeMode: 'dark',
      accent: 'purple',
      background: { kind: 'image', presetId: null, assetId: 'asset-1', revision: 2, hasImage: true },
      thumbDataUrl: thumb,
    }));

    const root = document.documentElement;
    expect(root.dataset.theme).toBe('dark');
    expect(root.dataset.accent).toBe('purple');
    expect(root.dataset.background).toBe('image');
    expect(root.dataset.mica).toBe('on');
    expect(root.style.getPropertyValue('--panel-mica-thumb')).toContain(thumb);

    appearanceModule.applyAppearanceToDocument(sampleAppearance({
      background: { kind: 'preset', presetId: 'dusk', assetId: null, revision: 0, hasImage: false },
    }));
    expect(root.dataset.background).toBe('preset');
    expect(root.dataset.preset).toBe('dusk');
    expect(root.style.getPropertyValue('--panel-mica-thumb')).toBe('');
  });

  it('validates uploaded background files client-side', async () => {
    const image = (type: string, size: number) => new File([new Uint8Array(size)], 'background', { type });
    expect(appearanceModule.validateBackgroundFile(image('image/png', 1024))).toBeNull();
    expect(appearanceModule.validateBackgroundFile(image('image/jpeg', 1024))).toBeNull();
    expect(appearanceModule.validateBackgroundFile(image('image/webp', 1024))).toBe('type');
    expect(appearanceModule.validateBackgroundFile(image('image/png', 8 * 1024 * 1024 + 1))).toBe('size');
    // jsdom has no createImageBitmap: normalization must fall back to the original file.
    const file = image('image/jpeg', 1024);
    await expect(appearanceModule.normalizeBackgroundFile(file)).resolves.toBe(file);
  });

  it('keeps the preset/accent contract in sync with main.css', () => {
    for (const preset of appearanceModule.PRESET_BACKGROUNDS) {
      expect(appStyles).toContain(`[data-preset='${preset.id}']`);
    }
    for (const accent of appearanceModule.ACCENT_PRESETS) {
      expect(appStyles).toContain(`[data-accent='${accent.id}']`);
      expect(appStyles.toLowerCase()).toContain(accent.seed.toLowerCase());
    }
    expect(appearanceModule.PRESET_BACKGROUNDS).toHaveLength(4);
    expect(appearanceModule.ACCENT_PRESETS).toHaveLength(9);
  });
});

describe('appearance save semantics', () => {
  it('omits background when changing accent so an uploaded image is preserved', async () => {
    vi.mocked(appearanceApi.get).mockResolvedValue(sampleAppearance({
      background: { kind: 'image', presetId: null, assetId: 'asset-1', revision: 3, hasImage: true },
      thumbDataUrl: 'data:image/png;base64,AAA',
    }));
    const saved = sampleAppearance({
      accent: 'red',
      background: { kind: 'image', presetId: null, assetId: 'asset-1', revision: 3, hasImage: true },
      thumbDataUrl: 'data:image/png;base64,AAA',
    });
    vi.mocked(appearanceApi.update).mockResolvedValue(saved);

    await appearanceModule.loadAppearance();
    await appearanceModule.setAccent('red');

    expect(appearanceApi.update).toHaveBeenCalledWith({ themeMode: 'light', mica: true, accent: 'red' });
    expect(appearanceModule.useAppearance().appearance.value?.accent).toBe('red');
  });

  it('rolls back the optimistic change when the server rejects it', async () => {
    const current = sampleAppearance();
    vi.mocked(appearanceApi.get).mockResolvedValue(current);
    const failure = new Error('offline');
    vi.mocked(appearanceApi.update).mockRejectedValue(failure);

    await appearanceModule.loadAppearance();
    await expect(appearanceModule.setThemeMode('dark')).rejects.toBe(failure);

    expect(appearanceModule.useAppearance().appearance.value?.themeMode).toBe('light');
    expect(document.documentElement.dataset.theme).toBe('light');
    expect(appearanceModule.readAppearanceCache()?.themeMode).toBe('light');
  });

  it('migrates the legacy lighthouse/ocean preference only while uninitialized', async () => {
    localStorage.setItem('panel.theme.scheme', 'ocean');
    localStorage.setItem('panel.theme.mode', 'dark');
    vi.mocked(appearanceApi.get).mockResolvedValue(sampleAppearance({ initialized: false }));
    vi.mocked(appearanceApi.update).mockResolvedValue(sampleAppearance({ initialized: true, accent: 'blue', themeMode: 'dark' }));

    await appearanceModule.loadAppearance();

    expect(appearanceApi.update).toHaveBeenCalledWith({ themeMode: 'dark', mica: true, accent: 'blue' });
    expect(localStorage.getItem('panel.theme.scheme')).toBeNull();
    expect(localStorage.getItem('panel.theme.mode')).toBeNull();
  });
});
