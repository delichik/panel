import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

const page = readFileSync(resolve(__dirname, 'pages/NomadNodesPage.vue'), 'utf8');
const setupPage = readFileSync(resolve(__dirname, 'pages/NomadSetupPage.vue'), 'utf8');

describe('NomadNodesPage', () => {
  it('offers joining unmanaged SSH servers into Nomad', () => {
    expect(page).toContain("t('nomadNodesPage.joinNode')");
    expect(page).toContain('nomadApi.controlPlane');
    expect(page).toContain('nomadApi.joinServer');
    expect(page).toContain('controlPlane?.status');
    expect(page).toContain('unconfigured');
    expect(page).toContain("router.replace('/nomad/setup')");
    expect(page).toContain('candidateServers');
    expect(page).toContain('ProjectedNomadNodeDto');
    expect(page).toContain('unmanaged');
    expect(page).toContain('registering');
    expect(page).toContain('missing');
    expect(page).toContain('nomad_unreachable');
    expect(page).toContain('nomadApi.removeNode');
    expect(page).toContain('nomadApi.updateReverseProxy');
    expect(page).toContain("t('nomadNodesPage.reverseProxy')");
    expect(page).toContain('canJoinNode');
    expect(page).toContain('canRemoveNode');
  });

  it('keeps first-server bootstrap isolated on the setup page', () => {
    expect(setupPage).toContain('nomadApi.controlPlane');
    expect(setupPage).toContain('nomadApi.bootstrapServer');
    expect(setupPage).toContain("t('nomadSetupPage.bootstrapFirstServer')");
    expect(setupPage).toContain('bootstrapCandidates');
    expect(setupPage).toContain('to="/servers"');
    expect(setupPage).toContain("router.replace('/nomad/nodes')");
  });
});
