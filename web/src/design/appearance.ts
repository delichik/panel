import { computed, ref } from 'vue';
import { appearanceApi } from '@/api/appearance';
import { ApiError } from '@/api/client';
import type {
  Appearance,
  AppearanceBackground,
  AppearanceUpdate,
  AccentId,
  PresetBackground,
  PublicAppearance,
  ResolvedTheme,
  ThemeMode,
} from '@/types/appearance';

/** localStorage 只做首屏缓存；服务器（/settings/appearance）是唯一真源。 */
export const APPEARANCE_CACHE_KEY = 'panel.appearance.cache';
const LEGACY_MODE_KEY = 'panel.theme.mode';
const LEGACY_SCHEME_KEY = 'panel.theme.scheme';
const SESSION_TOKEN_KEY = 'panel.auth.token';
const MAX_BACKGROUND_BYTES = 8 * 1024 * 1024;
const BACKGROUND_TYPES = ['image/jpeg', 'image/png'];

export const ACCENT_IDS: readonly AccentId[] = ['blue', 'teal', 'green', 'gold', 'orange', 'red', 'pink', 'purple', 'gray'];

/** Windows personalization accent seeds (must match the `[data-accent]` rules in main.css). */
export const ACCENT_PRESETS: readonly { id: AccentId; seed: string }[] = [
  { id: 'blue', seed: '#0078D4' },
  { id: 'teal', seed: '#038387' },
  { id: 'green', seed: '#107C10' },
  { id: 'gold', seed: '#986F0B' },
  { id: 'orange', seed: '#CA5010' },
  { id: 'red', seed: '#C50F1F' },
  { id: 'pink', seed: '#C239B3' },
  { id: 'purple', seed: '#8764B8' },
  { id: 'gray', seed: '#69797E' },
];
export const THEME_MODES: readonly ThemeMode[] = ['system', 'light', 'dark'];

/**
 * Built-in abstract backgrounds. These must stay in sync with the
 * `[data-background='preset'][data-preset='…']` rules in `styles/main.css`:
 * the same CSS value renders the Mica base layer and the settings preview tile.
 */
export const PRESET_BACKGROUNDS: readonly PresetBackground[] = [
  {
    id: 'aurora',
    gradient:
      'radial-gradient(at 18% 22%, #2d5c8a 0%, transparent 60%), radial-gradient(at 82% 18%, #6a4a86 0%, transparent 55%), radial-gradient(at 55% 85%, #1f6b5f 0%, transparent 60%)',
  },
  {
    id: 'dusk',
    gradient:
      'radial-gradient(at 20% 80%, #8a4a36 0%, transparent 60%), radial-gradient(at 78% 25%, #7a3f68 0%, transparent 55%), linear-gradient(135deg, #1c2340, #2b1d2a)',
  },
  {
    id: 'mist',
    gradient:
      'radial-gradient(at 30% 30%, #6f8fa8 0%, transparent 60%), radial-gradient(at 80% 70%, #8aa3b5 0%, transparent 55%), linear-gradient(160deg, #c8d6df, #9fb3c2)',
  },
  {
    id: 'slate',
    gradient:
      'radial-gradient(at 70% 20%, #4a5a6a 0%, transparent 60%), radial-gradient(at 25% 75%, #39424c 0%, transparent 55%), linear-gradient(180deg, #262d34, #1d2329)',
  },
];

const preferredDarkMedia = typeof window !== 'undefined' && typeof window.matchMedia === 'function'
  ? window.matchMedia('(prefers-color-scheme: dark)')
  : null;
const prefersDark = ref(preferredDarkMedia?.matches ?? false);
preferredDarkMedia?.addEventListener('change', (event) => {
  prefersDark.value = event.matches;
  const current = appearance.value;
  if (current) applyAppearanceToDocument(current);
});

function isThemeMode(value: unknown): value is ThemeMode {
  return value === 'system' || value === 'light' || value === 'dark';
}

function isAccentId(value: unknown): value is AccentId {
  return typeof value === 'string' && (ACCENT_IDS as readonly string[]).includes(value);
}

function isBackgroundKind(value: unknown): value is AppearanceBackground['kind'] {
  return value === 'none' || value === 'preset' || value === 'image';
}

