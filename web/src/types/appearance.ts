export type ThemeMode = 'system' | 'light' | 'dark';
export type ResolvedTheme = 'light' | 'dark';

export type AccentId =
  | 'blue'
  | 'teal'
  | 'green'
  | 'gold'
  | 'orange'
  | 'red'
  | 'pink'
  | 'purple'
  | 'gray';

export type BackgroundKind = 'none' | 'preset' | 'image';
export type PresetBackgroundId = 'aurora' | 'dusk' | 'mist' | 'slate';

export interface AppearanceBackground {
  kind: BackgroundKind;
  presetId: string | null;
  assetId: string | null;
  revision: number;
  hasImage: boolean;
}

/** Server-persisted appearance settings (`GET/PUT /settings/appearance`). */
export interface Appearance {
  themeMode: ThemeMode;
  mica: boolean;
  accent: AccentId;
  background: AppearanceBackground;
  /** Blurred thumbnail data URL used as the Mica base (image backgrounds only). */
  thumbDataUrl: string | null;
  /** False until the panel has persisted an explicit appearance choice. */
  initialized: boolean;
  updatedAt: string;
}

/**
 * PUT /settings/appearance. Omitted `background` keeps the current background
 * (including an uploaded image), so changing theme/accent never drops it.
 * `background.kind = 'image'` re-enables an already uploaded image.
 */
export interface AppearanceUpdate {
  themeMode: ThemeMode;
  mica: boolean;
  accent: AccentId;
  background?: {
    kind: BackgroundKind;
    presetId: string | null;
  };
}

/** Public appearance payload for the pre-auth login page. */
export interface PublicAppearance {
  themeMode: ThemeMode;
  mica: boolean;
  accent: AccentId;
  background: Pick<AppearanceBackground, 'kind' | 'presetId' | 'hasImage'>;
  thumbDataUrl: string | null;
}

export interface PresetBackground {
  id: PresetBackgroundId;
  /** CSS background-image value used for the settings preview tile. */
  gradient: string;
}
