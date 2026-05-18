export type TaskStatus = 'queued' | 'running' | 'completed' | 'failed' | 'cancelled';
export type TaskStage = 'connecting' | 'preparing' | 'running' | 'verifying' | 'finalizing' | string;

export interface OSInfoDto {
  id: string;
  versionId: string;
  prettyName: string;
  supported: boolean;
}

export interface SudoInfoDto {
  passwordless: boolean;
  lastCheckedAt: string | null;
}

export interface ServerDto {
  id: string;
  name: string;
  host: string;
  port: number;
  sshUsername: string;
  credentialId: string | null;
  labels?: string[];
  notes?: string;
  os?: OSInfoDto | null;
  sudo?: SudoInfoDto | null;
  reachable: boolean;
  lastCheckedAt: string | null;
  lastError?: string;
  createdAt: string;
  updatedAt: string;
}

export interface CredentialDto {
  id: string;
  name: string;
  type: 'password' | 'private_key';
  username: string;
  createdAt: string;
  updatedAt: string;
}

export interface OverviewServerDto {
  id: string;
  name: string;
  host: string;
  supported: boolean;
  reachable: boolean;
  metricsFresh: boolean;
  packageUpdateCount: number;
  lastMetricsAt: string | null;
  lastPackageRefreshAt: string | null;
}

export interface OverviewDto {
  servers: OverviewServerDto[];
}

export type MetricsRange = '1h' | '6h' | '24h';

export interface CpuPointDto {
  time: string;
  usagePercent: number;
}

export interface MemoryPointDto {
  time: string;
  usedBytes: number;
  totalBytes: number;
}

export interface DiskPointDto {
  time: string;
  usedBytes: number;
  totalBytes: number;
}

export interface NetworkPointDto {
  time: string;
  rxBytesPerSecond: number;
  txBytesPerSecond: number;
}

export interface MetricsSeriesDto {
  range: MetricsRange;
  cpu: CpuPointDto[];
  memory: MemoryPointDto[];
  disk: DiskPointDto[];
  network: NetworkPointDto[];
}

export interface PackageUpdateDto {
  name: string;
  installedVersion: string;
  candidateVersion: string;
  source: string;
}

export interface PackageUpdatesDto {
  serverId: string;
  lastRefreshedAt: string | null;
  updates: PackageUpdateDto[];
}

export interface TaskDto {
  id: string;
  type: string;
  serverId: string | null;
  status: TaskStatus;
  stage: TaskStage;
  percentage: number | null;
  summary: string;
  error?: string;
  createdAt: string;
  startedAt: string | null;
  finishedAt: string | null;
}

export interface TaskLogDto {
  cursor: number;
  time: string;
  stream: 'system' | 'stdout' | 'stderr' | string;
  line: string;
}

export interface TaskLogsDto {
  nextCursor: number;
  logs: TaskLogDto[];
}

export interface RuntimeSettingsDto {
  listenAddress: string;
  appDatabase: string;
  metricsDatabase: string;
  dataRoot: string;
  metricsRetentionDays: number;
  metricsCollectionIntervalSeconds: number;
  cleanupSchedule: string;
}

export interface RuntimeSettingsUpdate {
  metricsRetentionDays: number;
  metricsCollectionIntervalSeconds: number;
  cleanupSchedule: string;
}
