import { apiClient } from './client';
import type { RuntimeSettingsDto, RuntimeSettingsUpdate } from '@/types/api';

export const settingsApi = {
  runtime() {
    return apiClient.get<RuntimeSettingsDto>('/settings/runtime');
  },
  updateRuntime(input: RuntimeSettingsUpdate) {
    return apiClient.put<RuntimeSettingsDto>('/settings/runtime', input);
  },
};
