import { apiClient, type ApiRequestOptions } from './client';
import type { UfwAllowInput, UfwState } from '@/types/security';

const serverPath = (serverId: string) => `/servers/${encodeURIComponent(serverId)}`;

export const securityApi = {
  ufwState(serverId: string, options?: ApiRequestOptions) {
    return apiClient.get<UfwState>(`${serverPath(serverId)}/ufw`, options);
  },
  addUfwRule(serverId: string, input: UfwAllowInput) {
    return apiClient.post<UfwState>(`${serverPath(serverId)}/ufw/rules`, input);
  },
  deleteUfwRule(serverId: string, number: number) {
    return apiClient.delete<UfwState>(`${serverPath(serverId)}/ufw/rules/${encodeURIComponent(String(number))}`);
  },
};
