import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, it } from 'vitest';

function read(relativePath: string) {
  return readFileSync(resolve(__dirname, relativePath), 'utf8');
}

describe('selector detail workspaces', () => {
  it('uses the shared selector width across desktop layouts', () => {
    const pages = [
      read('servers/pages/ServersPage.vue'),
      read('packages/pages/PackageUpdatesPage.vue'),
      read('firewall/pages/FirewallPage.vue'),
      read('dns/pages/DomainsPage.vue'),
    ];

    for (const page of pages) {
      expect(page).toContain('clamp(300px, 26vw, 340px) minmax(0, 1fr)');
      expect(page).toContain('@media (max-width: 1080px)');
    }
  });

  it('clears stale async details and ignores late responses', () => {
    const packagesPage = read('packages/pages/PackageUpdatesPage.vue');
    const firewallPage = read('firewall/pages/FirewallPage.vue');
    const domainsPage = read('dns/pages/DomainsPage.vue');

    expect(packagesPage).toContain('updates.value = null;');
    expect(packagesPage).toContain('requestId !== updatesRequestId || serverId.value !== requestedServerId');
    expect(packagesPage).toContain('loadingUpdates && !updates');

    expect(firewallPage).toContain('state.value = null;');
    expect(firewallPage).toContain('requestId !== stateRequestId || serverId.value !== requestedServerId');
    expect(firewallPage).toContain('loadingState && !state');

    expect(domainsPage).toContain('records.value = [];');
    expect(domainsPage).toContain('requestId !== recordsRequestId || selectedDomainId.value !== domain.id');
    expect(domainsPage).toContain('recordsLoading && records.length === 0 && selectedDomain');
  });

  it('keeps the shared server selector visually flat', () => {
    const selector = read('../components/ServerSelector.vue');

    expect(selector).toContain('class="server-item"');
    expect(selector).toContain('class="status-dot"');
    expect(selector).not.toContain('transform: translateY');
    expect(selector).not.toContain('animation: pulse-green');
  });
});
