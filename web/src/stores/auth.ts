import { defineStore } from 'pinia';
import { authApi } from '@/api/auth';
import { ApiError } from '@/api/client';

interface AuthState {
  username: string;
  authenticated: boolean;
  checked: boolean;
  loading: boolean;
  error: string;
}

export const useAuthStore = defineStore('auth', {
  state: (): AuthState => ({
    username: '',
    authenticated: false,
    checked: false,
    loading: false,
    error: '',
  }),
  actions: {
    async restoreSession() {
      if (this.checked) return this.authenticated;
      this.loading = true;
      try {
        const session = await authApi.session();
        this.authenticated = session.authenticated;
        this.username = session.username ?? '';
        this.error = '';
        return this.authenticated;
      } catch (error) {
        this.authenticated = false;
        this.username = '';
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
        this.checked = true;
      } catch (error) {
        this.authenticated = false;
        this.username = '';
        this.error = error instanceof Error ? error.message : 'Login failed';
        throw error;
      } finally {
        this.loading = false;
      }
    },
    async logout() {
      try {
        await authApi.logout();
      } finally {
        this.authenticated = false;
        this.username = '';
        this.checked = true;
      }
    },
  },
});
