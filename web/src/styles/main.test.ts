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
