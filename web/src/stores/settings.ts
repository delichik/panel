import { defineStore } from 'pinia';
import { settingsApi } from '@/api/settings';
import type { RuntimeSettingsDto, RuntimeSettingsUpdate } from '@/types/api';
import { normalizeLocale, setLocale } from '@/i18n';

interface SettingsState {
  runtime: RuntimeSettingsDto | null;
  loaded: boolean;
  loading: boolean;
}

export const useSettingsStore = defineStore('settings', {
  state: (): SettingsState => ({
    runtime: null,
    loaded: false,
    loading: false,
  }),
  actions: {
    applyLocale(settings: RuntimeSettingsDto | null) {
      if (!settings) return;
      setLocale(settings.language);
    },
    async loadRuntime(force = false) {
      if (this.loaded && !force) return this.runtime;
      this.loading = true;
      try {
        const runtime = await settingsApi.runtime();
        runtime.language = normalizeLocale(runtime.language);
        this.runtime = runtime;
        this.loaded = true;
        this.applyLocale(runtime);
        return runtime;
      } finally {
        this.loading = false;
      }
    },
    async updateRuntime(input: RuntimeSettingsUpdate) {
      this.loading = true;
      try {
        const runtime = await settingsApi.updateRuntime({
          ...input,
          language: normalizeLocale(input.language),
        });
        runtime.language = normalizeLocale(runtime.language);
        this.runtime = runtime;
        this.loaded = true;
        this.applyLocale(runtime);
        return runtime;
      } finally {
        this.loading = false;
      }
    },
    reset() {
      this.runtime = null;
      this.loaded = false;
      this.loading = false;
    },
  },
});
