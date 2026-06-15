import { apiClient } from './client';
import type {
  CertificateDto,
  CertificateIssueDto,
  CertificateIssueInput,
  SelfSignedCAInput,
  SelfSignedCertificateDto,
  SelfSignedLeafInput,
} from '@/types/api';

export const certificatesApi = {
  list() {
    return apiClient.get<CertificateDto[]>('/certificates');
  },
  issue(input: CertificateIssueInput) {
    return apiClient.post<CertificateIssueDto>('/certificates', input);
  },
  delete(certificateId: string) {
    return apiClient.delete(`/certificates/${certificateId}`);
  },
  renew(certificateId: string) {
    return apiClient.post<{ renewed: boolean }>(`/certificates/${certificateId}/renew`);
  },
  listSelfSigned() {
    return apiClient.get<SelfSignedCertificateDto[]>('/self-signed-certificates');
  },
  createCA(input: SelfSignedCAInput) {
    return apiClient.post<SelfSignedCertificateDto>('/self-signed-cas', input);
  },
  createSelfSigned(input: SelfSignedLeafInput) {
    return apiClient.post<SelfSignedCertificateDto>('/self-signed-certificates', input);
  },
  renewSelfSigned(certificateId: string) {
    return apiClient.post<SelfSignedCertificateDto>(`/self-signed-certificates/${certificateId}/renew`);
  },
  deleteSelfSigned(certificateId: string) {
    return apiClient.delete(`/self-signed-certificates/${certificateId}`);
  },
};
