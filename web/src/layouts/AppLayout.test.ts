import { describe, expect, it } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

const appLayout = readFileSync(resolve(__dirname, 'AppLayout.vue'), 'utf8');

describe('AppLayout navigation', () => {
  it('keeps the drawer menu scrollable inside the viewport', () => {
    expect(appLayout).toContain('class="app-drawer-nav py-4 px-3"');
    expect(appLayout).toContain('.app-drawer-nav {');
    expect(appLayout).toContain('flex: 1 1 auto;');
    expect(appLayout).toContain('min-height: 0;');
    expect(appLayout).toContain('overflow-y: auto;');
  });

  it('does not expose DNS record management in the navigation', () => {
    expect(appLayout).not.toContain("layout.nav.records");
    expect(appLayout).not.toContain("value: 'dns-records'");
  });

  it('exposes Nomad inventory pages', () => {
    expect(appLayout).toContain("layout.nav.nomadJobs");
    expect(appLayout).toContain("layout.nav.deployments");
    expect(appLayout).toContain("to: '/nomad/jobs'");
    expect(appLayout).toContain("to: '/deployments'");
  });
});
