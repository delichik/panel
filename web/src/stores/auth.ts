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
  passwordChangeRequired: boolean;
  checked: boolean;
  loading: boolean;
  error: string;
}

export const useAuthStore = defineStore('auth', {
  state: (): AuthState => ({
    username: '',
    token: readStoredToken(),
    authenticated: false,
    passwordChangeRequired: false,
    checked: false,
    loading: false,
    error: '',
  }),
  actions: {
    applySession(response: { authenticated: boolean; token?: string; username?: string; passwordChangeRequired?: boolean }, fallbackUsername = '') {
      this.authenticated = response.authenticated;
      if (!response.authenticated) {
        this.username = '';
        this.passwordChangeRequired = false;
        this.token = '';
        clearStoredToken();
        return;
      }
      this.username = response.username ?? fallbackUsername;
      this.passwordChangeRequired = Boolean(response.passwordChangeRequired);
      if (response.token) {
        this.token = response.token;
        storeToken(response.token);
      }
    },
    async restoreSession() {
      if (this.checked) return this.authenticated;
      if (!this.token) {
        this.authenticated = false;
        this.username = '';
        this.passwordChangeRequired = false;
        this.checked = true;
        return false;
      }
      this.loading = true;
      try {
        const session = await authApi.session();
        this.applySession(session);
        this.error = '';
        return this.authenticated;
      } catch (error) {
        this.authenticated = false;
        this.username = '';
        this.passwordChangeRequired = false;
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
        this.applySession(response, username);
        this.checked = true;
      } catch (error) {
        this.authenticated = false;
        this.username = '';
        this.passwordChangeRequired = false;
        this.token = '';
        clearStoredToken();
        this.error = error instanceof Error ? error.message : t('login.failed');
        throw error;
      } finally {
        this.loading = false;
      }
    },
    async updateAccount(input: { currentPassword: string; username: string; newPassword: string }) {
      this.loading = true;
      this.error = '';
      try {
        const response = await authApi.updateAccount(input);
        this.applySession(response, input.username);
        this.checked = true;
        return response;
      } catch (error) {
        this.error = error instanceof Error ? error.message : t('settingsPage.accountSaveFailed');
        throw error;
      } finally {
        this.loading = false;
      }
    },
    async updateJwtSecret(jwtSecret: string) {
      this.loading = true;
      this.error = '';
      try {
        const response = await authApi.updateJwtSecret({ jwtSecret });
        this.applySession(response, this.username);
        this.checked = true;
        return response;
      } catch (error) {
        this.error = error instanceof Error ? error.message : t('settingsPage.jwtSecretSaveFailed');
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
        this.passwordChangeRequired = false;
        this.token = '';
        clearStoredToken();
        this.checked = true;
        settings.reset();
      }
    },
  },
});
