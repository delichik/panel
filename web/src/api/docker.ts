import { apiClient, type ApiClient } from './client';
import type {
  DockerCapabilityDto,
  DockerComposeStatusDto,
  DockerImageDto,
  DockerNetworkDto,
  DockerRuntimeListDto,
  DockerRuntimeServiceDto,
  DockerVolumeDto,
} from '@/types/api';
import type { TaskCreatedDto } from './servers';

type RefreshResult = DockerCapabilityDto | TaskCreatedDto;

export function createDockerApi(client: ApiClient = apiClient) {
  return {
    getCapability(serverId: string) {
      return client.get<DockerCapabilityDto>(`/servers/${serverId}/docker/capability`);
    },
    refreshCapability(serverId: string) {
      return client.post<RefreshResult>(`/servers/${serverId}/docker/refresh`);
    },
    listServices(serverId: string) {
      return client.get<DockerRuntimeListDto<DockerRuntimeServiceDto>>(`/servers/${serverId}/docker/services`);
    },
    getProjectStatus(serverId: string, projectName: string) {
      return client.get<DockerComposeStatusDto>(
        `/servers/${serverId}/docker/projects/${encodeURIComponent(projectName)}/status`,
      );
    },
    startContainer(serverId: string, containerId: string) {
      return client.post<TaskCreatedDto>(`/servers/${serverId}/docker/containers/${encodeURIComponent(containerId)}/start`);
    },
    stopContainer(serverId: string, containerId: string) {
      return client.post<TaskCreatedDto>(`/servers/${serverId}/docker/containers/${encodeURIComponent(containerId)}/stop`);
    },
    deleteContainer(serverId: string, containerId: string) {
      return client.delete<TaskCreatedDto>(`/servers/${serverId}/docker/containers/${encodeURIComponent(containerId)}`);
    },
    listNetworks(serverId: string) {
      return client.get<DockerRuntimeListDto<DockerNetworkDto>>(`/servers/${serverId}/docker/networks`);
    },
    deleteNetwork(serverId: string, networkId: string) {
      return client.delete<TaskCreatedDto>(`/servers/${serverId}/docker/networks/${encodeURIComponent(networkId)}`);
    },
    pruneNetworks(serverId: string) {
      return client.post<TaskCreatedDto>(`/servers/${serverId}/docker/networks/prune`);
    },
    listVolumes(serverId: string) {
      return client.get<DockerRuntimeListDto<DockerVolumeDto>>(`/servers/${serverId}/docker/volumes`);
    },
    deleteVolume(serverId: string, volumeId: string) {
      return client.delete<TaskCreatedDto>(`/servers/${serverId}/docker/volumes/${encodeURIComponent(volumeId)}`);
    },
    pruneVolumes(serverId: string) {
      return client.post<TaskCreatedDto>(`/servers/${serverId}/docker/volumes/prune`);
    },
    listImages(serverId: string) {
      return client.get<DockerRuntimeListDto<DockerImageDto>>(`/servers/${serverId}/docker/images`);
    },
    checkImageUpdates(serverId: string) {
      return client.post<TaskCreatedDto>(`/servers/${serverId}/docker/images/check-updates`);
    },
    updateSelectedImages(serverId: string, imageIds: string[]) {
      return client.post<TaskCreatedDto>(`/servers/${serverId}/docker/images/update-selected`, { imageIds });
    },
    updateAllImages(serverId: string) {
      return client.post<TaskCreatedDto>(`/servers/${serverId}/docker/images/update-all`);
    },
    deleteImage(serverId: string, imageId: string) {
      return client.delete<TaskCreatedDto>(`/servers/${serverId}/docker/images/${encodeURIComponent(imageId)}`);
    },
    pruneImages(serverId: string) {
      return client.post<TaskCreatedDto>(`/servers/${serverId}/docker/images/prune`);
    },
  };
}

export const dockerApi = createDockerApi();
