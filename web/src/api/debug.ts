import { apiClient } from './client';
import type { DebugDatabaseSnapshots, DebugPprofStatus, DebugRuntimeSnapshot, DebugTaskSnapshot } from '@/types/debug';

export const debugApi = {
  runtime() {
    return apiClient.get<DebugRuntimeSnapshot>('/debug/runtime');
  },
  tasks() {
    return apiClient.get<DebugTaskSnapshot>('/debug/tasks');
  },
  databases() {
    return apiClient.get<DebugDatabaseSnapshots>('/debug/databases');
  },
  pprofStatus() {
    return apiClient.get<DebugPprofStatus>('/debug/pprof');
  },
  setPprof(enabled: boolean) {
    return apiClient.put<DebugPprofStatus>('/debug/pprof', { enabled });
  },
  clearRuntimeData() {
    return apiClient.post<{ cleared: boolean }>('/debug/clear-runtime-data', { confirmation: 'CLEAR RUNTIME DATA' });
  },
};
