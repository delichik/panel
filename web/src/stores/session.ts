import { defineStore } from 'pinia';
import { authApi, type AuthSession, type AccountUpdateInput } from '@/api/auth';

const tokenStorageKey = 'panel.auth.token';

function storedToken() {
  return localStorage.getItem(tokenStorageKey) || '';
}

export const useSessionStore = defineStore('session', {
  state: () => ({
    username: '',
    token: storedToken(),
    authenticated: false,
    passwordChangeRequired: false,
    ready: false,
  }),
  actions: {
    applySession(session: AuthSession) {
      this.authenticated = Boolean(session.authenticated && session.token);
      this.username = session.username ?? '';
      this.token = session.token ?? '';
      this.passwordChangeRequired = Boolean(session.passwordChangeRequired);
      if (this.token) localStorage.setItem(tokenStorageKey, this.token);
      else localStorage.removeItem(tokenStorageKey);
    },
    clearSession() {
      this.username = '';
      this.token = '';
      this.authenticated = false;
      this.passwordChangeRequired = false;
      localStorage.removeItem(tokenStorageKey);
    },
    async restore() {
      try {
        this.applySession(await authApi.session());
      } catch {
        this.clearSession();
      } finally {
        this.ready = true;
      }
    },
    async signIn(username: string, password: string) {
      this.applySession(await authApi.login(username, password));
      this.ready = true;
    },
    signOut() {
      this.clearSession();
    },
    async logout() {
      try {
        if (this.token) await authApi.logout();
      } finally {
        this.clearSession();
        this.ready = true;
      }
    },
    async updateAccount(input: AccountUpdateInput) {
      this.applySession(await authApi.updateAccount(input));
    },
    async updateJwtSecret(jwtSecret: string) {
      this.applySession(await authApi.updateJwtSecret(jwtSecret));
    },
  },
});
