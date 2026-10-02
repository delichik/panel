import { describe, expect, it } from 'vitest';
import { agentTone, connectionHost, connectionSignature, credentialReferences, hasBlockingPairIssues, parsePairs, tailscaleObservedStatus, tailscalePreferences, tailscaleStatusKey, tailscaleTone, tailscaleTrait, validateProbeInput, validateServerInput } from './model';
import type { ServerDto } from '@/types/servers';

const server: ServerDto = {
  id: 'srv-1',
  name: 'edge',
  kind: 'normal',
  host: '10.0.0.1',
  port: 22,
  credentialId: 'cred-1',
  reachable: true,
  tailscaleEnabled: false,
  tailscalePreferAgent: false,
  tailscalePreferInterconnect: false,
  sudo: { passwordless: true },
  privilege: { privileged: true },
  traits: { 'agent.enabled': 'true', 'agent.status': 'compatible', 'sys.ufw_supported': 'true', 'sys.ufw_installed': 'false' },
};

describe('server model', () => {
  it('maps agent capabilities from real server fields', () => {
    expect(agentTone(server)).toBe('success');
  });

  it('computes credential references from server inventory', () => {
    expect(credentialReferences('cred-1', [server])).toEqual([{ id: 'srv-1', name: 'edge', host: '10.0.0.1' }]);
  });

  it('validates server forms before API calls', () => {
    expect(validateServerInput({ name: '', kind: 'normal', agentPublicPort: 0, ipv4: '', ipv6: '', port: 70000, credentialId: '', sshUsername: '', dockerHost: '', tailscaleEnabled: false, tailscalePreferAgent: false, tailscalePreferInterconnect: false, traits: {}, variables: {}, notes: '' })).toMatchObject({
      name: 'serversPage.validationName',
      ipv4: 'serversPage.validationAddressRequired',
      port: 'serversPage.validationPort',
      credentialId: 'serversPage.validationCredential',
      dockerHost: 'serversPage.validationDocker',
    });
  });

  it('validates ipv4 and ipv6 literals and derives the connection host', () => {
    const base = { name: 'edge', kind: 'normal', agentPublicPort: 0, sshUsername: '', traits: {}, variables: {}, notes: '', tailscaleEnabled: false, tailscalePreferAgent: false, tailscalePreferInterconnect: false };
    expect(validateServerInput({ ...base, ipv4: '999.0.0.1', ipv6: '', port: 22, credentialId: 'cred-1', dockerHost: 'unix:///var/run/docker.sock' }).ipv4).toBe('serversPage.validationIpv4');
    expect(validateServerInput({ ...base, ipv4: '', ipv6: '2001:db8::1', port: 22, credentialId: 'cred-1', dockerHost: 'unix:///var/run/docker.sock' })).toEqual({});
    expect(connectionHost({ ipv4: '203.0.113.5', ipv6: '2001:db8::5' })).toBe('203.0.113.5');
    expect(connectionHost({ ipv4: '', ipv6: '2001:db8::5' })).toBe('2001:db8::5');
  });

  it('parses key=value lines and reports blocking issues instead of silently dropping them', () => {
    const result = parsePairs('  A=1 \n\nB = 2\nbroken\n=3\nB=4');
    expect(result.pairs).toEqual({ A: '1', B: '4' });
    expect(result.issues).toEqual([
      { line: 4, kind: 'missing_separator' },
      { line: 5, kind: 'empty_key' },
      { line: 6, kind: 'duplicate_key', key: 'B' },
    ]);
    expect(hasBlockingPairIssues(result.issues)).toBe(true);
    expect(hasBlockingPairIssues([{ line: 1, kind: 'duplicate_key', key: 'B' }])).toBe(false);
    expect(hasBlockingPairIssues([])).toBe(false);
  });

  it('keeps probe gating limited to connection fields while creation stays gated on the full form', () => {
    const base = { kind: 'normal', agentPublicPort: 0, ipv4: '203.0.113.10', ipv6: '', port: 22, sshUsername: '', credentialId: 'cred-1' };
    expect(validateProbeInput(base)).toEqual({});
    expect(validateProbeInput({ ...base, ipv4: '', ipv6: '2001:db8::1' })).toEqual({});
    expect(validateProbeInput({ ...base, ipv4: '999.1.1.1' })).toEqual({ ipv4: 'serversPage.validationIpv4' });
    expect(validateProbeInput({ ...base, ipv4: '', ipv6: '' })).toEqual({ ipv4: 'serversPage.validationAddressRequired' });
    expect(validateProbeInput({ ...base, credentialId: '' })).toEqual({ credentialId: 'serversPage.validationCredential' });
    expect(validateProbeInput({ ...base, port: 0 })).toEqual({ port: 'serversPage.validationPort' });
    // 名称与 Docker Host 错误不影响探测
    const full = { ...base, name: '', dockerHost: '', variables: {}, notes: '', tailscaleEnabled: false, tailscalePreferAgent: false, tailscalePreferInterconnect: false };
    expect(Object.keys(validateServerInput(full))).toEqual(['name', 'dockerHost']);
  });

  it('marks probe results stale only when a probed connection field changes', () => {
    const base = { ipv4: '203.0.113.10', ipv6: '', port: 22, sshUsername: 'root', credentialId: 'cred-1' };
    const signature = connectionSignature(base);
    expect(connectionSignature({ ...base, ipv6: '  ' })).toBe(signature);
    expect(connectionSignature({ ...base, port: 2222 })).not.toBe(signature);
    expect(connectionSignature({ ...base, credentialId: 'cred-2' })).not.toBe(signature);
    expect(connectionSignature({ ...base, sshUsername: 'ops' })).not.toBe(signature);
  });

  it('forces the tailscale preferences off when the node does not join the tailnet', () => {
    expect(tailscalePreferences({ tailscaleEnabled: false, tailscalePreferAgent: true, tailscalePreferInterconnect: true })).toEqual({ tailscalePreferAgent: false, tailscalePreferInterconnect: false });
    expect(tailscalePreferences({ tailscaleEnabled: true, tailscalePreferAgent: true, tailscalePreferInterconnect: false })).toEqual({ tailscalePreferAgent: true, tailscalePreferInterconnect: false });
  });

  it('reads the observed tailscale state from traits only and never invents one', () => {
    expect(tailscaleObservedStatus(server)).toBe('');
    expect(tailscaleStatusKey(server)).toBe('serversPage.tailscale.noStatus');
    expect(tailscaleTone(server)).toBe('neutral');

    const running = { ...server, tailscaleEnabled: true, traits: { ...server.traits, 'tailscale.status': 'running', 'tailscale.ipv4': '100.64.0.11', 'tailscale.lastError': '', 'tailscale.updatedAt': '2026-08-01T07:52:00.000Z' } };
    expect(tailscaleObservedStatus(running)).toBe('running');
    expect(tailscaleStatusKey(running)).toBe('serversPage.tailscale.status.running');
    expect(tailscaleTone(running)).toBe('success');
    expect(tailscaleTrait(running, 'ipv4')).toBe('100.64.0.11');
    expect(tailscaleTrait(running, 'lastError')).toBe('');

    const unknown = { ...server, tailscaleEnabled: true, traits: { ...server.traits, 'tailscale.status': 'reconciling' } };
    expect(tailscaleObservedStatus(unknown)).toBe('');
    expect(tailscaleTone(unknown)).toBe('neutral');
    expect(tailscaleTone({ ...server, tailscaleEnabled: true, traits: { 'tailscale.status': 'error' } })).toBe('danger');
    expect(tailscaleTone({ ...server, tailscaleEnabled: true, traits: { 'tailscale.status': 'installing' } })).toBe('warning');
    expect(tailscaleTone({ ...server, tailscaleEnabled: true, traits: { 'tailscale.status': 'unsupported' } })).toBe('info');
  });
});
