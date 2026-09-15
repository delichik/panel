import { apiClient } from './client';
import type { DnsDomainDto, DnsDomainInput, DnsRecordDto, DnsRecordInput, DnsRecordSnapshot } from '@/types/dns';
import type { OperationAccepted } from '@/types/servers';
import type { ListPage } from '@/types/pagination';

export const dnsApi = {
  listDomains: () => apiClient.get<ListPage<DnsDomainDto>>('/dns/domains?pageSize=200').then((result) => result.items),
  listDomainPage: (params: { page?: number; pageSize?: number; q?: string } = {}) => {
    const query = new URLSearchParams();
    Object.entries(params).forEach(([name, value]) => { if (value !== undefined && value !== '') query.set(name, String(value)); });
    return apiClient.get<ListPage<DnsDomainDto>>(`/dns/domains${query.size ? `?${query}` : ''}`);
  },
  createDomain: (input: DnsDomainInput) => apiClient.post<DnsDomainDto>('/dns/domains', input),
  updateDomain: (id: string, input: DnsDomainInput) => apiClient.put<DnsDomainDto>(`/dns/domains/${encodeURIComponent(id)}`, input),
  deleteDomain: (id: string) => apiClient.delete<void>(`/dns/domains/${encodeURIComponent(id)}`),
  listRecords: (domainId: string, signal?: AbortSignal) => apiClient.get<DnsRecordSnapshot>(`/dns/domains/${encodeURIComponent(domainId)}/records`, { signal }),
  refreshRecords: (domainId: string) => apiClient.post<OperationAccepted>(`/dns/domains/${encodeURIComponent(domainId)}/records/refresh`),
  createRecord: (domainId: string, input: DnsRecordInput) => apiClient.post<DnsRecordDto>(`/dns/domains/${encodeURIComponent(domainId)}/records`, input),
  updateRecord: (domainId: string, recordId: string, input: DnsRecordInput) => apiClient.put<DnsRecordDto>(`/dns/domains/${encodeURIComponent(domainId)}/records/${encodeURIComponent(recordId)}`, input),
  deleteRecord: (domainId: string, recordId: string) => apiClient.delete<void>(`/dns/domains/${encodeURIComponent(domainId)}/records/${encodeURIComponent(recordId)}`),
};
