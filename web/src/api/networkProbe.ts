import { apiClient, type ApiRequestOptions } from './client';
import type { NetworkProbeConfig, NetworkProbeMatrix, NetworkProbeSaveInput, NetworkProbeTargetMatrix } from '@/types/networkProbe';

export const networkProbeFacilityApi = {
  config(options?: ApiRequestOptions) {
    return apiClient.get<NetworkProbeConfig>('/facility-apps/network-probe', options);
  },
  saveConfig(input: NetworkProbeSaveInput) {
    return apiClient.put<NetworkProbeConfig>('/facility-apps/network-probe', input);
  },
  results(options?: ApiRequestOptions) {
    return apiClient.get<NetworkProbeMatrix>('/facility-apps/network-probe/results', options)
      // 旧服务端/nil slice 可能返回 null；统一成数组，避免页面计算属性读 .length/.map 崩溃。
      .then((result) => ({
        ...result,
        nodes: result.nodes ?? [],
        results: result.results ?? [],
      }));
  },
  /** 按目标聚合视图：所有服务器各一条摘要（可含跨源聚合的 24h 历史）。 */
  targets(options?: ApiRequestOptions, includeHistory = true) {
    const query = includeHistory ? '?view=targets&history=1' : '?view=targets';
    return apiClient.get<NetworkProbeTargetMatrix>(`/facility-apps/network-probe/results${query}`, options)
      .then((result) => ({
        nodes: result.nodes ?? [],
        updatedAt: result.updatedAt ?? null,
      }));
  },
};
