import { defineStore } from 'pinia';
import { authApi } from '@/api/auth';
import { ApiError } from '@/api/client';
import { t } from '@/i18n';
import { useSettingsStore } from '@/stores/settings';

function readStoredToken() {
  return globalThis.localStorage?.getItem('authToken') ?? '';
}

function storeToken(token: string) {
  globalThis.localStorage?.setItem('authToken', token);
}

function clearStoredToken() {
  globalThis.localStorage?.removeItem('authToken');
}

interface AuthState {
  username: string;
  token: string;
  authenticated: boolean;
  checked: boolean;
  loading: boolean;
  error: string;
}

export const useAuthStore = defineStore('auth', {
  state: (): AuthState => ({
    username: '',
    token: readStoredToken(),
    authenticated: false,
    checked: false,
    loading: false,
    error: '',
  }),
  actions: {
    async restoreSession() {
      if (this.checked) return this.authenticated;
      if (!this.token) {
        this.authenticated = false;
        this.username = '';
        this.checked = true;
        return false;
      }
      this.loading = true;
      try {
        const session = await authApi.session();
        this.authenticated = session.authenticated;
        this.username = '';
        this.error = '';
        return this.authenticated;
      } catch (error) {
        this.authenticated = false;
        this.username = '';
        this.token = '';
        clearStoredToken();
        if (error instanceof ApiError && error.status !== 401) {
          this.error = error.message;
        }
        return false;
      } finally {
        this.checked = true;
        this.loading = false;
      }
    },
    async login(username: string, password: string) {
      this.loading = true;
      this.error = '';
      try {
        const response = await authApi.login({ username, password });
        this.authenticated = response.authenticated;
        this.username = username;
        this.token = response.token;
        storeToken(response.token);
        this.checked = true;
      } catch (error) {
        this.authenticated = false;
        this.username = '';
        this.token = '';
        clearStoredToken();
        this.error = error instanceof Error ? error.message : t('login.failed');
        throw error;
      } finally {
        this.loading = false;
      }
    },
    async logout() {
      const settings = useSettingsStore();
      try {
        await authApi.logout();
      } finally {
        this.authenticated = false;
        this.username = '';
        this.token = '';
        clearStoredToken();
        this.checked = true;
        settings.reset();
      }
    },
  },
});
