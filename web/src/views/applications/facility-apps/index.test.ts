import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, it } from 'vitest';

const source = readFileSync(resolve(__dirname, 'index.vue'), 'utf8');

describe('facility apps page', () => {
  it('keeps static domain group keys independent from the editable domain value', () => {
    expect(source).toContain('const byGroupId = new Map<string, number>();');
    expect(source).toContain('const key = site.localGroupId;');
    expect(source).not.toContain('const key = domain || site.localGroupId;');
  });

  it('sends normalized static site configuration when saving', () => {
    expect(source).toContain('staticSites: normalizedStaticSitesForSave(),');
    expect(source).toContain('return staticSiteGroups.value.flatMap');
  });
});
