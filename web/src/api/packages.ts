import { apiClient, type ApiRequestOptions } from './client';
import type { OperationAccepted } from '@/types/servers';
import type { PackageUpdateList, RefreshResult } from '@/types/resources';

const base = (serverId: string) => `/servers/${encodeURIComponent(serverId)}/packages`;

export const packagesApi = {
  updates(serverId: string, options?: ApiRequestOptions) {
    return apiClient.get<PackageUpdateList>(`${base(serverId)}/updates`, options);
  },
  refresh(serverId: string) {
    return apiClient.post<RefreshResult>(`${base(serverId)}/refresh`);
  },
  upgradeSelected(serverId: string, packages: string[]) {
    return apiClient.post<OperationAccepted>(`${base(serverId)}/upgrade-selected`, { packages });
  },
  upgradeAll(serverId: string) {
    return apiClient.post<OperationAccepted>(`${base(serverId)}/upgrade-all`);
  },
};
