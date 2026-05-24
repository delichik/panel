import { apiClient } from './client';
import type { PackageRefreshDto, PackageUpdatesDto } from '@/types/api';
import type { TaskCreatedDto } from './servers';

export const packagesApi = {
  listUpdates(serverId: string) {
    return apiClient.get<PackageUpdatesDto>(`/servers/${serverId}/packages/updates`);
  },
  refresh(serverId: string) {
    return apiClient.post<PackageRefreshDto>(`/servers/${serverId}/packages/refresh`);
  },
  upgradeSelected(serverId: string, packages: string[]) {
    return apiClient.post<TaskCreatedDto>(`/servers/${serverId}/packages/upgrade-selected`, { packages });
  },
  upgradeAll(serverId: string) {
    return apiClient.post<TaskCreatedDto>(`/servers/${serverId}/packages/upgrade-all`);
  },
};
