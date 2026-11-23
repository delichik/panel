import { defineStore } from 'pinia';
import { ApiError } from '@/api/client';
import { authApi, setupApi, type AuthRole, type AuthSession, type AuthUser, type AccountUpdateInput } from '@/api/auth';

const tokenStorageKey = 'panel.auth.token';

function storedToken() {
  return localStorage.getItem(tokenStorageKey) || '';
}

export const useSessionStore = defineStore('session', {
  state: () => ({
    username: '',
    displayName: '',
    token: storedToken(),
    authenticated: false,
    mustChangePassword: false,
    user: null as AuthUser | null,
    roles: [] as AuthRole[],
    permissions: [] as string[],
    setupRequired: null as boolean | null,
    ready: false,
  }),
  actions: {
    applySession(session: AuthSession) {
      this.authenticated = Boolean(session.authenticated && session.token);
      this.username = session.user?.username ?? session.username ?? '';
      this.displayName = session.user?.displayName ?? '';
      this.user = session.user ?? null;
      this.roles = session.roles ?? [];
      this.permissions = session.permissions ?? [];
      this.mustChangePassword = Boolean(session.mustChangePassword);
      this.token = session.token ?? '';
      if (this.token) localStorage.setItem(tokenStorageKey, this.token);
      else localStorage.removeItem(tokenStorageKey);
    },
    clearSession() {
      this.username = '';
      this.displayName = '';
      this.token = '';
      this.authenticated = false;
      this.mustChangePassword = false;
      this.user = null;
      this.roles = [];
      this.permissions = [];
      localStorage.removeItem(tokenStorageKey);
    },
    can(key: string) {
      return this.permissions.includes(key);
    },
    canAny(keys: string[]) {
      return keys.some((key) => this.permissions.includes(key));
    },
    async ensureSetupStatus() {
      if (this.setupRequired !== null) return this.setupRequired;
      try {
        this.setupRequired = (await setupApi.status()).required;
      } catch {
        // Do not trap the user on the setup page when the status probe fails;
        // login will surface the real error.
        this.setupRequired = false;
      }
      return this.setupRequired;
    },
    markSetupComplete() {
      this.setupRequired = false;
    },
    async restore() {
      try {
        this.applySession(await authApi.session());
      } catch (error) {
        // Only an explicit 401/unauthorized invalidates the stored token.
        // Network errors and request timeouts keep the token so a later
        // restore (or optimistic navigation) can still reuse it; the router
        // guard decides how to proceed.
        const apiError = error instanceof ApiError ? error : null;
        if (apiError?.status === 401 || apiError?.code === 'unauthorized') {
          this.clearSession();
        }
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
    /** Global 401 handler entry point: drop the session so guards redirect to login. */
    onUnauthorized() {
      this.clearSession();
    },
    async updateAccount(input: AccountUpdateInput) {
      const result = await authApi.updateAccount(input);
      this.clearSession();
      return result;
    },
    async updateJwtSecret(jwtSecret: string) {
      const result = await authApi.updateJwtSecret(jwtSecret);
      this.clearSession();
      return result;
    },
  },
});
