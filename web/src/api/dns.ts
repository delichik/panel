import { apiClient } from './client';
import type { DnsDomainDto, DnsDomainInput } from '@/types/api';

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
};
