import { apiClient } from './client';
import type { DnsDomainDto, DnsDomainInput, DnsRecordDto, DnsRecordInput } from '@/types/api';

export const dnsApi = {
  listDomains() {
    return apiClient.get<DnsDomainDto[]>('/dns/domains');
  },
  createDomain(input: DnsDomainInput) {
    return apiClient.post<DnsDomainDto>('/dns/domains', input);
  },
  updateDomain(domainId: string, input: DnsDomainInput) {
    return apiClient.put<DnsDomainDto>(`/dns/domains/${domainId}`, input);
  },
  deleteDomain(domainId: string) {
    return apiClient.delete(`/dns/domains/${domainId}`);
  },
  listRecords(domainId: string) {
    return apiClient.get<DnsRecordDto[]>(`/dns/domains/${domainId}/records`);
  },
  createRecord(domainId: string, input: DnsRecordInput) {
    return apiClient.post<DnsRecordDto>(`/dns/domains/${domainId}/records`, input);
  },
  updateRecord(domainId: string, recordId: string, input: DnsRecordInput) {
    return apiClient.put<DnsRecordDto>(`/dns/domains/${domainId}/records/${recordId}`, input);
  },
  deleteRecord(domainId: string, recordId: string) {
    return apiClient.delete(`/dns/domains/${domainId}/records/${recordId}`);
  },
};
