import { describe, expect, it } from 'vitest';
import { ufwTone, validateUfwRule } from './securityModel';

describe('server firewall tab model', () => {
  it('maps firewall state to semantic tones', () => {
    expect(ufwTone({ serverId: 'srv-1', supported: true, installed: true, active: true, status: 'active', rules: [] })).toBe('success');
    expect(ufwTone({ serverId: 'srv-1', supported: true, installed: false, active: false, status: 'inactive', rules: [] })).toBe('warning');
  });

  it('validates UFW allow rules before API calls', () => {
    expect(validateUfwRule({ port: 0, protocol: 'icmp', from: '' })).toEqual({
      port: 'securityPage.validationPort',
      protocol: 'securityPage.validationProtocol',
      from: 'securityPage.validationFrom',
    });
  });
});
