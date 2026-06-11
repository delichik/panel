import { apiClient, type ApiClient } from './client';
import type { SystemVersionDto } from '@/types/api';

export function createSystemApi(client: ApiClient = apiClient) {
  return {
    version() {
      return client.get<SystemVersionDto>('/system/version');
    },
  };
}

export const systemApi = createSystemApi();

