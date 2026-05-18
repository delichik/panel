import { apiClient } from './client';
import type { CredentialDto, ServerDto } from '@/types/api';

export interface ServerInput {
  name: string;
  host: string;
  port: number;
  sshUsername: string;
  credentialId: string | null;
  labels: string[];
  notes: string;
}

export interface CredentialInput {
  name: string;
  type: 'password' | 'private_key';
  username: string;
  password?: string;
  privateKey?: string;
  passphrase?: string;
}

export interface TaskCreatedDto {
  taskId: string;
}

export const serversApi = {
  listServers() {
    return apiClient.get<ServerDto[]>('/servers');
  },
  createServer(input: ServerInput) {
    return apiClient.post<ServerDto>('/servers', input);
  },
  updateServer(serverId: string, input: ServerInput) {
    return apiClient.put<ServerDto>(`/servers/${serverId}`, input);
  },
  deleteServer(serverId: string) {
    return apiClient.delete(`/servers/${serverId}`);
  },
  testConnection(serverId: string) {
    return apiClient.post<TaskCreatedDto>(`/servers/${serverId}/test`);
  },
  listCredentials() {
    return apiClient.get<CredentialDto[]>('/credentials');
  },
  createCredential(input: CredentialInput) {
    return apiClient.post<CredentialDto>('/credentials', input);
  },
  updateCredential(credentialId: string, input: CredentialInput) {
    return apiClient.put<CredentialDto>(`/credentials/${credentialId}`, input);
  },
  deleteCredential(credentialId: string) {
    return apiClient.delete(`/credentials/${credentialId}`);
  },
};
