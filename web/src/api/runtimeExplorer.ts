import { apiClient, type ApiClient } from './client';
import type { RuntimeExplorerNodeDto, RuntimeExplorerOperationDto } from '@/types/api';

function nodePath(nodeId: string) {
  return `/runtime-explorer/nodes/${encodeURIComponent(nodeId)}`;
}

function containerPath(nodeId: string, containerId: string) {
  return `${nodePath(nodeId)}/containers/${encodeURIComponent(containerId)}`;
}

export function createRuntimeExplorerApi(client: ApiClient = apiClient) {
  return {
    getNodeRuntime(nodeId: string) {
      return client.get<RuntimeExplorerNodeDto>(nodePath(nodeId));
    },
    restartContainer(nodeId: string, containerId: string) {
      return client.post<RuntimeExplorerOperationDto>(`${containerPath(nodeId, containerId)}/restart`);
    },
    stopContainer(nodeId: string, containerId: string) {
      return client.post<RuntimeExplorerOperationDto>(`${containerPath(nodeId, containerId)}/stop`);
    },
    deleteContainer(nodeId: string, containerId: string) {
      return client.delete<RuntimeExplorerOperationDto>(containerPath(nodeId, containerId));
    },
    prune(nodeId: string) {
      return client.post<RuntimeExplorerOperationDto>(`${nodePath(nodeId)}/prune`);
    },
  };
}

export const runtimeExplorerApi = createRuntimeExplorerApi();
