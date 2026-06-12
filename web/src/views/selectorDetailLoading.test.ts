import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, it } from 'vitest';

function read(relativePath: string) {
  return readFileSync(resolve(__dirname, relativePath), 'utf8');
}

describe('selector detail workspaces', () => {
  it('uses the shared selector width across desktop layouts', () => {
    const pages = [
      read('servers/_shared/ServersPageContent.vue'),
      read('servers/packages/index.vue'),
      read('servers/firewall/index.vue'),
      read('dns/domains/index.vue'),
    ];

    for (const page of pages) {
      expect(page).toContain('clamp(300px, 26vw, 340px) minmax(0, 1fr)');
      expect(page).toContain('@media (max-width: 1080px)');
    }
  });

  it('clears stale async details and ignores late responses', () => {
    const packagesPage = read('servers/packages/index.vue');
    const firewallPage = read('servers/firewall/index.vue');
    const domainsPage = read('dns/domains/index.vue');

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
