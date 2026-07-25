import { apiClient } from './client';
import type { DebugSnapshot } from '@/types/debug';

export const debugApi = {
  snapshot() {
    return apiClient.get<DebugSnapshot>('/debug/snapshot');
  },
};
