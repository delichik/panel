import { apiClient } from './client';
import type { CredentialDto, ServerDto } from '@/types/api';

export interface ServerInput {
  name: string;
  host: string;
  port: number;
  sshUsername: string;
  credentialId: string | null;
  traits: Record<string, string>;
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

export interface ServerProbeDto {
  reachable: boolean;
  passwordlessSudo: boolean;
  root: boolean;
  privileged: boolean;
  os: {
    id: string;
    versionId: string;
    prettyName: string;
    supported: boolean;
  };
  traits: Record<string, string>;
  error?: string;
  passwordlessSudoText?: string;
}

function normalizeList<T>(items: T[] | null | undefined) {
  return Array.isArray(items) ? items : [];
}

export function createServersApi(client = apiClient) {
  return {
    async listServers() {
      return normalizeList(await client.get<ServerDto[] | null>('/servers'));
    },
    createServer(input: ServerInput) {
      return client.post<ServerDto>('/servers', input);
    },
    probeServer(input: ServerInput) {
      return client.post<ServerProbeDto>('/servers/probe', input);
    },
    updateServer(serverId: string, input: ServerInput) {
      return client.put<ServerDto>(`/servers/${serverId}`, input);
    },
    deleteServer(serverId: string) {
      return client.delete(`/servers/${serverId}`);
    },
    testConnection(serverId: string) {
      return client.post<TaskCreatedDto>(`/servers/${serverId}/test`);
    },
    installUFW(serverId: string) {
      return client.post<TaskCreatedDto>(`/servers/${serverId}/ufw/install`);
    },
    async listCredentials() {
      return normalizeList(await client.get<CredentialDto[] | null>('/credentials'));
    },
    createCredential(input: CredentialInput) {
      return client.post<CredentialDto>('/credentials', input);
    },
    updateCredential(credentialId: string, input: CredentialInput) {
      return client.put<CredentialDto>(`/credentials/${credentialId}`, input);
    },
    deleteCredential(credentialId: string) {
      return client.delete(`/credentials/${credentialId}`);
    },
  };
}

export const serversApi = createServersApi();
