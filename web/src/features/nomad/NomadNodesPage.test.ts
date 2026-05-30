import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

const page = readFileSync(resolve(__dirname, 'pages/NomadNodesPage.vue'), 'utf8');
const setupPage = readFileSync(resolve(__dirname, 'pages/NomadSetupPage.vue'), 'utf8');

describe('NomadNodesPage', () => {
  it('offers joining unmanaged SSH servers into Nomad', () => {
    expect(page).toContain('Join Node');
    expect(page).toContain('nomadApi.controlPlane');
    expect(page).toContain('nomadApi.joinServer');
    expect(page).toContain('controlPlane?.status');
    expect(page).toContain('unconfigured');
    expect(page).toContain("router.replace('/nomad/setup')");
    expect(page).toContain('candidateServers');
    expect(page).toContain('ProjectedNomadNodeDto');
    expect(page).toContain('unmanaged');
    expect(page).toContain('registering');
  });

  it('keeps first-server bootstrap isolated on the setup page', () => {
    expect(setupPage).toContain('nomadApi.controlPlane');
    expect(setupPage).toContain('nomadApi.bootstrapServer');
    expect(setupPage).toContain('Bootstrap first Nomad server');
    expect(setupPage).toContain('bootstrapCandidates');
    expect(setupPage).toContain('to="/servers"');
    expect(setupPage).toContain("router.replace('/nomad/nodes')");
  });
});
