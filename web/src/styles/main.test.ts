import { describe, expect, it } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

const mainStyles = readFileSync(resolve(__dirname, 'main.css'), 'utf8');
const shellSource = readFileSync(resolve(__dirname, '../components/shell/AppShell.vue'), 'utf8');
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
    expect(shellSource).toContain('h-dvh min-h-0 w-full overflow-hidden');
    expect(shellSource).toContain('grid-rows-[56px_minmax(0,1fr)]');
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
  });
});
