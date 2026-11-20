import { apiClient, fetchBlob, fetchJson } from './client';
import type { Appearance, AppearanceUpdate, PublicAppearance } from '@/types/appearance';

const appearancePath = '/api/v1/settings/appearance';

export const appearanceApi = {
  get() {
    return apiClient.get<Appearance>('/settings/appearance');
  },

  update(input: AppearanceUpdate) {
    return apiClient.put<Appearance>('/settings/appearance', input);
  },

  async uploadBackground(file: File) {
    const form = new FormData();
    form.set('file', file);
    return fetchJson<Appearance>(`${appearancePath}/background`, {
      method: 'POST',
      body: form,
      timeoutMs: 120_000,
    });
  },

  removeBackground() {
    return apiClient.delete<Appearance>('/settings/appearance/background');
  },

  /** Blurred Mica base or the normalized original for the settings preview. */
  backgroundContent(variant: 'full' | 'thumb') {
    return fetchBlob(`${appearancePath}/background/content?variant=${variant}`, {
      fallbackFilename: variant === 'thumb' ? 'background-thumb.jpg' : 'background.jpg',
    });
  },

  publicAppearance() {
    return apiClient.get<PublicAppearance>('/settings/public-appearance', { skipAuth: true });
  },
};
