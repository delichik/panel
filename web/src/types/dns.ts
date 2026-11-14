export type DnsProvider = 'cloudflare';

export interface DnsDomainDto {
  id: string;
  name: string;
  provider: DnsProvider | string;
  createdAt: string;
  updatedAt: string;
}

export interface DnsDomainInput {
  name: string;
  provider: DnsProvider;
  apiToken?: string;
}

export interface DnsRecordDto {
  id: string;
  type: string;
  name: string;
  value: string;
  ttl: number;
  proxied?: boolean;
  comment?: string;
  createdAt?: string;
  modifiedAt?: string;
}

export interface DnsRecordInput {
  type: string;
  name: string;
  value: string;
  ttl: number;
  proxied?: boolean;
}

export interface DnsRecordSnapshot {
  items: DnsRecordDto[];
  observedAt?: string | null;
  stale: boolean;
  refreshing: boolean;
  refreshTaskId?: string;
  lastRefreshError?: string;
}
