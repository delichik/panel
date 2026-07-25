export type HealthState = 'healthy' | 'warning' | 'critical' | 'unknown';

export interface ApiEnvelope<T> {
  data?: T;
  error?: {
    code?: string;
    message?: string;
    details?: unknown;
  };
}

export interface DashboardCard {
  id: string;
  labelKey: string;
  value: string;
  detailKey: string;
  state: HealthState;
  progress: number;
}

export interface DashboardIssue {
  id: string;
  subject: string;
  messageKey: string;
  state: HealthState;
}

export interface DashboardDto {
  cards: DashboardCard[];
  issues: DashboardIssue[];
  resourceCells: HealthState[];
}

export type CollectionKind =
  | 'servers'
  | 'credentials'
  | 'firewall'
  | 'fail2ban'
  | 'packages'
  | 'containers'
  | 'images'
  | 'networks'
  | 'volumes'
  | 'applications'
  | 'facilityApps'
  | 'dns'
  | 'certificates'
  | 'selfSigned'
  | 'keys'
  | 'applicationOperations'
  | 'systemEvents'
  | 'tasks'
  | 'settings'
  | 'debug';

export interface DetailMetric {
  labelKey: string;
  value: string;
  state?: HealthState;
}

export interface OperationRow {
  id: string;
  name: string;
  summaryKey: string;
  state: HealthState;
  scope: string;
  updatedAt: string;
  metrics: DetailMetric[];
  records: Array<Record<string, string>>;
  primaryActionKey?: string;
  destructiveActionKey?: string;
}

export interface CollectionDto {
  kind: CollectionKind;
  rows: OperationRow[];
}

export interface OperationAcceptedDto {
  taskId: string;
  acceptedAt: string;
}
