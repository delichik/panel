import { apiClient } from './client';

export interface LoginRequest {
  username: string;
  password: string;
}

export interface LoginResponse {
  authenticated: boolean;
  token: string;
  username: string;
  passwordChangeRequired: boolean;
}

export interface SessionResponse {
  authenticated: boolean;
  token?: string;
  username?: string;
  passwordChangeRequired?: boolean;
}

export interface AccountUpdateRequest {
  currentPassword: string;
  username: string;
  newPassword: string;
}

export interface JwtSecretUpdateRequest {
  jwtSecret: string;
}

export const authApi = {
  login(input: LoginRequest) {
    return apiClient.post<LoginResponse>('/auth/login', input);
  },
  logout() {
    return apiClient.post<SessionResponse>('/auth/logout');
  },
  session() {
    return apiClient.get<SessionResponse>('/auth/session');
  },
  updateAccount(input: AccountUpdateRequest) {
    return apiClient.post<LoginResponse>('/auth/account', input);
  },
  updateJwtSecret(input: JwtSecretUpdateRequest) {
    return apiClient.post<LoginResponse>('/auth/jwt-secret', input);
  },
};
