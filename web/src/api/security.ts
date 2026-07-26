import { apiClient, type ApiRequestOptions } from './client';
import type { OperationAccepted } from '@/types/servers';
import type { Fail2BanEnableInput, Fail2BanState, UfwAllowInput, UfwState } from '@/types/security';

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
  enableUfw(serverId: string) {
    return apiClient.post<OperationAccepted>(`${serverPath(serverId)}/ufw/enable`);
  },
  installUfw(serverId: string) {
    return apiClient.post<OperationAccepted>(`${serverPath(serverId)}/ufw/install`);
  },
  async fail2BanState(serverId: string, options?: ApiRequestOptions) {
    return normalizeFail2BanState(await apiClient.get<Fail2BanState | null>(`${serverPath(serverId)}/fail2ban`, options));
  },
  async saveFail2Ban(serverId: string, configYaml: string) {
    return normalizeFail2BanState(await apiClient.put<Fail2BanState | null>(`${serverPath(serverId)}/fail2ban`, { configYaml }));
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

function normalizeFail2BanState(state: Fail2BanState | null): Fail2BanState {
  const config = state?.config ?? { jails: [] };
  return {
    serverId: state?.serverId ?? '',
    installed: Boolean(state?.installed),
    active: Boolean(state?.active),
    managed: Boolean(state?.managed),
    panelConfigPresent: Boolean(state?.panelConfigPresent),
    jails: Array.isArray(state?.jails) ? state.jails : [],
    raw: state?.raw ?? '',
    configYaml: state?.configYaml ?? 'jails: []\n',
    config: {
      ...config,
      jails: Array.isArray(config.jails) ? config.jails : [],
    },
    updatedAt: state?.updatedAt ?? null,
  };
}
