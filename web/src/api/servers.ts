import { apiClient, type ApiRequestOptions } from './client';
import type { OperationAccepted, ServerDto, ServerProbeResult, ServerSaveInput } from '@/types/servers';
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

export interface LoadMetricPoint extends MetricsPoint {
  load1: number;
  load5: number;
  load15: number;
}

export interface ServerMetricsSeries {
  range: string;
  cpu: CpuMetricPoint[];
  memory: MemoryMetricPoint[];
  disk: MemoryMetricPoint[];
  network: NetworkMetricPoint[];
  load: LoadMetricPoint[];
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
  probe(input: ServerSaveInput) {
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
  metrics(id: string, range: ServerMetricsRange = '1h', options?: ApiRequestOptions) {
    return apiClient.get<ServerMetricsSeries>(`/servers/${encodeURIComponent(id)}/metrics?range=${encodeURIComponent(range)}`, options);
  },
  installUfw(id: string) {
    return apiClient.post<OperationAccepted>(`/servers/${encodeURIComponent(id)}/ufw/install`);
  },
};
