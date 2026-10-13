import { describe, expect, it } from 'vitest';
import { fail2BanPreset, fail2BanTone, hasAdvancedJailConfig, jailsToYaml, parseSimpleJailsFromYaml, ufwTone, validateUfwRule } from './securityModel';

describe('server firewall tab model', () => {
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


  it('detects advanced Fail2Ban config that the visual draft would drop', () => {
    expect(hasAdvancedJailConfig(jailsToYaml([fail2BanPreset('ssh')]))).toBe(false);
    expect(hasAdvancedJailConfig('jails:\n  - name: sshd\n    enabled: true\n    options:\n      maxmemory: 128m\n')).toBe(true);
    expect(hasAdvancedJailConfig('jails:\n  - name: sshd\n    enabled: true\n    custom_key: value\n')).toBe(true);
    expect(hasAdvancedJailConfig('jails:\n  - name: sshd\n    enabled: true\n')).toBe(false);
    expect(hasAdvancedJailConfig('')).toBe(false);
    expect(hasAdvancedJailConfig('not: valid yaml: [')).toBe(false);
  });

  it('keeps fail2ban presets convertible between visual and YAML drafts', () => {
    const yaml = jailsToYaml([fail2BanPreset('ssh'), fail2BanPreset('recidive')]);
    expect(yaml).toContain('name: sshd');
    expect(parseSimpleJailsFromYaml(yaml).map((item) => item.name)).toEqual(['sshd', 'recidive']);
  });
});
