import { apiClient, type ApiRequestOptions } from './client';
import type { OperationAccepted, ServerDto, ServerProbeInput, ServerProbeResult, ServerSaveInput } from '@/types/servers';
import type { NetworkProbeServerResults } from '@/types/networkProbe';
import type { ListPage } from '@/types/pagination';

export interface MetricsPoint {
  time: string;
}

export interface CpuMetricPoint extends MetricsPoint {
  usagePercent: number;
}

export interface MemoryMetricPoint extends MetricsPoint {
  usedBytes: number;
  totalBytes: number;
}

export interface NetworkMetricPoint extends MetricsPoint {
  rxBytesPerSecond: number;
  txBytesPerSecond: number;
}

export interface DiskIOMetricPoint extends MetricsPoint {
  readBytesPerSecond: number;
  writeBytesPerSecond: number;
}

export interface LoadMetricPoint extends MetricsPoint {
  load1: number;
  load5: number;
  load15: number;
}

export interface ServerMetricsLatest {
  time: string;
  cpuUsagePercent: number;
  memoryUsedBytes: number;
  memoryTotalBytes: number;
  diskUsedBytes: number;
  diskTotalBytes: number;
  diskSampledAt: string | null;
  load1: number;
  rxBytesPerSecond: number;
  txBytesPerSecond: number;
}

export interface ServerMetricsSeries {
  range: string;
  cpu: CpuMetricPoint[];
  memory: MemoryMetricPoint[];
  /** 磁盘仅保留当前值：最近一次有效磁盘采样，可能为 null。 */
  diskCurrent: MemoryMetricPoint | null;
  /** 读写字节速率历史序列。 */
  diskIo: DiskIOMetricPoint[];
  network: NetworkMetricPoint[];
  load: LoadMetricPoint[];
  /** 仅桶查询返回：最新原始样本，供卡片显示当前值。 */
  latest?: ServerMetricsLatest;
}

export type ServerMetricsRange = '1h' | '6h' | '1d' | '7d';

export const serversApi = {
  list(options?: ApiRequestOptions) {
    return apiClient.get<ListPage<ServerDto>>('/servers?pageSize=200', options).then((result) => result.items);
  },
  listPage(params: { page?: number; pageSize?: number; q?: string } = {}, options?: ApiRequestOptions) {
    const query = new URLSearchParams();
    Object.entries(params).forEach(([name, value]) => { if (value !== undefined && value !== '') query.set(name, String(value)); });
    return apiClient.get<ListPage<ServerDto>>(`/servers${query.size ? `?${query}` : ''}`, options);
  },
  get(id: string, options?: ApiRequestOptions) {
    return apiClient.get<ServerDto>(`/servers/${encodeURIComponent(id)}`, options);
  },
  create(input: ServerSaveInput) {
    return apiClient.post<ServerDto>('/servers', input);
  },
  update(id: string, input: ServerSaveInput) {
    return apiClient.put<ServerDto>(`/servers/${encodeURIComponent(id)}`, input);
  },
  delete(id: string) {
    return apiClient.delete<void>(`/servers/${encodeURIComponent(id)}`);
  },
  probe(input: ServerProbeInput) {
    return apiClient.post<ServerProbeResult>('/servers/probe', input);
  },
  test(id: string) {
    return apiClient.post<ServerDto>(`/servers/${encodeURIComponent(id)}/test`);
  },
  trustHostKey(id: string) {
    return apiClient.post<ServerDto>(`/servers/${encodeURIComponent(id)}/trust-host-key`);
  },
  restart(id: string) {
    return apiClient.post<OperationAccepted>(`/servers/${encodeURIComponent(id)}/restart`);
  },
  deployAgent(id: string) {
    return apiClient.post<OperationAccepted>(`/servers/${encodeURIComponent(id)}/agent/deploy`);
  },
  /** 创建或复用 server_tailscale_apply 任务；返回的是受理回执，不是执行结果。 */
  applyTailscale(id: string) {
    return apiClient.post<OperationAccepted>(`/servers/${encodeURIComponent(id)}/tailscale/apply`);
  },
  metrics(id: string, range: ServerMetricsRange = '1h', options?: ApiRequestOptions) {
    return apiClient.get<ServerMetricsSeries>(`/servers/${encodeURIComponent(id)}/metrics?range=${encodeURIComponent(range)}`, options);
  },
  /** 24h 历史桶查询：服务端按 bucketSeconds 聚合，并返回最新原始样本 latest。 */
  metricsBucketed(id: string, range: ServerMetricsRange, bucketSeconds: number, options?: ApiRequestOptions) {
    return apiClient.get<ServerMetricsSeries>(`/servers/${encodeURIComponent(id)}/metrics?range=${encodeURIComponent(range)}&bucket=${bucketSeconds}`, options);
  },
  /** 该节点作为探测源的最新结果与目标表；includeHistory 时附带最近 24h 历史。 */
  networkProbes(id: string, options?: ApiRequestOptions, includeHistory = false) {
    const query = includeHistory ? '?history=1' : '';
    return apiClient.get<NetworkProbeServerResults>(`/servers/${encodeURIComponent(id)}/network-probes${query}`, options);
  },
};
