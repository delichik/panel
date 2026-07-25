import { apiClient } from './client';
import type { DnsDomainDto, DnsDomainInput, DnsRecordDto, DnsRecordInput } from '@/types/dns';

export const dnsApi = {
  listDomains: () => apiClient.get<DnsDomainDto[]>('/dns/domains'),
  createDomain: (input: DnsDomainInput) => apiClient.post<DnsDomainDto>('/dns/domains', input),
  updateDomain: (id: string, input: DnsDomainInput) => apiClient.put<DnsDomainDto>(`/dns/domains/${encodeURIComponent(id)}`, input),
  deleteDomain: (id: string) => apiClient.delete<void>(`/dns/domains/${encodeURIComponent(id)}`),
  listRecords: (domainId: string, signal?: AbortSignal) => apiClient.get<DnsRecordDto[]>(`/dns/domains/${encodeURIComponent(domainId)}/records`, { signal }),
  createRecord: (domainId: string, input: DnsRecordInput) => apiClient.post<DnsRecordDto>(`/dns/domains/${encodeURIComponent(domainId)}/records`, input),
  updateRecord: (domainId: string, recordId: string, input: DnsRecordInput) => apiClient.put<DnsRecordDto>(`/dns/domains/${encodeURIComponent(domainId)}/records/${encodeURIComponent(recordId)}`, input),
  deleteRecord: (domainId: string, recordId: string) => apiClient.delete<void>(`/dns/domains/${encodeURIComponent(domainId)}/records/${encodeURIComponent(recordId)}`),
};
