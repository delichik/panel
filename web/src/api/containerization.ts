import { apiClient } from './client';

export interface DockerPortDto {
  ip?: string;
  privatePort: number;
  publicPort?: number;
  type: string;
}

export interface DockerContainerDto {
  id: string;
  names: string[];
  image: string;
  imageId: string;
  command: string;
  created: number;
  state: string;
  status: string;
  ports: DockerPortDto[];
  labels: Record<string, string>;
  managed: boolean;
  applicationId?: string;
  instanceId?: string;
}

export interface DockerContainerLogsDto {
  containerId: string;
  logs: string;
}

export interface DockerImageDto {
  id: string;
  repoTags: string[];
  repoDigests: string[];
  created: number;
  size: number;
  reference: string;
  localDigest?: string;
  latestDigest?: string;
  checkable: boolean;
  updateAvailable: boolean;
  checkedAt?: string;
  lastError?: string;
  inUse: boolean;
  applicationIds: string[];
  upgradeable: boolean;
}

export interface DockerImageListDto {
  serverId: string;
  items: DockerImageDto[];
  lastRefreshedAt?: string;
  refreshing: boolean;
}

export interface DockerNetworkDto {
  id: string;
  name: string;
  driver: string;
  scope: string;
  created?: string;
  internal: boolean;
}

export interface DockerVolumeDto {
  name: string;
  driver: string;
  mountpoint: string;
  createdAt?: string;
  inUse: boolean;
  containerCount: number;
}

export interface TaskCreatedDto {
  taskId: string;
}

export interface ResourceOperationDto {
  refreshTaskId?: string;
}

export const containerizationApi = {
  containers(serverId: string) {
    return apiClient.get<DockerContainerDto[]>(`/servers/${serverId}/containers`);
  },
  containerLogs(serverId: string, containerId: string, tail?: number) {
    const params = new URLSearchParams();
    if (tail) params.set('tail', String(tail));
    const query = params.toString();
    return apiClient.get<DockerContainerLogsDto>(`/servers/${serverId}/containers/${encodeURIComponent(containerId)}/logs${query ? `?${query}` : ''}`);
  },
  containerAction(serverId: string, containerId: string, action: 'start' | 'stop' | 'restart') {
    return apiClient.post<ResourceOperationDto>(`/servers/${serverId}/containers/${encodeURIComponent(containerId)}/${action}`);
  },
  deleteContainer(serverId: string, containerId: string) {
    return apiClient.delete<ResourceOperationDto>(`/servers/${serverId}/containers/${encodeURIComponent(containerId)}`);
  },
  images(serverId: string) {
    return apiClient.get<DockerImageListDto>(`/servers/${serverId}/images`);
  },
  pullImage(serverId: string, reference: string) {
    return apiClient.post<ResourceOperationDto>(`/servers/${serverId}/images/pull`, { reference });
  },
  refreshImages(serverId: string) {
    return apiClient.post<TaskCreatedDto>(`/servers/${serverId}/images/refresh`);
  },
  deleteImage(serverId: string, imageId: string) {
    return apiClient.delete<ResourceOperationDto>(`/servers/${serverId}/images/${encodeURIComponent(imageId)}`);
  },
  deleteUnusedImages(serverId: string) {
    return apiClient.post<ResourceOperationDto>(`/servers/${serverId}/images/delete-unused`);
  },
  upgradeSelected(applicationIds: string[]) {
    return apiClient.post<TaskCreatedDto>('/images/upgrade-selected', { applicationIds });
  },
  upgradeAll() {
    return apiClient.post<TaskCreatedDto>('/images/upgrade-all');
  },
  networks(serverId: string) {
    return apiClient.get<DockerNetworkDto[]>(`/servers/${serverId}/networks`);
  },
  volumes(serverId: string) {
    return apiClient.get<DockerVolumeDto[]>(`/servers/${serverId}/volumes`);
  },
  deleteVolume(serverId: string, name: string) {
    return apiClient.delete<ResourceOperationDto>(`/servers/${serverId}/volumes/${encodeURIComponent(name)}`);
  },
  deleteUnusedVolumes(serverId: string) {
    return apiClient.post<ResourceOperationDto>(`/servers/${serverId}/volumes/delete-unused`);
  },
};
