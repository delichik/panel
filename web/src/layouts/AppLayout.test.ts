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
    expect(appLayout).toContain('class="header-utility-strip"');
  });

  it('opens theme preferences from the header and supports system, light, and dark modes', () => {
    expect(appLayout).toContain("t('layout.theme.open')");
    expect(appLayout).toContain("value: 'system' as const");
    expect(appLayout).toContain("value: 'light' as const");
    expect(appLayout).toContain("value: 'dark' as const");
    expect(appLayout).toContain('themePreferences.sharedPreset');
    expect(appLayout).toContain("setPreset('shared'");
    expect(appLayout).toContain("setPreset(mode");
    expect(appLayout).toContain('theme-preset-option');
    expect(appLayout).not.toContain('type="color"');
  });

  it('keeps the username and logout action inside the user icon menu', () => {
    expect(appLayout).toContain("t('layout.userMenu.open')");
    expect(appLayout).toContain("{{ auth.username || t('layout.userMenu.unknownUser') }}");
    expect(appLayout).toContain(':title="t(\'layout.logout\')"');
    expect(appLayout).not.toContain('class="user-pill"');
    expect(appLayout).not.toContain('class="text-none logout-btn"');
  });

  it('uses distinct nested values for the applications group and its child item', () => {
    expect(appLayout).toContain("key: 'applications'");
    expect(appLayout).toContain("to: '/applications/apps', title: t('layout.nav.applications'), value: 'applications-apps'");
    expect(appLayout).not.toContain("to: '/applications/apps', title: t('layout.nav.applications'), value: 'applications'");
  });

  it('shows the build channel marker from version metadata', () => {
    expect(appLayout).toContain("versionInfo.value?.channel === 'dev'");
    expect(appLayout).toContain('v-if="isDevChannel"');
    expect(appLayout).toContain("t('layout.developmentChannel')");
    expect(appLayout).toContain("t('layout.developmentChannelDetail'");
  });

  it('shows the fail2ban navigation item only on dev builds', () => {
    expect(appLayout).toContain('devOnly?: boolean;');
    expect(appLayout).toContain("to: '/security/fail2ban', title: t('layout.nav.fail2ban'), value: 'server-fail2ban', devOnly: true");
    expect(appLayout).toContain('v-if="!item.devOnly || isDevChannel"');
  });

  it('does not expose DNS record management in the navigation', () => {
    expect(appLayout).not.toContain("layout.nav.records");
    expect(appLayout).not.toContain("value: 'dns-records'");
  });

  it('groups security, resources, and applications as separate navigation areas', () => {
    expect(appLayout).toContain("key: 'security'");
    expect(appLayout).toContain("t('layout.nav.security')");
    expect(appLayout).toContain("to: '/security/firewall'");
    expect(appLayout).toContain("t('layout.nav.firewall')");
    expect(appLayout).toContain("to: '/security/fail2ban'");
    expect(appLayout).toContain("t('layout.nav.fail2ban')");
    expect(appLayout).toContain("key: 'resources'");
    expect(appLayout).toContain("to: '/resources/packages'");
    expect(appLayout).toContain("to: '/resources/containers'");
    expect(appLayout).toContain("key: 'applications'");
    expect(appLayout).toContain("to: '/applications/apps'");
    expect(appLayout).toContain("to: '/applications/facility-apps'");
    expect(appLayout).not.toContain("to: '/servers/firewall'");
    expect(appLayout).not.toContain("to: '/containerization/applications'");
  });

  it('splits self-signed certificates and keys into separate menu entries', () => {
    expect(appLayout).toContain("to: '/certificates/self-signed'");
    expect(appLayout).toContain("t('layout.nav.selfSignedCertificates')");
    expect(appLayout).toContain("to: '/certificates/keys'");
    expect(appLayout).toContain("t('layout.nav.keys')");
    expect(appLayout).not.toContain("to: '/certificates/key-assets'");
  });

  it('places system certificates under settings', () => {
    expect(appLayout).toContain("to: '/settings/system-certificates'");
    expect(appLayout).toContain("t('layout.nav.settingsSystemCertificates')");
    expect(appLayout).not.toContain("to: '/certificates/system'");
  });
});
