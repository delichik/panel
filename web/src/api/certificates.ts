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

export const certificatesApi = {
  list: () => apiClient.get<DomainCertificateDto[]>('/certificates'),
  issue: (input: IssueCertificateInput) => apiClient.post<IssueCertificateResult>('/certificates', input),
  renew: (id: string) => apiClient.post<RenewCertificateResult>(`/certificates/${encodeURIComponent(id)}/renew`),
  delete: (id: string) => apiClient.delete<void>(`/certificates/${encodeURIComponent(id)}`),
  listSelfSigned: () => apiClient.get<SelfSignedCertificateDto[]>('/self-signed-certificates'),
  createSelfSignedCa: (input: SelfSignedCaInput) => apiClient.post<SelfSignedCertificateDto>('/self-signed-cas', input),
  createSelfSignedLeaf: (input: SelfSignedLeafInput) => apiClient.post<SelfSignedCertificateDto>('/self-signed-certificates', input),
  renewSelfSigned: (id: string) => apiClient.post<SelfSignedCertificateDto>(`/self-signed-certificates/${encodeURIComponent(id)}/renew`),
  deleteSelfSigned: (id: string) => apiClient.delete<void>(`/self-signed-certificates/${encodeURIComponent(id)}`),
};
