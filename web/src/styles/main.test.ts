import { describe, expect, it } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

const mainStyles = readFileSync(resolve(__dirname, 'main.css'), 'utf8');
const shellSource = readFileSync(resolve(__dirname, '../components/shell/AppShell.vue'), 'utf8');
const consolePageSource = readFileSync(resolve(__dirname, '../components/templates/ConsolePage.vue'), 'utf8');
const editorPageSource = readFileSync(resolve(__dirname, '../components/templates/EditorPage.vue'), 'utf8');
const dialogSource = readFileSync(resolve(__dirname, '../components/ui/Dialog.vue'), 'utf8');
const dropdownSource = readFileSync(resolve(__dirname, '../components/ui/Dropdown.vue'), 'utf8');
const routerSource = readFileSync(resolve(__dirname, '../router/index.ts'), 'utf8');

describe('new frontend foundation', () => {
  it('uses Tailwind and self-owned semantic tokens', () => {
    expect(mainStyles).toContain('@import "tailwindcss"');
    expect(mainStyles).toContain('--panel-bg:');
    expect(mainStyles).toContain('--panel-primary:');
    expect(mainStyles).toContain('--panel-success-bg:');
    expect(mainStyles).not.toContain('--v-theme');
  });

  it('keeps desktop shell scrolling constrained to internal regions', () => {
    expect(mainStyles).toMatch(/body\s*\{[^}]*overflow:\s*hidden;/s);
    expect(shellSource).toContain('lg:h-dvh lg:min-h-0 lg:overflow-hidden');
    expect(shellSource).toContain('lg:grid-rows-[56px_minmax(0,1fr)]');
    expect(shellSource).toContain('overflow-hidden');
  });

  it('does not route every page through the retired collection shell', () => {
    expect(routerSource).not.toContain('CollectionPage');
    expect(routerSource).toContain('@/views/servers/index.vue');
    expect(routerSource).toContain('@/views/security/index.vue');
    expect(routerSource).toContain('@/views/resources/index.vue');
    expect(routerSource).toContain('@/views/applications/index.vue');
  });
});

describe('responsive scrolling exception', () => {
  it('restores page-level scrolling below the desktop breakpoint', () => {
    expect(mainStyles).toMatch(/@media \(max-width:\s*1023\.98px\)\s*\{[\s\S]*body\s*\{[^}]*overflow:\s*auto;/);
    expect(shellSource).toContain('overflow-visible lg:overflow-hidden');
    expect(consolePageSource).toContain('max-lg:overflow-visible');
    expect(editorPageSource).toContain('max-lg:overflow-visible');
    expect(dialogSource).toContain('min-h-0 overflow-auto');
    expect(dropdownSource).toContain('<Teleport to="body">');
    expect(dropdownSource).toContain("position: 'fixed'");
  });
});
