import { apiClient, type ApiClient } from './client';
import type { TaskCreatedDto } from './servers';
import type {
  ComposeRenderPreviewDto,
  ComposeServiceDto,
  ComposeServiceInputDto,
  ComposeValidationResultDto,
  ServiceTemplateDto,
  ServiceTemplateInputDto,
  TemplateFileDto,
  TemplateFileInputDto,
} from '@/types/api';

function serviceTemplatePath(templateId: string) {
  return `/service-templates/${encodeURIComponent(templateId)}`;
}

function servicePath(serviceId: string) {
  return `/services/${encodeURIComponent(serviceId)}`;
}

export function createComposeApi(client: ApiClient = apiClient) {
  return {
    listTemplates() {
      return client.get<ServiceTemplateDto[]>('/service-templates');
    },
    createTemplate(input: ServiceTemplateInputDto) {
      return client.post<ServiceTemplateDto>('/service-templates', input);
    },
    getTemplate(templateId: string) {
      return client.get<ServiceTemplateDto>(serviceTemplatePath(templateId));
    },
    updateTemplate(templateId: string, input: ServiceTemplateInputDto) {
      return client.put<ServiceTemplateDto>(serviceTemplatePath(templateId), input);
    },
    deleteTemplate(templateId: string) {
      return client.delete(serviceTemplatePath(templateId));
    },
    validateTemplate(templateId: string, input: { serverId?: string; values?: Record<string, unknown> } = {}) {
      return client.post<ComposeValidationResultDto>(`${serviceTemplatePath(templateId)}/validate`, input);
    },
    renderTemplatePreview(templateId: string, input: { serverId?: string; values?: Record<string, unknown> } = {}) {
      return client.post<ComposeRenderPreviewDto>(`${serviceTemplatePath(templateId)}/render-preview`, input);
    },
    listTemplateServices(templateId: string) {
      return client.get<ComposeServiceDto[]>(`${serviceTemplatePath(templateId)}/services`);
    },
    listTemplateFiles(templateId: string) {
      return client.get<TemplateFileDto[]>(`${serviceTemplatePath(templateId)}/files`);
    },
    createTemplateTextFile(templateId: string, input: TemplateFileInputDto) {
      return client.post<TemplateFileDto>(`${serviceTemplatePath(templateId)}/files/template`, input);
    },
    createTemplateBinaryFile(templateId: string, input: TemplateFileInputDto) {
      return client.post<TemplateFileDto>(`${serviceTemplatePath(templateId)}/files/binary`, input);
    },
    updateTemplateFile(templateId: string, fileId: string, input: TemplateFileInputDto) {
      return client.put<TemplateFileDto>(
        `${serviceTemplatePath(templateId)}/files/${encodeURIComponent(fileId)}`,
        input,
      );
    },
    deleteTemplateFile(templateId: string, fileId: string) {
      return client.delete(`${serviceTemplatePath(templateId)}/files/${encodeURIComponent(fileId)}`);
    },
    getServerVariables(serverId: string) {
      return client.get<Record<string, unknown>>(`/servers/${encodeURIComponent(serverId)}/variables`);
    },
    updateServerVariables(serverId: string, variables: Record<string, unknown>) {
      return client.put<Record<string, unknown>>(`/servers/${encodeURIComponent(serverId)}/variables`, variables);
    },
    listServices() {
      return client.get<ComposeServiceDto[]>('/services');
    },
    createService(input: ComposeServiceInputDto) {
      return client.post<ComposeServiceDto>('/services', input);
    },
    getService(serviceId: string) {
      return client.get<ComposeServiceDto>(servicePath(serviceId));
    },
    updateService(serviceId: string, input: ComposeServiceInputDto) {
      return client.put<ComposeServiceDto>(servicePath(serviceId), input);
    },
    deleteService(serviceId: string) {
      return client.delete(servicePath(serviceId));
    },
    renderService(serviceId: string) {
      return client.post<ComposeRenderPreviewDto>(`${servicePath(serviceId)}/render`);
    },
    deployService(serviceId: string) {
      return client.post<TaskCreatedDto>(`${servicePath(serviceId)}/deploy`);
    },
    syncService(serviceId: string) {
      return client.post<TaskCreatedDto>(`${servicePath(serviceId)}/sync`);
    },
    restartService(serviceId: string) {
      return client.post<TaskCreatedDto>(`${servicePath(serviceId)}/restart`);
    },
    stopService(serviceId: string) {
      return client.post<TaskCreatedDto>(`${servicePath(serviceId)}/stop`);
    },
    removeService(serviceId: string) {
      return client.post<TaskCreatedDto>(`${servicePath(serviceId)}/remove`);
    },
  };
}

export const composeApi = createComposeApi();
