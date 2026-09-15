import { describe, expect, it } from 'vitest';
import { agentTone, canInstallUfw, connectionHost, credentialReferences, validateServerInput } from './model';
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
    expect(validateServerInput({ name: '', ipv4: '', ipv6: '', port: 70000, credentialId: '', sshUsername: '', dockerHost: '', traits: {}, variables: {}, notes: '' })).toMatchObject({
      name: 'serversPage.validationName',
      ipv4: 'serversPage.validationAddressRequired',
      port: 'serversPage.validationPort',
      credentialId: 'serversPage.validationCredential',
      dockerHost: 'serversPage.validationDocker',
    });
  });

  it('validates ipv4 and ipv6 literals and derives the connection host', () => {
    expect(validateServerInput({ name: 'edge', ipv4: '999.0.0.1', ipv6: '', port: 22, credentialId: 'cred-1', sshUsername: '', dockerHost: 'unix:///var/run/docker.sock', traits: {}, variables: {}, notes: '' }).ipv4).toBe('serversPage.validationIpv4');
    expect(validateServerInput({ name: 'edge', ipv4: '', ipv6: '2001:db8::1', port: 22, credentialId: 'cred-1', sshUsername: '', dockerHost: 'unix:///var/run/docker.sock', traits: {}, variables: {}, notes: '' })).toEqual({});
    expect(connectionHost({ ipv4: '203.0.113.5', ipv6: '2001:db8::5' })).toBe('203.0.113.5');
    expect(connectionHost({ ipv4: '', ipv6: '2001:db8::5' })).toBe('2001:db8::5');
  });
});
