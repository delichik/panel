import type { DnsDomainDto } from '@/types/dns';

export type FieldErrors = Record<string, string>;

export function domainTone(domain: DnsDomainDto, errorDomainId?: string) {
  if (domain.id === errorDomainId) return 'danger';
  return domain.provider === 'cloudflare' ? 'success' : 'warning';
}

export function normalizeRecordName(value: string) {
  const trimmed = value.trim();
  return trimmed || '@';
}

/** Lightweight FQDN check for managed DNS zone names (not record labels). */
export function isValidDnsDomainName(value: string) {
  const name = value.trim().toLowerCase();
  if (!name || name.length > 253) return false;
  return /^(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$/i.test(name);
}

export function validateDnsDomainForm(input: { name: string }): FieldErrors {
  const errors: FieldErrors = {};
  const name = input.name.trim();
  if (!name) errors.name = 'dnsPage.validationNameRequired';
  else if (!isValidDnsDomainName(name)) errors.name = 'dnsPage.validationNameInvalid';
  return errors;
}

export function validateDnsRecordForm(input: { type: string; name: string; value: string }): FieldErrors {
  const errors: FieldErrors = {};
  if (!input.type.trim()) errors.type = 'dnsPage.validationTypeRequired';
  if (!input.name.trim()) errors.name = 'dnsPage.validationRecordNameRequired';
  if (!input.value.trim()) errors.value = 'dnsPage.validationContentRequired';
  return errors;
}
