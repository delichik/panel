import { apiClient } from './client';
import type { RuntimeBrandingSettingsDto, RuntimeSettingsDto, RuntimeSettingsUpdate } from '@/types/api';

export const settingsApi = {
  runtime() {
    return apiClient.get<RuntimeSettingsDto>('/settings/runtime');
  },
  publicBranding() {
    return apiClient.get<RuntimeBrandingSettingsDto>('/settings/public-branding');
  },
  updateRuntime(input: RuntimeSettingsUpdate) {
    return apiClient.put<RuntimeSettingsDto>('/settings/runtime', input);
  },
};
