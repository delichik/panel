import { apiClient } from './client';
import type { CredentialDto, CredentialInput } from '@/types/credentials';

export const credentialsApi = {
  list() {
    return apiClient.get<CredentialDto[]>('/credentials');
  },
  create(input: CredentialInput) {
    return apiClient.post<CredentialDto>('/credentials', input);
  },
  update(id: string, input: CredentialInput) {
    return apiClient.put<CredentialDto>(`/credentials/${encodeURIComponent(id)}`, input);
  },
  delete(id: string) {
    return apiClient.delete<void>(`/credentials/${encodeURIComponent(id)}`);
  },
};
