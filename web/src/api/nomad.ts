import { apiClient, type ApiClient } from './client';
import type {
  NomadControlPlaneDto,
  NomadNodeDto,
  NomadReverseProxyStaticSiteDto,
  NomadReverseProxyUpdateDto,
  NomadStatusDto,
  ServerDto,
} from '@/types/api';

export interface TaskCreatedDto {
  taskId: string;
}

export interface RemoveNomadNodeInput {
  serverId?: string;
  nodeId?: string;
}

export interface RedeployNomadNodeInput {
  serverId: string;
  role: string;
}

export interface NomadServerInput {
  serverId: string;
  advertiseAddress: string;
}

export interface ReverseProxyInput {
  serverId: string;
  enabled: boolean;
  staticFiles: boolean;
  staticSites: NomadReverseProxyStaticSiteDto[];
}

function normalizeList<T>(items: T[] | null | undefined) {
  return Array.isArray(items) ? items : [];
}

function normalizeControlPlane(controlPlane: NomadControlPlaneDto) {
  return {
    ...controlPlane,
    nodes: normalizeList(controlPlane.nodes),
    joinCandidates: normalizeList(controlPlane.joinCandidates),
    bootstrapCandidates: normalizeList(controlPlane.bootstrapCandidates),
  };
}

export function createNomadApi(client: ApiClient = apiClient) {
  return {
    status() {
      return client.get<NomadStatusDto>('/nomad/status');
    },
    nodes() {
      return client.get<NomadNodeDto[]>('/nomad/nodes');
    },
    async controlPlane() {
      return normalizeControlPlane(await client.get<NomadControlPlaneDto>('/nomad/control-plane'));
    },
    async joinCandidates() {
      return normalizeList(await client.get<ServerDto[] | null>('/nomad/join-candidates'));
    },
    joinServer(serverId: string) {
      return client.post<TaskCreatedDto>('/nomad/join', { serverId });
    },
    bootstrapServer(input: NomadServerInput) {
      return client.post<TaskCreatedDto>('/nomad/bootstrap-server', input);
    },
    redeployNode(input: RedeployNomadNodeInput) {
      return client.post<TaskCreatedDto>('/nomad/redeploy-node', input);
    },
    rebuildCluster(input: NomadServerInput) {
      return client.post<TaskCreatedDto>('/nomad/rebuild-cluster', input);
    },
    switchServer(input: NomadServerInput) {
      return client.post<TaskCreatedDto>('/nomad/switch-server', input);
    },
    removeNode(input: RemoveNomadNodeInput) {
      return client.post<TaskCreatedDto>('/nomad/remove-node', input);
    },
    updateReverseProxy(input: ReverseProxyInput) {
      return client.put<NomadReverseProxyUpdateDto>('/nomad/reverse-proxy', input);
    },
  };
}

export const nomadApi = createNomadApi();
