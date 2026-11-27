import { apiClient } from './client';

export interface AuthRole {
  id: string;
  key?: string;
  name: string;
}

export interface AuthUser {
  id: string;
  username: string;
  displayName: string;
  isBuiltinAdmin: boolean;
}

export interface AuthSession {
  authenticated: boolean;
  token?: string;
  username?: string;
  expiresAt?: string;
  authKind?: 'session' | 'credential';
  mustChangePassword?: boolean;
  mustRelogin?: boolean;
  user?: AuthUser;
  roles?: AuthRole[];
  permissions?: string[];
  credentialScope?: 'readonly' | 'readwrite';
  allowManagement?: boolean;
}

export interface AccountUpdateInput {
  currentPassword: string;
  username?: string;
  displayName?: string;
  newPassword?: string;
}

export const authApi = {
  login(username: string, password: string) {
    return apiClient.post<AuthSession>('/auth/login', { username, password }, { skipAuth: true });
  },
  loginWithCredential(credential: string) {
    return apiClient.post<AuthSession>('/auth/login', { credential }, { skipAuth: true });
  },
  logout() {
    return apiClient.post<AuthSession>('/auth/logout');
  },
  session() {
    return apiClient.get<AuthSession>('/auth/session');
  },
  updateAccount(input: AccountUpdateInput) {
    return apiClient.post<AuthSession>('/auth/account', input);
  },
  updateJwtSecret() {
    return apiClient.post<AuthSession>('/auth/jwt-secret', {});
  },
};

export interface SetupStatus {
  required: boolean;
}

export const setupApi = {
  status() {
    return apiClient.get<SetupStatus>('/setup/status', { skipAuth: true });
  },
  create(username: string, password: string) {
    return apiClient.post<{ created: boolean }>('/setup', { username, password }, { skipAuth: true });
  },
};
