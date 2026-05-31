import { describe, expect, it } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

const serversPage = readFileSync(resolve(__dirname, 'pages/ServersPage.vue'), 'utf8');
const appLayout = readFileSync(resolve(__dirname, '../../layouts/AppLayout.vue'), 'utf8');
const router = readFileSync(resolve(__dirname, '../../router/index.ts'), 'utf8');

describe('ServersPage shell style alignment', () => {
  it('uses sidebar submenu routes instead of page tabs', () => {
    expect(appLayout).toContain("key: 'servers'");
    expect(appLayout).toContain("to: '/servers'");
    expect(appLayout).toContain("t('layout.nav.node')");
    expect(appLayout).toContain("to: '/credentials'");
    expect(appLayout).toContain("t('layout.nav.credentials')");
    expect(router).toContain("{ path: 'credentials', name: 'credentials', component: ServersPage");
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

  it('shows derived Nomad status without adding server-level Nomad actions', () => {
    expect(serversPage).toContain('nomadApi.controlPlane');
    expect(serversPage).toContain('nomadStatusForServer');
    expect(serversPage).toContain("t('serversPage.notJoined')");
    expect(serversPage).toContain("t('serversPage.bootstrappingServer')");
    expect(serversPage).toContain("t('serversPage.joiningClient')");
    expect(serversPage).toContain("t('serversPage.managedNode')");
    expect(serversPage).toContain("t('serversPage.joinFailed')");
    expect(serversPage).not.toContain('nomadApi.joinServer');
    expect(serversPage).not.toContain('nomadApi.bootstrapServer');
  });
});
