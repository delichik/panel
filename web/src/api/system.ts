import { apiClient } from './client';

export interface VersionInfo {
  version: string;
  channel: string;
  commit?: string;
  repository?: string;
  latestVersion?: string;
  updateAvailable: boolean;
  checkedAt?: string;
}

export const systemApi = {
  version() {
    return apiClient.get<VersionInfo>('/system/version');
  },
};

