import { apiClient, type ApiClient } from './client';
import type {
  NomadDeploymentDto,
  NomadEvaluationDto,
  NomadJobDto,
  NomadNodeDto,
  NomadServiceRegistrationDto,
  NomadStatusDto,
} from '@/types/api';

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
  };
}

export const nomadApi = createNomadApi();
