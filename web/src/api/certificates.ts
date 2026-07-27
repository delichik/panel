import { apiClient } from './client';
import type {
  DomainCertificateDto,
  IssueCertificateInput,
  IssueCertificateResult,
  RenewCertificateResult,
  SelfSignedCaInput,
  SelfSignedCertificateDto,
  SelfSignedLeafInput,
} from '@/types/certificates';
import type { ListPage } from '@/types/pagination';

export const certificatesApi = {
  list: (params: { page?: number; pageSize?: number; q?: string } = {}) => {
    const query = new URLSearchParams();
    Object.entries(params).forEach(([name, value]) => { if (value !== undefined && value !== '') query.set(name, String(value)); });
    return apiClient.get<ListPage<DomainCertificateDto>>(`/certificates${query.size ? `?${query}` : ''}`);
  },
  get: (id: string) => apiClient.get<DomainCertificateDto>(`/certificates/${encodeURIComponent(id)}`),
  issue: (input: IssueCertificateInput) => apiClient.post<IssueCertificateResult>('/certificates', input),
  reissue: (id: string, input: IssueCertificateInput) => apiClient.put<IssueCertificateResult>(`/certificates/${encodeURIComponent(id)}`, input),
  renew: (id: string) => apiClient.post<RenewCertificateResult>(`/certificates/${encodeURIComponent(id)}/renew`),
  delete: (id: string) => apiClient.delete<void>(`/certificates/${encodeURIComponent(id)}`),
  listSelfSigned: () => apiClient.get<ListPage<SelfSignedCertificateDto>>('/self-signed-certificates?pageSize=200').then((result) => result.items),
  listSelfSignedPage: (params: { page?: number; pageSize?: number; q?: string } = {}) => {
    const query = new URLSearchParams();
    Object.entries(params).forEach(([name, value]) => { if (value !== undefined && value !== '') query.set(name, String(value)); });
    return apiClient.get<ListPage<SelfSignedCertificateDto>>(`/self-signed-certificates${query.size ? `?${query}` : ''}`);
  },
  createSelfSignedCa: (input: SelfSignedCaInput) => apiClient.post<SelfSignedCertificateDto>('/self-signed-cas', input),
  createSelfSignedLeaf: (input: SelfSignedLeafInput) => apiClient.post<SelfSignedCertificateDto>('/self-signed-certificates', input),
  renewSelfSigned: (id: string) => apiClient.post<SelfSignedCertificateDto>(`/self-signed-certificates/${encodeURIComponent(id)}/renew`),
  deleteSelfSigned: (id: string) => apiClient.delete<void>(`/self-signed-certificates/${encodeURIComponent(id)}`),
};
