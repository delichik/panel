export type ApplicationOperationStatus = 'queued' | 'running' | 'succeeded' | 'failed' | 'partial_failed' | 'cancelled' | 'superseded' | 'consistent' | string;

export interface ApplicationOperationDto {
  operationId: string;
  applicationId: string;
  applicationName: string;
  action: string;
  source: string;
  triggeredBy?: string;
  status: ApplicationOperationStatus;
  startedAt?: string;
  finishedAt?: string;
  targetTotal: number;
  targetSucceeded: number;
  targetFailed: number;
  targetServers?: string[];
  latestAt: string;
  failureSummary?: string;
  createdAt: string;
  updatedAt: string;
}

export interface ApplicationOperationStageDto {
  id: string;
  stage: string;
  status: string;
  detail?: string;
  startedAt?: string;
  finishedAt?: string;
}

export interface ApplicationOperationTargetDto {
  id: string;
  operationId?: string;
  applicationId?: string;
  serverId?: string;
  serverName?: string;
  action?: string;
  state?: string;
  status: string;
  stage?: string;
  attempt?: number;
  nextRunAt?: string;
  claimedTaskId?: string;
  containerName?: string;
  desiredState?: string;
  desiredGeneration?: number;
  desiredSpecHash?: string;
  observedState?: string;
  observedExitCode?: string;
  observedError?: string;
  observedGeneration?: number;
  observedSpecHash?: string;
  observedImage?: string;
  observedAt?: string;
  errorCode?: string;
  errorMessage?: string;
  errorDetail?: string;
  createdAt?: string;
  startedAt?: string;
  finishedAt?: string;
  updatedAt?: string;
  stages: ApplicationOperationStageDto[];
}

export interface ApplicationOperationDetailDto {
  operation: ApplicationOperationDto;
  targets: ApplicationOperationTargetDto[];
}

export interface ApplicationOperationListResult {
  items: ApplicationOperationDto[];
  total: number;
  page: number;
  pageSize: number;
}

export interface ApplicationOperationListParams {
  applicationId?: string;
  status?: string;
  source?: string;
  from?: string;
  to?: string;
  page?: number;
  pageSize?: number;
}