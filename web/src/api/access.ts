import { apiClient } from './client';

export type PermissionMode = 'read' | 'write';

export interface PermissionUITarget {
  page: string;
  kind: string;
  target: string;
  anchor?: string;
  effect: 'hide' | 'disable' | 'readonly' | 'mask';
}

export interface PermissionDefinition {
  key: string;
  group: string;
  labelKey: string;
  descriptionKey: string;
  risk: 'normal' | 'sensitive' | 'critical';
  mode: PermissionMode;
  requires?: string[];
  grantable: boolean;
  ui: PermissionUITarget[];
}

export interface PermissionGroup {
  key: string;
  labelKey: string;
  order: number;
}

export interface PermissionCatalog {
  groups: PermissionGroup[];
  permissions: PermissionDefinition[];
}

export interface AccessRole {
  id: string;
  key?: string;
  name: string;
  description: string;
  builtin: boolean;
  permissionKeys: string[];
  userCount: number;
  createdAt: string;
}

export interface AccessUser {
  id: string;
  username: string;
  displayName: string;
  status: 'active' | 'disabled';
  builtin: boolean;
  mustChangePassword: boolean;
  roles: Array<{ id: string; key?: string; name: string }>;
  lastLoginAt?: string;
  createdAt: string;
}

export interface AccessCredential {
  id: string;
  name: string;
  tokenPrefix: string;
  scope: 'readonly' | 'readwrite';
  allowManagement: boolean;
  status: 'active' | 'expired' | 'revoked';
  expiresAt: string;
  revokedAt?: string;
  lastUsedAt?: string;
  createdAt: string;
}

export interface AccessCredentialCreated extends AccessCredential {
  token: string;
}

export interface ListPage<T> {
  items: T[];
  total: number;
  page: number;
  pageSize: number;
}

export const permissionsApi = {
  catalog() {
    return apiClient.get<PermissionCatalog>('/permissions');
  },
};

export const usersApi = {
  list(query: { page?: number; pageSize?: number; q?: string; status?: string } = {}) {
    const params = new URLSearchParams();
    if (query.page) params.set('page', String(query.page));
    if (query.pageSize) params.set('pageSize', String(query.pageSize));
    if (query.q) params.set('q', query.q);
    if (query.status) params.set('status', query.status);
    const suffix = params.toString();
    return apiClient.get<ListPage<AccessUser>>(`/users${suffix ? `?${suffix}` : ''}`);
  },
  create(input: { username: string; displayName?: string; password: string; roleIds: string[] }) {
    return apiClient.post<AccessUser>('/users', input);
  },
  update(id: string, input: { displayName?: string; status?: string; roleIds?: string[] }) {
    return apiClient.patch<AccessUser>(`/users/${id}`, input);
  },
  remove(id: string) {
    return apiClient.delete<void>(`/users/${id}`);
  },
  resetPassword(id: string, newPassword: string) {
    return apiClient.post<{ updated: boolean }>(`/users/${id}/password`, { newPassword });
  },
  forceLogout(id: string) {
    return apiClient.post<{ signedOut: boolean }>(`/users/${id}/logout`);
  },
};

export const rolesApi = {
  list() {
    return apiClient.get<{ items: AccessRole[] }>('/roles');
  },
  create(input: { name: string; description?: string; permissionKeys: string[] }) {
    return apiClient.post<AccessRole>('/roles', input);
  },
  update(id: string, input: { name: string; description?: string; permissionKeys: string[] }) {
    return apiClient.patch<AccessRole>(`/roles/${id}`, input);
  },
  remove(id: string) {
    return apiClient.delete<void>(`/roles/${id}`);
  },
};

export const accessCredentialsApi = {
  list() {
    return apiClient.get<{ items: AccessCredential[] }>('/access-credentials');
  },
  create(input: { name: string; expiresInSeconds: number; scope: 'readonly' | 'readwrite'; allowManagement: boolean }) {
    return apiClient.post<AccessCredentialCreated>('/access-credentials', input);
  },
  revoke(id: string) {
    return apiClient.delete<void>(`/access-credentials/${id}`);
  },
};
