import { apiClient } from './client';
import type { DebugPprofStatus, DebugSnapshot } from '@/types/debug';

export const debugApi = {
  snapshot() {
    return apiClient.get<DebugSnapshot>('/debug/snapshot');
  },
  pprofStatus() {
    return apiClient.get<DebugPprofStatus>('/debug/pprof');
  },
  setPprof(enabled: boolean) {
    return apiClient.put<DebugPprofStatus>('/debug/pprof', { enabled });
  },
};
