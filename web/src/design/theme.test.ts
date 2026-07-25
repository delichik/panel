import { describe, expect, it } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

const themeSource = readFileSync(resolve(__dirname, 'theme.ts'), 'utf8');
const appSource = readFileSync(resolve(__dirname, '../app/App.vue'), 'utf8');

describe('theme runtime', () => {
  it('uses the system/light/dark mode contract without a UI framework provider', () => {
    expect(themeSource).toContain("export type ThemeMode = 'system' | 'light' | 'dark'");
    expect(themeSource).toContain("const STORAGE_KEY = 'panel.theme.mode'");
    expect(themeSource).toContain('prefers-color-scheme: dark');
    expect(appSource).not.toContain(['N', 'ConfigProvider'].join(''));
    expect(themeSource).not.toContain(['naive', 'ui'].join('-'));
  });

  it('sets runtime theme state through document data attributes', () => {
    expect(themeSource).toContain('document.documentElement.dataset.theme');
    expect(themeSource).toContain('document.documentElement.style.colorScheme');
  });
});
