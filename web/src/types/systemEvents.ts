export type SystemEventSeverity = 'debug' | 'info' | 'warning' | 'error' | 'critical' | string;

export interface SystemEventDto {
  id: string;
  eventType: string;
  category: string;
  severity: SystemEventSeverity;
  subjectType?: string;
  subjectId?: string;
  subjectName?: string;
  source?: string;
  sourceModule?: string;
  summary: string;
  occurredAt: string;
  detailAvailable: boolean;
  detailPrunedAt?: string;
}

export interface SystemEventLogRefDto {
  label: string;
  source?: string;
  from?: string;
  to?: string;
}

export interface SystemEventDetailDto {
  event: SystemEventDto;
  payload?: unknown;
  error?: string;
  logRefs?: SystemEventLogRefDto[];
  taskRefs?: string[];
  targetRefs?: string[];
}

export interface SystemEventListResult {
  items: SystemEventDto[];
  total: number;
  page: number;
  pageSize: number;
}

export interface SystemEventListParams {
  category?: string;
  severity?: string;
  subjectId?: string;
  eventType?: string;
  from?: string;
  to?: string;
  page?: number;
  pageSize?: number;
}
