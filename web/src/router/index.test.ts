import { describe, expect, it } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

const routerSource = readFileSync(resolve(__dirname, 'index.ts'), 'utf8');

describe('router key asset routes', () => {
  it('exposes the self-signed and key pages on current routes only', () => {
    expect(routerSource).toContain("import SelfSignedCertificatesPage from '@/views/certificates/self-signed/index.vue'");
    expect(routerSource).toContain("import KeysPage from '@/views/certificates/keys/index.vue'");
    expect(routerSource).toContain("path: 'certificates/self-signed'");
    expect(routerSource).toContain("name: 'certificates-self-signed'");
    expect(routerSource).toContain("path: 'certificates/keys'");
    expect(routerSource).toContain("name: 'certificates-keys'");
    expect(routerSource).toContain("import SettingsSystemCertificatesPage from '@/views/settings/system-certificates/index.vue'");
    expect(routerSource).toContain("path: 'settings/system-certificates'");
    expect(routerSource).toContain("name: 'settings-system-certificates'");
    expect(routerSource).not.toContain("path: 'certificates/key-assets'");
    expect(routerSource).not.toContain("path: 'dns/certificates'");
  });

  it('uses current menu paths while redirecting old server and containerization paths', () => {
    expect(routerSource).toContain("import FirewallPage from '@/views/security/firewall/index.vue'");
    expect(routerSource).toContain("import Fail2BanPage from '@/views/security/fail2ban/index.vue'");
    expect(routerSource).toContain("import PackageUpdatesPage from '@/views/resources/packages/index.vue'");
    expect(routerSource).toContain("import ApplicationsPage from '@/views/applications/apps/index.vue'");
    expect(routerSource).toContain("path: 'servers/firewall', redirect: '/security/firewall'");
    expect(routerSource).toContain("path: 'servers/packages', redirect: '/resources/packages'");
    expect(routerSource).toContain("path: 'containerization/applications', redirect: '/applications/apps'");
    expect(routerSource).toContain("path: 'containerization/containers', redirect: '/resources/containers'");
    expect(routerSource).toContain("path: 'security/fail2ban', name: 'server-fail2ban'");
    expect(routerSource).toContain("path: 'resources/packages', name: 'system-packages'");
    expect(routerSource).toContain("path: 'applications/apps', name: 'applications'");
    expect(routerSource).toContain("path: 'applications/apps/create', name: 'application-create'");
    expect(routerSource).toContain("path: 'applications/apps/:applicationId/edit', name: 'application-edit'");
  });
});

describe('hidden debug route', () => {
  it('registers the authenticated page without adding it to navigation', () => {
    expect(routerSource).toContain("import DebugPage from '@/views/debug/index.vue'");
    expect(routerSource).toContain("path: 'debug', name: 'debug'");
    const layoutSource = readFileSync(resolve(__dirname, '../layouts/AppLayout.vue'), 'utf8');
    expect(layoutSource).not.toContain("to: '/debug'");
  });
});

describe('facility reverse proxy configuration route', () => {
  it('registers the dedicated page without adding another navigation item', () => {
    expect(routerSource).toContain("import FacilityReverseProxyConfigPage from '@/views/applications/facility-apps/config.vue'");
    expect(routerSource).toContain("path: 'applications/facility-apps/reverse-proxy/config'");
    expect(routerSource).toContain("name: 'facility-reverse-proxy-config'");
    expect(routerSource).toContain("titleKey: 'routes.facilityReverseProxyConfig.title'");
    const layoutSource = readFileSync(resolve(__dirname, '../layouts/AppLayout.vue'), 'utf8');
    expect(layoutSource).not.toContain('/applications/facility-apps/reverse-proxy/config');
  });
});
