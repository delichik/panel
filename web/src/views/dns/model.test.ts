import { describe, expect, it } from 'vitest';
import { isValidDnsDomainName, validateDnsDomainForm, validateDnsRecordForm } from './model';

describe('dns form validation', () => {
  it('accepts basic domain shapes and rejects empty or invalid names', () => {
    expect(isValidDnsDomainName('example.com')).toBe(true);
    expect(isValidDnsDomainName('api.example.co.uk')).toBe(true);
    expect(isValidDnsDomainName('')).toBe(false);
    expect(isValidDnsDomainName('not a domain')).toBe(false);
    expect(isValidDnsDomainName('localhost')).toBe(false);
    expect(validateDnsDomainForm({ name: '' }).name).toBe('dnsPage.validationNameRequired');
    expect(validateDnsDomainForm({ name: 'bad name' }).name).toBe('dnsPage.validationNameInvalid');
    expect(validateDnsDomainForm({ name: 'example.com' })).toEqual({});
  });

  it('requires type, name, and content for DNS records', () => {
    expect(validateDnsRecordForm({ type: '', name: '@', value: '1.1.1.1' }).type).toBe('dnsPage.validationTypeRequired');
    expect(validateDnsRecordForm({ type: 'A', name: '', value: '1.1.1.1' }).name).toBe('dnsPage.validationRecordNameRequired');
    expect(validateDnsRecordForm({ type: 'A', name: '@', value: '' }).value).toBe('dnsPage.validationContentRequired');
    expect(validateDnsRecordForm({ type: 'A', name: '@', value: '1.1.1.1' })).toEqual({});
  });
});
