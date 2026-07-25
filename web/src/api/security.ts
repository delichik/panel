import { apiClient } from './client';
import type { OperationAccepted } from '@/types/servers';
import type { Fail2BanEnableInput, Fail2BanState, UfwAllowInput, UfwState } from '@/types/security';

const serverPath = (serverId: string) => `/servers/${encodeURIComponent(serverId)}`;

export const securityApi = {
  ufwState(serverId: string) {
    return apiClient.get<UfwState>(`${serverPath(serverId)}/ufw`);
  },
  addUfwRule(serverId: string, input: UfwAllowInput) {
    return apiClient.post<UfwState>(`${serverPath(serverId)}/ufw/rules`, input);
  },
  deleteUfwRule(serverId: string, number: number) {
    return apiClient.delete<UfwState>(`${serverPath(serverId)}/ufw/rules/${encodeURIComponent(String(number))}`);
  },
  enableUfw(serverId: string) {
    return apiClient.post<OperationAccepted>(`${serverPath(serverId)}/ufw/enable`);
  },
  installUfw(serverId: string) {
    return apiClient.post<OperationAccepted>(`${serverPath(serverId)}/ufw/install`);
  },
  fail2BanState(serverId: string) {
    return apiClient.get<Fail2BanState>(`${serverPath(serverId)}/fail2ban`);
  },
  saveFail2Ban(serverId: string, configYaml: string) {
    return apiClient.put<Fail2BanState>(`${serverPath(serverId)}/fail2ban`, { configYaml });
  },
  enableFail2Ban(serverId: string, input: Fail2BanEnableInput) {
    return apiClient.post<OperationAccepted>(`${serverPath(serverId)}/fail2ban/enable`, input);
  },
  releaseFail2Ban(serverId: string) {
    return apiClient.post<OperationAccepted>(`${serverPath(serverId)}/fail2ban/release`);
  },
  installFail2Ban(serverId: string) {
    return apiClient.post<OperationAccepted>(`${serverPath(serverId)}/fail2ban/install`);
  },
};
