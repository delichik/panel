import { apiClient, type ApiRequestOptions } from './client';
import type { NetworkProbeConfig, NetworkProbeMatrix, NetworkProbeSaveInput } from '@/types/networkProbe';

export const networkProbeFacilityApi = {
  config(options?: ApiRequestOptions) {
    return apiClient.get<NetworkProbeConfig>('/facility-apps/network-probe', options);
  },
  saveConfig(input: NetworkProbeSaveInput) {
    return apiClient.put<NetworkProbeConfig>('/facility-apps/network-probe', input);
  },
  results(options?: ApiRequestOptions) {
    return apiClient.get<NetworkProbeMatrix>('/facility-apps/network-probe/results', options);
  },
};
