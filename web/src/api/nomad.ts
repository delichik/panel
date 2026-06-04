import { apiClient, type ApiClient } from './client';
import type {
  NomadControlPlaneDto,
  NomadDeploymentDto,
  NomadEvaluationDto,
  NomadJobDto,
  NomadNodeDto,
  NomadReverseProxyStaticSiteDto,
  NomadServiceRegistrationDto,
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
    jobs() {
      return client.get<NomadJobDto[]>('/nomad/jobs');
    },
    deployments() {
      return client.get<NomadDeploymentDto[]>('/nomad/deployments');
    },
    evaluations() {
      return client.get<NomadEvaluationDto[]>('/nomad/evaluations');
    },
    services() {
      return client.get<NomadServiceRegistrationDto[]>('/nomad/services');
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
    bootstrapServer(serverId: string) {
      return client.post<TaskCreatedDto>('/nomad/bootstrap-server', { serverId });
    },
    removeNode(input: RemoveNomadNodeInput) {
      return client.post<TaskCreatedDto>('/nomad/remove-node', input);
    },
    updateReverseProxy(input: ReverseProxyInput) {
      return client.put<ServerDto>('/nomad/reverse-proxy', input);
    },
  };
}

export const nomadApi = createNomadApi();
