import { describe, expect, it } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

const serversPage = readFileSync(resolve(__dirname, 'ServersPageContent.vue'), 'utf8');
const appLayout = readFileSync(resolve(__dirname, '../../../layouts/AppLayout.vue'), 'utf8');
const router = readFileSync(resolve(__dirname, '../../../router/index.ts'), 'utf8');

describe('ServersPage shell style alignment', () => {
  it('uses sidebar submenu routes instead of page tabs', () => {
    expect(appLayout).toContain("key: 'servers'");
    expect(appLayout).toContain("to: '/servers'");
    expect(appLayout).toContain("t('layout.nav.node')");
    expect(appLayout).toContain("to: '/credentials'");
    expect(appLayout).toContain("t('layout.nav.credentials')");
    expect(router).toContain("{ path: 'credentials', name: 'credentials', component: ServerCredentialsPage");
    expect(router).toContain("{ path: 'servers/firewall', name: 'server-firewall'");
    expect(appLayout).toContain("to: '/servers/firewall'");
    expect(appLayout).toContain("t('layout.nav.firewall')");
    expect(serversPage).not.toContain('<v-tabs');
    expect(serversPage).not.toMatch(/<v-tab[\s>]/);
  });

  it('uses application-style dialogs for Server page editors', () => {
    expect(serversPage).toContain('<v-dialog v-model="serverDialog" width="640"');
    expect(serversPage).toContain('<v-dialog v-model="credentialDialog" width="680"');
    expect(serversPage).toContain('class="app-dialog-card"');
    expect(serversPage).not.toContain('variablesDialog');
    expect(serversPage).not.toContain('<v-navigation-drawer v-model="serverDialog"');
    expect(serversPage).not.toContain('<v-navigation-drawer v-model="credentialDialog"');
  });

  it('shows agent runtime status and deployment bundle actions', () => {
    expect(serversPage).toContain('agentStatusForServer');
    expect(serversPage).toContain('serversApi.issueAgentCertificate');
    expect(serversPage).toContain('PANEL_AGENT_DOCKER_HOST');
    expect(serversPage).toContain("t('serversPage.agentReady')");
    expect(serversPage).toContain("t('serversPage.deployAgent')");
    expect(serversPage).toContain("t('serversPage.dockerHost')");
  });

  it('keeps the server list focused on reachability and agent runtime status', () => {
    expect(serversPage).toContain('agentStatusForServer(server)');
    expect(serversPage).not.toContain(`<span class="status-dot" :class="server.reachable ? 'success' : 'warning'" />`);
  });

  it('loads servers and credentials without a runtime control-plane projection', () => {
    expect(serversPage).toContain('serversApi.listServers()');
    expect(serversPage).toContain('serversApi.listCredentials()');
    expect(serversPage).not.toContain('loadControlPlane');
  });

  it('requires confirmation before restarting a server', () => {
    expect(serversPage).toContain('@click="restartServer(selectedServer)"');
    expect(serversPage).toContain("confirm(t('serversPage.confirmRestart')");
    expect(serversPage).toContain('serversApi.restartServer(server.id)');
    expect(serversPage).toContain(':disabled="!canRestart(selectedServer)"');
  });

  it('renders network interfaces as separate cards', () => {
    expect(serversPage).toContain('class="network-grid"');
    expect(serversPage).toContain('v-for="network in networkInterfaces(selectedServer.traits)"');
    expect(serversPage).toContain('networkFamilyLabel(item.family)');
    expect(serversPage).not.toContain("traitValue(selectedServer, 'sys.network_interfaces')");
  });
});
