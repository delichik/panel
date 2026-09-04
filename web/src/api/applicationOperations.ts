import { apiClient } from './client';
import type { ApplicationOperationDetailDto, ApplicationOperationListParams, ApplicationOperationListResult } from '@/types/applicationOperations';

function id(value: string) {
  return encodeURIComponent(value);
}

function query(params: ApplicationOperationListParams) {
  const search = new URLSearchParams();
  Object.entries(params).forEach(([key, value]) => {
    if (value !== undefined && value !== '') search.set(key, String(value));
  });
  return search.size ? `?${search}` : '';
}

export const applicationOperationsApi = {
  list(params: ApplicationOperationListParams = {}) {
    return apiClient.get<ApplicationOperationListResult>(`/application-operations${query(params)}`);
  },
  get(operationId: string) {
    return apiClient.get<ApplicationOperationDetailDto>(`/application-operations/${id(operationId)}`);
  },
};
