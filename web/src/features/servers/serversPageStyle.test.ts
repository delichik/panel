import { describe, expect, it } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

const serversPage = readFileSync(resolve(__dirname, 'pages/ServersPage.vue'), 'utf8');
const appLayout = readFileSync(resolve(__dirname, '../../layouts/AppLayout.vue'), 'utf8');
const router = readFileSync(resolve(__dirname, '../../router/index.ts'), 'utf8');

describe('ServersPage shell style alignment', () => {
  it('uses sidebar submenu routes instead of page tabs', () => {
    expect(appLayout).toContain('<v-list-group value="servers">');
    expect(appLayout).toContain('<v-list-item to="/servers" title="Node" value="node" class="pl-8" />');
    expect(appLayout).toContain('<v-list-item to="/credentials" title="Credentials" value="credentials" class="pl-8" />');
    expect(router).toContain("{ path: 'credentials', name: 'credentials', component: ServersPage");
    expect(serversPage).not.toContain('<v-tabs');
    expect(serversPage).not.toMatch(/<v-tab[\s>]/);
  });

  it('uses right-side drawers for Server page editors', () => {
    expect(serversPage).toContain('<v-navigation-drawer v-model="serverDialog" location="right" temporary width="560"');
    expect(serversPage).toContain('<v-navigation-drawer v-model="credentialDialog" location="right" temporary width="620"');
    expect(serversPage).not.toContain('variablesDialog');
    expect(serversPage).not.toContain('<v-dialog v-model="serverDialog"');
    expect(serversPage).not.toContain('<v-dialog v-model="credentialDialog"');
  });

  it('shows derived Nomad status without adding server-level Nomad actions', () => {
    expect(serversPage).toContain('nomadApi.controlPlane');
    expect(serversPage).toContain('nomadStatusForServer');
    expect(serversPage).toContain('Not joined');
    expect(serversPage).toContain('Bootstrapping server');
    expect(serversPage).toContain('Joining client');
    expect(serversPage).toContain('Managed node');
    expect(serversPage).toContain('Join failed');
    expect(serversPage).not.toContain('nomadApi.joinServer');
    expect(serversPage).not.toContain('nomadApi.bootstrapServer');
  });
});