function normalizeAppearance(value: unknown): Appearance | null {
  if (!value || typeof value !== 'object') return null;
  const record = value as Partial<Appearance>;
  if (!isThemeMode(record.themeMode) || !isAccentId(record.accent) || typeof record.mica !== 'boolean') return null;
  const background = record.background as Partial<AppearanceBackground> | undefined;
  if (!background || !isBackgroundKind(background.kind)) return null;
  return {
    themeMode: record.themeMode,
    mica: record.mica,
    accent: record.accent,
    background: {
      kind: background.kind,
      presetId: typeof background.presetId === 'string' ? background.presetId : null,
      assetId: typeof background.assetId === 'string' ? background.assetId : null,
      revision: typeof background.revision === 'number' ? background.revision : 0,
      hasImage: Boolean(background.hasImage),
    },
    thumbDataUrl: typeof record.thumbDataUrl === 'string' ? record.thumbDataUrl : null,
    initialized: Boolean(record.initialized),
    updatedAt: typeof record.updatedAt === 'string' ? record.updatedAt : '',
  };
}

export function readAppearanceCache(): Appearance | null {
  try {
    const raw = localStorage.getItem(APPEARANCE_CACHE_KEY);
    if (!raw) return null;
    return normalizeAppearance(JSON.parse(raw));
  } catch {
    return null;
  }
}

export function writeAppearanceCache(appearance: Appearance) {
  try {
    localStorage.setItem(APPEARANCE_CACHE_KEY, JSON.stringify(appearance));
  } catch {
    // Quota/private-mode failures must not break theming; the server stays authoritative.
  }
}

export function resolveTheme(appearance: Appearance): ResolvedTheme {
  if (appearance.themeMode === 'system') return prefersDark.value ? 'dark' : 'light';
  return appearance.themeMode;
}

export function applyAppearanceToDocument(appearance: Appearance) {
  if (typeof document === 'undefined') return;
  const root = document.documentElement;
  const theme = resolveTheme(appearance);
  root.dataset.theme = theme;
  root.dataset.background = appearance.background.kind;
  root.dataset.mica = appearance.mica ? 'on' : 'off';
  root.dataset.accent = appearance.accent;
  if (appearance.background.kind === 'preset' && appearance.background.presetId) {
    root.dataset.preset = appearance.background.presetId;
  } else {
    delete root.dataset.preset;
  }
  root.style.colorScheme = theme;
  const thumb = appearance.background.kind === 'image' ? appearance.thumbDataUrl : null;
  if (thumb) {
    root.style.setProperty('--panel-mica-thumb', `url("${thumb}")`);
  } else {
    root.style.removeProperty('--panel-mica-thumb');
  }
}

function applyPublicAppearance(value: PublicAppearance) {
  applyAppearanceToDocument({
    themeMode: value.themeMode,
    mica: value.mica,
    accent: value.accent,
    background: {
      kind: value.background.kind,
      presetId: value.background.presetId,
      assetId: null,
      revision: 0,
      hasImage: value.background.hasImage,
    },
    thumbDataUrl: value.thumbDataUrl,
    initialized: false,
    updatedAt: '',
  });
}

function legacyMigrationInput(current: Appearance): AppearanceUpdate | null {
  const legacyMode = localStorage.getItem(LEGACY_MODE_KEY);
  const legacyScheme = localStorage.getItem(LEGACY_SCHEME_KEY);
  if (!legacyMode && !legacyScheme) return null;
  const themeMode = isThemeMode(legacyMode) ? legacyMode : current.themeMode;
  const accent: AccentId = legacyScheme === 'ocean'
    ? 'blue'
    : legacyScheme === 'lighthouse'
      ? 'teal'
      : current.accent;
  return { themeMode, mica: current.mica, accent };
}

function clearLegacyKeys() {
  localStorage.removeItem(LEGACY_MODE_KEY);
  localStorage.removeItem(LEGACY_SCHEME_KEY);
}

const appearance = ref<Appearance | null>(readAppearanceCache());
if (appearance.value) applyAppearanceToDocument(appearance.value);
const ready = ref(false);
const previewUrl = ref<string | null>(null);
let inFlight: Promise<void> | null = null;

function commit(next: Appearance) {
  appearance.value = next;
  writeAppearanceCache(next);
  applyAppearanceToDocument(next);
}

