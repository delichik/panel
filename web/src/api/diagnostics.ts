import { apiClient, type ApiClient } from './client';
import type { DebugSnapshotDto } from '@/types/api';

export function createDiagnosticsApi(client: ApiClient = apiClient) {
  return {
    snapshot() {
      return client.get<DebugSnapshotDto>('/debug/snapshot');
    },
  };
}

export const diagnosticsApi = createDiagnosticsApi();
