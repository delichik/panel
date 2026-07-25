import { describe, expect, it } from 'vitest';
import { agentTone, canInstallUfw, credentialReferences, validateServerInput } from './model';
import type { ServerDto } from '@/types/servers';

const server: ServerDto = {
  id: 'srv-1',
  name: 'edge',
  host: '10.0.0.1',
  port: 22,
  credentialId: 'cred-1',
  reachable: true,
  sudo: { passwordless: true },
  privilege: { privileged: true },
  traits: { 'agent.enabled': 'true', 'agent.status': 'compatible', 'sys.ufw_supported': 'true', 'sys.ufw_installed': 'false' },
};

describe('server model', () => {
  it('maps agent and UFW capabilities from real server fields', () => {
    expect(agentTone(server)).toBe('success');
    expect(canInstallUfw(server)).toBe(true);
  });

  it('computes credential references from server inventory', () => {
    expect(credentialReferences('cred-1', [server])).toEqual([{ id: 'srv-1', name: 'edge', host: '10.0.0.1' }]);
  });

  it('validates server forms before API calls', () => {
    expect(validateServerInput({ name: '', host: '', port: 70000, credentialId: '', sshUsername: '', dockerHost: '', traits: {}, variables: {}, notes: '' })).toMatchObject({
      name: 'serversPage.validationName',
      host: 'serversPage.validationHost',
      port: 'serversPage.validationPort',
      credentialId: 'serversPage.validationCredential',
      dockerHost: 'serversPage.validationDocker',
    });
  });
});
