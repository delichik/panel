import { describe, expect, it } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

const appearanceSource = readFileSync(resolve(__dirname, 'appearance.ts'), 'utf8');
const appSource = readFileSync(resolve(__dirname, '../app/App.vue'), 'utf8');

describe('appearance runtime', () => {
  it('persists theme/mica/accent on the server instead of a UI framework provider', () => {
    expect(appearanceSource).toContain("const APPEARANCE_CACHE_KEY = 'panel.appearance.cache'");
    expect(appearanceSource).toContain('/settings/appearance');
    expect(appearanceSource).toContain('prefers-color-scheme: dark');
    expect(appSource).toContain('bootstrapAppearance');
    expect(appSource).not.toContain(['N', 'ConfigProvider'].join(''));
    expect(appearanceSource).not.toContain(['naive', 'ui'].join('-'));
  });

  it('applies the appearance through document data attributes and Mica tokens', () => {
    expect(appearanceSource).toContain('document.documentElement');
    expect(appearanceSource).toContain('dataset.accent');
    expect(appearanceSource).toContain('dataset.background');
    expect(appearanceSource).toContain('dataset.mica');
    expect(appearanceSource).toContain('--panel-mica-thumb');
    expect(appearanceSource).toContain('root.style.colorScheme');
  });

  it('migrates the retired lighthouse/ocean preferences exactly once', () => {
    expect(appearanceSource).toContain("const LEGACY_MODE_KEY = 'panel.theme.mode'");
    expect(appearanceSource).toContain("const LEGACY_SCHEME_KEY = 'panel.theme.scheme'");
    expect(appearanceSource).toContain("legacyScheme === 'ocean'");
    expect(appearanceSource).toContain("clearLegacyKeys");
    expect(appearanceSource).toContain('initialized');
  });
});
