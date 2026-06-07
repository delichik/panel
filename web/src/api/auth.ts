import { apiClient } from './client';

export interface LoginRequest {
  username: string;
  password: string;
}

export interface LoginResponse {
  authenticated: boolean;
  token: string;
}

export interface SessionResponse {
  authenticated: boolean;
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
};
