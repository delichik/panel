import { apiClient, type ApiRequestOptions } from './client';
import type { OperationAccepted, ServerDto, ServerProbeResult, ServerSaveInput } from '@/types/servers';

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

export interface AgentCertificateBundle {
  ca: string;
  certificate: string;
  privateKey: string;
  listenAddress: string;
  agentUrl: string;
  dockerHost: string;
}

export const serversApi = {
  list(options?: ApiRequestOptions) {
    return apiClient.get<ServerDto[]>('/servers', options);
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
  restart(id: string) {
    return apiClient.post<OperationAccepted>(`/servers/${encodeURIComponent(id)}/restart`);
  },
  deployAgent(id: string) {
    return apiClient.post<OperationAccepted>(`/servers/${encodeURIComponent(id)}/agent/deploy`);
  },
  issueAgentCertificate(id: string) {
    return apiClient.post<AgentCertificateBundle>(`/servers/${encodeURIComponent(id)}/agent/certificate`);
  },
  metrics(id: string, range: ServerMetricsRange = '1h', options?: ApiRequestOptions) {
    return apiClient.get<ServerMetricsSeries>(`/servers/${encodeURIComponent(id)}/metrics?range=${encodeURIComponent(range)}`, options);
  },
  installUfw(id: string) {
    return apiClient.post<OperationAccepted>(`/servers/${encodeURIComponent(id)}/ufw/install`);
  },
};
