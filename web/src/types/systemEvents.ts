export type SystemEventSeverity = 'info' | 'warning' | 'error' | string;

export interface SystemEventDto {
  id: string;
  eventType: string;
  category: string;
  severity: SystemEventSeverity;
  source?: string;
  sourceModule?: string;
  summary: string;
  occurredAt: string;
}

export interface SystemEventListResult {
  items: SystemEventDto[];
  total: number;
  page: number;
  pageSize: number;
}

export interface SystemEventListParams {
  eventType?: string;
  severity?: string;
  from?: string;
  to?: string;
  page?: number;
  pageSize?: number;
}