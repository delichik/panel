import { apiClient, type ApiClient } from './client';
import type {
  ContainerServiceDto,
  ContainerServiceFileDto,
  ContainerServiceFileInputDto,
  ContainerServiceInputDto,
  ContainerServiceLogsDto,
  ContainerServiceRuntimeDto,
  ContainerServiceRuntimeOperationDto,
  ContainerServiceValidationResultDto,
  DependencyImpactPreviewDto,
  RenderPreviewDto,
  SchedulePreviewDto,
} from '@/types/api';

function servicePath(serviceId: string) {
  return `/container-services/${encodeURIComponent(serviceId)}`;
}

function filePath(serviceId: string, fileId: string) {
  return `${servicePath(serviceId)}/files/${encodeURIComponent(fileId)}`;
}

export function createContainerServicesApi(client: ApiClient = apiClient) {
  return {
    list() {
      return client.get<ContainerServiceDto[]>('/container-services');
    },
    create(input: ContainerServiceInputDto) {
      return client.post<ContainerServiceDto>('/container-services', input);
    },
    get(serviceId: string) {
      return client.get<ContainerServiceDto>(servicePath(serviceId));
    },
    update(serviceId: string, input: ContainerServiceInputDto) {
      return client.put<ContainerServiceDto>(servicePath(serviceId), input);
    },
    delete(serviceId: string) {
      return client.delete(servicePath(serviceId));
    },
    validate(serviceId: string, input?: ContainerServiceInputDto) {
      return client.post<ContainerServiceValidationResultDto>(`${servicePath(serviceId)}/validate`, input);
    },
    renderPreview(serviceId: string, input?: ContainerServiceInputDto) {
      return client.post<RenderPreviewDto>(`${servicePath(serviceId)}/render-preview`, input);
    },
    schedulePreview(serviceId: string, input?: ContainerServiceInputDto) {
      return client.post<SchedulePreviewDto>(`${servicePath(serviceId)}/schedule-preview`, input);
    },
    reconcile(serviceId: string) {
      return client.post<ContainerServiceRuntimeOperationDto>(`${servicePath(serviceId)}/reconcile`);
    },
    restart(serviceId: string) {
      return client.post<ContainerServiceRuntimeOperationDto>(`${servicePath(serviceId)}/restart`);
    },
    enablePreview(serviceId: string) {
      return client.post<DependencyImpactPreviewDto>(`${servicePath(serviceId)}/enable-preview`);
    },
    enable(serviceId: string) {
      return client.post<ContainerServiceRuntimeOperationDto>(`${servicePath(serviceId)}/enable`);
    },
    disablePreview(serviceId: string) {
      return client.post<DependencyImpactPreviewDto>(`${servicePath(serviceId)}/disable-preview`);
    },
    disable(serviceId: string) {
      return client.post<ContainerServiceRuntimeOperationDto>(`${servicePath(serviceId)}/disable`);
    },
    listFiles(serviceId: string) {
      return client.get<ContainerServiceFileDto[]>(`${servicePath(serviceId)}/files`);
    },
    createFile(serviceId: string, input: ContainerServiceFileInputDto) {
      return client.post<ContainerServiceFileDto>(`${servicePath(serviceId)}/files`, input);
    },
    updateFile(serviceId: string, fileId: string, input: ContainerServiceFileInputDto) {
      return client.put<ContainerServiceFileDto>(filePath(serviceId, fileId), input);
    },
    deleteFile(serviceId: string, fileId: string) {
      return client.delete(filePath(serviceId, fileId));
    },
    runtime(serviceId: string) {
      return client.get<ContainerServiceRuntimeDto>(`${servicePath(serviceId)}/runtime`);
    },
    logs(serviceId: string, tail = 200) {
      return client.get<ContainerServiceLogsDto>(`${servicePath(serviceId)}/logs?tail=${tail}`);
    },
  };
}

export const containerServicesApi = createContainerServicesApi();
