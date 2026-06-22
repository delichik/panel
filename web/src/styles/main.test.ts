import { describe, expect, it } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

const mainStyles = readFileSync(resolve(__dirname, 'main.css'), 'utf8');

describe('global responsive width constraints', () => {
  it('contains wide tables inside their own horizontal scroller', () => {
    expect(mainStyles).toMatch(/\.v-table\s*\{[^}]*width:\s*100%;[^}]*min-width:\s*0;[^}]*max-width:\s*100%;/s);
    expect(mainStyles).toMatch(/\.v-table \.v-table__wrapper\s*\{[^}]*width:\s*100%;[^}]*min-width:\s*0;[^}]*max-width:\s*100%;[^}]*overflow-x:\s*auto;/s);
    expect(mainStyles).toContain('width: max-content;');
  });

  it('prevents page-shell children and cards from widening the page', () => {
    expect(mainStyles).toMatch(/\.page-shell\s*\{[^}]*width:\s*100%;[^}]*max-width:\s*100%;/s);
    expect(mainStyles).toMatch(/\.page-shell > \*\s*\{[^}]*width:\s*100%;[^}]*min-width:\s*0;[^}]*max-width:\s*100%;/s);
    expect(mainStyles).toMatch(/\.page-shell \.v-card\s*\{[^}]*min-width:\s*0;[^}]*max-width:\s*100%;/s);
  });
});

describe('global display transitions', () => {
  it('smooths common Vuetify visibility transitions', () => {
    expect(mainStyles).toContain('.dialog-transition-enter-active');
    expect(mainStyles).toContain('.scale-transition-enter-active');
    expect(mainStyles).toContain('.v-snackbar-transition-enter-active');
    expect(mainStyles).toContain('.message-transition-enter-active');
    expect(mainStyles).toContain('transition-duration: 200ms !important;');
    expect(mainStyles).toContain('transform: translateY(6px) scale(0.98) !important;');
  });

  it('keeps expand transitions aligned with the global motion timing', () => {
    expect(mainStyles).toMatch(/\.expand-transition-enter-active,[\s\S]*\.expand-both-transition-leave-active\s*\{[^}]*transition-duration:\s*200ms !important;/);
  });

  it('preserves component-defined fade opacity for dialog scrims', () => {
    expect(mainStyles).toMatch(/\.fade-transition-enter-to,\s*\.fade-transition-leave-from\s*\{[^}]*transform:\s*translateY\(0\) scale\(1\) !important;[^}]*\}/s);
    expect(mainStyles).not.toMatch(/\.fade-transition-enter-to,[\s\S]*?\.fade-transition-leave-from[^{]*\{[^}]*opacity:\s*1 !important;/s);
  });
});

describe('global dialog presentation', () => {
  it('matches dialog headers to selector panel headers', () => {
    expect(mainStyles).toMatch(/\.app-dialog-title\s*\{[^}]*background:\s*rgb\(var\(--v-theme-surface-variant\)\);/s);
  });
});
