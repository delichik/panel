import { apiClient } from './client';

export interface AuthSession {
  authenticated: boolean;
  token?: string;
  username?: string;
  passwordChangeRequired?: boolean;
}

export interface AccountUpdateInput {
  currentPassword: string;
  username: string;
  newPassword?: string;
}

export const authApi = {
  login(username: string, password: string) {
    return apiClient.post<AuthSession>('/auth/login', { username, password }, { skipAuth: true });
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
  updateJwtSecret(jwtSecret: string) {
    return apiClient.post<AuthSession>('/auth/jwt-secret', { jwtSecret });
  },
};