async function fetchAppearance(): Promise<void> {
  const fetched = await appearanceApi.get();
  commit(fetched);
  if (fetched.initialized) {
    clearLegacyKeys();
  } else {
    const migration = legacyMigrationInput(fetched);
    if (migration) {
      try {
        await appearanceApi.update(migration);
        clearLegacyKeys();
      } catch {
        // Keep the legacy keys so the next load can retry the one-time migration.
      }
    }
  }
}

/** Authenticated load; call from AppShell. Deduplicated while in flight. */
export function loadAppearance(): Promise<void> {
  if (!inFlight) {
    inFlight = fetchAppearance()
      .catch((error: unknown) => {
        // Keep the cached appearance on transient failures; 401 is handled globally.
        if (!(error instanceof ApiError)) console.warn('Failed to load appearance', error);
      })
      .finally(() => {
        ready.value = true;
        inFlight = null;
      });
  }
  return inFlight;
}

/** Pre-auth load for the login page; only theme/accent/background thumbnail are exposed. */
export async function loadPublicAppearance(): Promise<void> {
  try {
    applyPublicAppearance(await appearanceApi.publicAppearance());
  } catch {
    // Keep the cached/first-paint appearance when the public endpoint is unavailable.
  }
}

/** App bootstrap: authenticated users read the full settings, others read the public subset. */
export function bootstrapAppearance(): Promise<void> {
  const hasToken = Boolean(localStorage.getItem(SESSION_TOKEN_KEY));
  return hasToken ? loadAppearance() : loadPublicAppearance();
}

function updatePayload(current: Appearance, input: AppearanceUpdate): AppearanceUpdate {
  return {
    themeMode: input.themeMode,
    mica: input.mica,
    accent: input.accent,
    ...(input.background ? { background: { kind: input.background.kind, presetId: input.background.presetId } } : {}),
  };
}

function optimistic(current: Appearance, input: AppearanceUpdate): Appearance {
  const background = input.background
    ? {
        ...current.background,
        kind: input.background.kind,
        presetId: input.background.presetId,
      }
    : current.background;
  const showThumb = background.kind === 'image';
  return {
    ...current,
    themeMode: input.themeMode,
    mica: input.mica,
    accent: input.accent,
    background,
    thumbDataUrl: showThumb ? current.thumbDataUrl : null,
  };
}

/**
 * Persist theme/mica/accent. `background` is optional: omitting it keeps the
 * current background (including an uploaded image). Optimistically applies the
 * change, rolls back on failure and rethrows so the caller can toast.
 */
export async function saveAppearance(input: AppearanceUpdate): Promise<void> {
  const current = appearance.value;
  if (!current) throw new Error('Appearance is not loaded yet');
  const rollback = current;
  commit(optimistic(current, input));
  try {
    commit(await appearanceApi.update(updatePayload(current, input)));
  } catch (error) {
    commit(rollback);
    throw error;
  }
}

export function setThemeMode(themeMode: ThemeMode) {
  const current = appearance.value;
  if (!current) return Promise.reject(new Error('Appearance is not loaded yet'));
  return saveAppearance({ themeMode, mica: current.mica, accent: current.accent });
}

export function setMica(mica: boolean) {
  const current = appearance.value;
  if (!current) return Promise.reject(new Error('Appearance is not loaded yet'));
  return saveAppearance({ themeMode: current.themeMode, mica, accent: current.accent });
}

export function setAccent(accent: AccentId) {
  const current = appearance.value;
  if (!current) return Promise.reject(new Error('Appearance is not loaded yet'));
  return saveAppearance({ themeMode: current.themeMode, mica: current.mica, accent });
}

/** Switch to solid / preset background (keeps an uploaded image asset for later reuse). */
export function setBackground(kind: 'none' | 'preset', presetId: string | null) {
  const current = appearance.value;
  if (!current) return Promise.reject(new Error('Appearance is not loaded yet'));
  return saveAppearance({
    themeMode: current.themeMode,
    mica: current.mica,
    accent: current.accent,
    background: { kind, presetId },
  });
}

/** Re-enable the already uploaded image after switching to solid/preset. */
export function enableImageBackground() {
  const current = appearance.value;
  if (!current || !current.background.hasImage) {
    return Promise.reject(new Error('No uploaded background image is available'));
  }
  return saveAppearance({
    themeMode: current.themeMode,
    mica: current.mica,
    accent: current.accent,
    background: { kind: 'image', presetId: null },
  });
}

