import { apiClient } from './client';
import type { DebugDatabaseSnapshots, DebugPprofStatus, DebugRuntimeSnapshot, DebugTaskSnapshot } from '@/types/debug';

export interface ClearRuntimeDataStatus {
  cleared: boolean;
  running: boolean;
  status: 'idle' | 'running' | 'succeeded' | 'failed';
  errorCode?: string;
  runId?: string;
  stage?: string;
  failedStage?: string;
  startedAt?: string;
  finishedAt?: string;
}

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
  clearRuntimeData(signal?: AbortSignal) {
    return apiClient.post<ClearRuntimeDataStatus>('/debug/clear-runtime-data', { confirmation: 'CLEAR RUNTIME DATA' }, { signal });
  },
  clearRuntimeDataStatus(signal?: AbortSignal) {
    return apiClient.get<ClearRuntimeDataStatus>('/debug/clear-runtime-data', { signal });
  },
};
