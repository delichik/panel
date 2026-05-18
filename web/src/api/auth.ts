import { apiClient } from './client';

export interface LoginRequest {
  username: string;
  password: string;
}

export interface LoginResponse {
  authenticated: boolean;
}

export interface SessionResponse {
  authenticated: boolean;
  username?: string;
}

export const authApi = {
  login(input: LoginRequest) {
    return apiClient.post<LoginResponse>('/auth/login', input);
  },
  logout() {
    return apiClient.post<LoginResponse>('/auth/logout');
  },
  session() {
    return apiClient.get<SessionResponse>('/auth/session');
  },
};
