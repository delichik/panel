import { describe, expect, it } from 'vitest';
import { fail2BanPreset, fail2BanTone, jailsToYaml, parseSimpleJailsFromYaml, serverOptionState, ufwTone, validateUfwRule } from './model';
import type { ServerDto } from '@/types/servers';

const server: ServerDto = {
  id: 'srv-1',
  name: 'edge',
  host: '10.0.0.1',
  port: 22,
  credentialId: 'cred-1',
  reachable: true,
  sudo: { passwordless: true },
  privilege: { privileged: true, mode: 'passwordless_sudo' },
  traits: { 'agent.status': 'compatible', 'agent.url': 'https://10.0.0.1:9786' },
};

describe('security model', () => {
  it('allows security management through compatible Agent or privilege', () => {
    expect(serverOptionState(server)).toMatchObject({ agentReady: true, privileged: true, canManageSecurity: true });
    expect(serverOptionState({ ...server, reachable: false }).canManageSecurity).toBe(false);
  });

  it('maps firewall and fail2ban state to semantic tones', () => {
    expect(ufwTone({ serverId: 'srv-1', supported: true, installed: true, active: true, status: 'active', rules: [] })).toBe('success');
    expect(ufwTone({ serverId: 'srv-1', supported: true, installed: false, active: false, status: 'inactive', rules: [] })).toBe('warning');
    expect(fail2BanTone({ serverId: 'srv-1', installed: true, active: true, managed: true, panelConfigPresent: true, jails: [], raw: '', configYaml: '', config: { jails: [] } })).toBe('success');
  });

  it('validates UFW allow rules before API calls', () => {
    expect(validateUfwRule({ port: 0, protocol: 'icmp', from: '' })).toEqual({
      port: 'securityPage.validationPort',
      protocol: 'securityPage.validationProtocol',
      from: 'securityPage.validationFrom',
    });
  });

  it('keeps fail2ban presets convertible between visual and YAML drafts', () => {
    const yaml = jailsToYaml([fail2BanPreset('ssh'), fail2BanPreset('recidive')]);
    expect(yaml).toContain('name: sshd');
    expect(parseSimpleJailsFromYaml(yaml).map((item) => item.name)).toEqual(['sshd', 'recidive']);
  });
});
