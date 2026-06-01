import { apiClient } from './client';
import type { CertificateDto, CertificateIssueDto, CertificateIssueInput } from '@/types/api';

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
};
