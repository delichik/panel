import { apiClient, type ApiRequestOptions } from './client';
import type { OperationAccepted } from '@/types/servers';
import type { ContainerDto, ContainerLogs, ImageList, NetworkDto, OperationResult, VolumeDto } from '@/types/resources';

const serverBase = (serverId: string) => `/servers/${encodeURIComponent(serverId)}`;
const resourceId = (id: string) => encodeURIComponent(id);

export const containersApi = {
  containers(serverId: string, options?: ApiRequestOptions) {
    return apiClient.get<ContainerDto[]>(`${serverBase(serverId)}/containers`, options);
  },
  containerLogs(serverId: string, containerId: string, tail = 500, options?: ApiRequestOptions) {
    return apiClient.get<ContainerLogs>(`${serverBase(serverId)}/containers/${resourceId(containerId)}/logs?tail=${tail}`, options);
  },
  containerAction(serverId: string, containerId: string, action: 'start' | 'stop' | 'restart') {
    return apiClient.post<OperationResult>(`${serverBase(serverId)}/containers/${resourceId(containerId)}/${action}`);
  },
  deleteContainer(serverId: string, containerId: string) {
    return apiClient.delete<OperationResult>(`${serverBase(serverId)}/containers/${resourceId(containerId)}`);
  },
  images(serverId: string, options?: ApiRequestOptions) {
    return apiClient.get<ImageList>(`${serverBase(serverId)}/images`, options);
  },
  pullImage(serverId: string, reference: string) {
    return apiClient.post<OperationResult>(`${serverBase(serverId)}/images/pull`, { reference });
  },
  refreshImages(serverId: string) {
    return apiClient.post<OperationAccepted>(`${serverBase(serverId)}/images/refresh`);
  },
  deleteUnusedImages(serverId: string) {
    return apiClient.post<OperationResult>(`${serverBase(serverId)}/images/delete-unused`);
  },
  deleteImage(serverId: string, imageId: string) {
    return apiClient.delete<OperationResult>(`${serverBase(serverId)}/images/${resourceId(imageId)}`);
  },
  upgradeSelectedImages(applicationIds: string[]) {
    return apiClient.post<OperationAccepted>('/images/upgrade-selected', { applicationIds });
  },
  upgradeAllImages() {
    return apiClient.post<OperationAccepted>('/images/upgrade-all');
  },
  networks(serverId: string, options?: ApiRequestOptions) {
    return apiClient.get<NetworkDto[]>(`${serverBase(serverId)}/networks`, options);
  },
  volumes(serverId: string, options?: ApiRequestOptions) {
    return apiClient.get<VolumeDto[]>(`${serverBase(serverId)}/volumes`, options);
  },
  deleteUnusedVolumes(serverId: string) {
    return apiClient.post<OperationResult>(`${serverBase(serverId)}/volumes/delete-unused`);
  },
  deleteVolume(serverId: string, name: string) {
    return apiClient.delete<OperationResult>(`${serverBase(serverId)}/volumes/${resourceId(name)}`);
  },
};
