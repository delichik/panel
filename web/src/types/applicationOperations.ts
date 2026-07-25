export type ApplicationOperationStatus = 'queued' | 'running' | 'succeeded' | 'failed' | 'partial_failed' | 'cancelled' | string;

export interface ApplicationOperationDto {
  operationId: string;
  applicationId: string;
  applicationNameSnapshot: string;
  action: string;
  source: string;
  triggeredBy?: string;
  triggerReason?: string;
  status: ApplicationOperationStatus;
  startedAt: string;
  finishedAt?: string;
  targetTotal: number;
  targetSucceeded: number;
  targetFailed: number;
  latestEventAt: string;
  detailAvailable: boolean;
  detailPrunedAt?: string;
  failureSummary?: string;
}

export interface ApplicationOperationTargetDto {
  id: string;
  serverId?: string;
  serverName?: string;
  action: string;
  status: string;
  stage?: string;
  error?: string;
  logRef?: string;
  updatedAt?: string;
}

export interface ApplicationOperationEventDto {
  id: string;
  eventType: string;
  severity: string;
  summary: string;
  occurredAt: string;
  detailAvailable: boolean;
}

export interface ApplicationOperationDetailDto {
  operation: ApplicationOperationDto;
  targets: ApplicationOperationTargetDto[];
  events: ApplicationOperationEventDto[];
}

export interface ApplicationOperationListResult {
  items: ApplicationOperationDto[];
  total: number;
  page: number;
  pageSize: number;
}

export interface ApplicationOperationListParams {
  applicationId?: string;
  action?: string;
  source?: string;
  status?: string;
  from?: string;
  to?: string;
  page?: number;
  pageSize?: number;
}