/** Returns a stable client-side validation error key, or null when the file is acceptable. */
export function validateBackgroundFile(file: File): 'type' | 'size' | 'dimensions' | null {
  if (!BACKGROUND_TYPES.includes(file.type)) return 'type';
  if (file.size > MAX_BACKGROUND_BYTES) return 'size';
  return null;
}

const MAX_BACKGROUND_EDGE = 2560;

/**
 * Browser-side normalization before upload: `createImageBitmap` applies the
 * JPEG EXIF orientation and `canvas.toBlob` re-encodes without metadata, so the
 * server never needs an EXIF parser. Falls back to the original file when the
 * browser lacks these APIs (e.g. jsdom tests).
 */
export async function normalizeBackgroundFile(file: File): Promise<File> {
  if (typeof createImageBitmap !== 'function' || typeof document === 'undefined') return file;
  try {
    const bitmap = await createImageBitmap(file);
    const canvas = document.createElement('canvas');
    const scale = Math.min(1, MAX_BACKGROUND_EDGE / Math.max(bitmap.width, bitmap.height));
    canvas.width = Math.max(1, Math.round(bitmap.width * scale));
    canvas.height = Math.max(1, Math.round(bitmap.height * scale));
    const context = canvas.getContext('2d');
    if (!context) {
      bitmap.close?.();
      return file;
    }
    context.drawImage(bitmap, 0, 0, canvas.width, canvas.height);
    bitmap.close?.();
    const type = file.type === 'image/png' ? 'image/png' : 'image/jpeg';
    const blob = await new Promise<Blob | null>((resolve) => {
      canvas.toBlob((value) => resolve(value), type, 0.9);
    });
    if (!blob) return file;
    const name = file.name.replace(/\.(jpe?g|png)$/i, type === 'image/png' ? '.png' : '.jpg');
    return new File([blob], name, { type });
  } catch {
    return file;
  }
}

export async function uploadBackgroundImage(file: File): Promise<void> {
  const invalid = validateBackgroundFile(file);
  if (invalid) {
    const error = new Error(`background_${invalid}`);
    (error as Error & { code?: string }).code = `appearance_${invalid}`;
    throw error;
  }
  const normalized = await normalizeBackgroundFile(file);
  const saved = await appearanceApi.uploadBackground(normalized);
  commit(saved);
}

export async function removeBackgroundImage(): Promise<void> {
  commit(await appearanceApi.removeBackground());
  clearBackgroundPreview();
}

/** Restore defaults: remove the uploaded image and reset theme/mica/accent to the default mist preset. */
export async function resetAppearance(): Promise<void> {
  if (appearance.value?.background.hasImage) {
    await removeBackgroundImage();
  }
  await saveAppearance({ themeMode: 'system', mica: true, accent: 'blue', background: { kind: 'preset', presetId: 'mist' } });
}

export async function loadBackgroundPreview(): Promise<string | null> {
  if (!appearance.value?.background.hasImage) {
    clearBackgroundPreview();
    return null;
  }
  const result = await appearanceApi.backgroundContent('full');
  clearBackgroundPreview();
  previewUrl.value = URL.createObjectURL(result.blob);
  return previewUrl.value;
}

export function clearBackgroundPreview() {
  if (previewUrl.value) URL.revokeObjectURL(previewUrl.value);
  previewUrl.value = null;
}

export function useAppearance() {
  const resolvedTheme = computed<ResolvedTheme>(() => {
    const current = appearance.value;
    return current ? resolveTheme(current) : (prefersDark.value ? 'dark' : 'light');
  });

  return {
    appearance,
    ready,
    resolvedTheme,
    presetBackgrounds: PRESET_BACKGROUNDS,
    accentPresets: ACCENT_PRESETS,
    previewUrl,
    load: loadAppearance,
    loadPublic: loadPublicAppearance,
    bootstrap: bootstrapAppearance,
    save: saveAppearance,
    setThemeMode,
    setMica,
    setAccent,
    setBackground,
    enableImageBackground,
    uploadBackground: uploadBackgroundImage,
    removeBackground: removeBackgroundImage,
    reset: resetAppearance,
    loadPreview: loadBackgroundPreview,
    clearPreview: clearBackgroundPreview,
    validateBackgroundFile,
    normalizeBackgroundFile,
  };
}
