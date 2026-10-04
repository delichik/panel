import type { DnsDomainDto } from '@/types/dns';

export function domainTone(domain: DnsDomainDto, errorDomainId?: string) {
  if (domain.id === errorDomainId) return 'danger';
  return domain.provider === 'cloudflare' ? 'success' : 'warning';
}

export function normalizeRecordName(value: string) {
  const trimmed = value.trim();
  return trimmed || '@';
}
