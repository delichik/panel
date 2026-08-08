import { apiClient } from './client';
import type { CredentialDetailDto, CredentialDto, CredentialInput } from '@/types/credentials';
import type { ListPage } from '@/types/pagination';

export const credentialsApi = {
  list() {
    return apiClient.get<ListPage<CredentialDto>>('/credentials?pageSize=200').then((result) => result.items);
  },
  listPage(params: { page?: number; pageSize?: number; q?: string } = {}) {
    const query = new URLSearchParams();
    Object.entries(params).forEach(([name, value]) => { if (value !== undefined && value !== '') query.set(name, String(value)); });
    return apiClient.get<ListPage<CredentialDto>>(`/credentials${query.size ? `?${query}` : ''}`);
  },
  create(input: CredentialInput) {
    return apiClient.post<CredentialDto>('/credentials', input);
  },
  update(id: string, input: CredentialInput) {
    return apiClient.put<CredentialDto>(`/credentials/${encodeURIComponent(id)}`, input);
  },
  get(id: string) {
    return apiClient.get<CredentialDetailDto>(`/credentials/${encodeURIComponent(id)}`);
  },
  delete(id: string) {
    return apiClient.delete<void>(`/credentials/${encodeURIComponent(id)}`);
  },
};
