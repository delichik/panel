export type TaskStatus = 'queued' | 'scheduled' | 'running' | 'completed' | 'failed' | 'failed_retryable' | 'blocked' | 'cancelled';

export interface TaskDto {
  id: string;
  operationId: string;
  type: string;
  status: TaskStatus | string;
  stage: string;
  percentage?: number | null;
  summary: string;
  error?: string;
  serverId?: string;
  resourceType?: string;
  resourceId?: string;
  triggerType?: string;
  triggeredBy?: string;
  retryCount: number;
  maxRetries: number;
  nextRunAt?: string;
  createdAt: string;
  startedAt?: string;
  finishedAt?: string;
  allowRunNow: boolean;
  allowRetry: boolean;
}

export interface TaskListResult {
  items: TaskDto[];
  total: number;
  page: number;
  pageSize: number;
}

export interface TaskStep {
  id: string;
  taskId: string;
  step: string;
  status: string;
  percentage: number;
  metadataJson?: string;
  startedAt?: string;
  finishedAt?: string;
  error?: string;
}

export interface TaskLog {
  cursor: number;
  time: string;
  stream: string;
  line: string;
}

export interface TaskLogsResult {
  nextCursor: number;
  logs: TaskLog[];
}

export interface TaskOperationGroup {
  operationId: string;
  type: string;
  title: string;
  status: string;
  latestAt: string;
  tasks: TaskDto[];
  failed: number;
  running: number;
  completed: number;
  actionable: boolean;
}
