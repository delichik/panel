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

  it('keeps compact header utilities on one row and gives active tasks their own row', () => {
    expect(appLayout).toContain('grid-template-columns: minmax(0, 1fr) auto;');
    expect(appLayout).toContain('grid-column: 1 / -1;');
    expect(appLayout).toContain('.logout-btn :deep(.v-btn__content)');
    expect(appLayout).toContain('.user-name {\n    display: none;');
  });

  it('does not expose DNS record management in the navigation', () => {
    expect(appLayout).not.toContain("layout.nav.records");
    expect(appLayout).not.toContain("value: 'dns-records'");
  });

  it('does not expose raw Nomad inventory pages', () => {
    expect(appLayout).not.toContain("layout.nav.nomadJobs");
    expect(appLayout).not.toContain("layout.nav.deployments");
    expect(appLayout).not.toContain("to: '/nomad/jobs'");
    expect(appLayout).not.toContain("to: '/deployments'");
  });

  it('exposes the UFW firewall page in the server group', () => {
    expect(appLayout).toContain("to: '/servers/firewall'");
    expect(appLayout).toContain("t('layout.nav.firewall')");
  });

  it('renames the third certificates menu entry to key assets', () => {
    expect(appLayout).toContain("to: '/certificates/key-assets'");
    expect(appLayout).toContain("t('layout.nav.keyAssets')");
    expect(appLayout).not.toContain("to: '/certificates/self-signed'");
  });
});
