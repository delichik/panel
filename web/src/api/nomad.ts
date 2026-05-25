import { apiClient, type ApiClient } from './client';
import type {
  NomadControlPlaneDto,
  NomadDeploymentDto,
  NomadEvaluationDto,
  NomadJobDto,
  NomadNodeDto,
  NomadServiceRegistrationDto,
  NomadStatusDto,
  ServerDto,
} from '@/types/api';

export interface TaskCreatedDto {
  taskId: string;
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
    controlPlane() {
      return client.get<NomadControlPlaneDto>('/nomad/control-plane');
    },
    joinCandidates() {
      return client.get<ServerDto[]>('/nomad/join-candidates');
    },
    joinServer(serverId: string) {
      return client.post<TaskCreatedDto>('/nomad/join', { serverId });
    },
    bootstrapServer(serverId: string) {
      return client.post<TaskCreatedDto>('/nomad/bootstrap-server', { serverId });
    },
  };
}

export const nomadApi = createNomadApi();
