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

export interface DockerCapabilityDto {
  serverId: string;
  dockerInstalled: boolean;
  dockerVersion: string;
  composeInstalled: boolean;
  composeVersion: string;
  supported?: boolean;
  lastCheckedAt?: string | null;
  checkedAt?: string | null;
  lastError?: string | null;
  stale?: boolean;
  pending?: boolean;
  taskId?: string;
}

export interface DockerRuntimeServiceDto {
  id: string;
  name: string;
  image: string;
  status: string;
  state?: string;
  command?: string;
  project?: string;
  service?: string;
  projectName?: string | null;
  serviceName?: string | null;
  ports?: string[] | string;
  labels?: Record<string, string>;
  managed?: boolean;
  createdAt?: string | null;
}

export interface DockerComposeStatusDto {
  projectName?: string;
  project?: string;
  status?: string;
  state?: string;
  services?: DockerRuntimeServiceDto[];
  checkedAt?: string | null;
  lastError?: string | null;
}

export interface DockerNetworkDto {
  id: string;
  name: string;
  driver: string;
  scope?: string;
  internal?: boolean;
  attachable?: boolean;
  labels?: Record<string, string>;
  managed?: boolean;
  createdAt?: string | null;
}

export interface DockerVolumeDto {
  name: string;
  driver: string;
  mountpoint?: string;
  scope?: string;
  labels?: Record<string, string>;
  managed?: boolean;
  createdAt?: string | null;
}

export interface DockerImageDto {
  id: string;
  repository: string;
  tag: string;
  digest?: string;
  size?: string;
  createdAt?: string | null;
  labels?: Record<string, string>;
  managed?: boolean;
  update?: DockerImageUpdateDto | null;
  updateAvailable?: boolean;
  currentVersion?: string | null;
  latestVersion?: string | null;
}

export interface DockerImageUpdateDto {
  imageId: string;
  repository: string;
  tag: string;
  currentDigest?: string | null;
  latestDigest?: string | null;
  currentVersion?: string | null;
  latestVersion?: string | null;
  updateAvailable: boolean;
  checkedAt?: string | null;
  lastError?: string | null;
  error?: string | null;
}

export interface DockerImageUpdatesDto {
  serverId: string;
  checkedAt: string | null;
  updates: DockerImageUpdateDto[];
}

export interface DockerRuntimeListDto<T> {
  serverId: string;
  lastRefreshedAt?: string | null;
  items: T[];
}

export interface ComposeTemplateVariableDto {
  name: string;
  label?: string;
  type?: 'string' | 'number' | 'boolean' | 'secret' | string;
  defaultValue?: unknown;
  required?: boolean;
  description?: string;
}

export interface ComposeVisualServiceDto {
  name: string;
  image: string;
  labels?: Record<string, string>;
  ports?: string[];
  environment?: Record<string, string>;
  volumes?: string[];
  command?: string;
}

export interface ComposeVisualModelDto {
  version?: string;
  services: ComposeVisualServiceDto[];
}

export interface ServiceTemplateDto {
  id: string;
  name: string;
  description?: string;
  version: number;
  composeYaml: string;
  visual?: ComposeVisualModelDto | Record<string, unknown> | null;
  variables?: ComposeTemplateVariableDto[];
  fileCount?: number;
  linkedServiceCount?: number;
  createdAt: string;
  updatedAt: string;
}

export interface ServiceTemplateInputDto {
  name: string;
  description?: string;
  composeYaml: string;
  visual?: ComposeVisualModelDto | Record<string, unknown> | null;
  variables?: ComposeTemplateVariableDto[];
}

export type TemplateFileKind = 'template' | 'binary' | string;

export interface TemplateFileDto {
  id: string;
  templateId?: string;
  kind: TemplateFileKind;
  path: string;
  content?: string;
  base64Content?: string;
  sizeBytes?: number;
  mode?: string;
  createdAt?: string;
  updatedAt?: string;
}

export interface TemplateFileInputDto {
  path: string;
  content?: string;
  base64Content?: string;
  mode?: string;
}

export interface ComposeValidationIssueDto {
  path?: string;
  variable?: string;
  message: string;
  severity?: 'error' | 'warning' | string;
}

export interface ComposeValidationResultDto {
  valid: boolean;
  issues?: ComposeValidationIssueDto[];
  renderedYaml?: string;
}

export interface ComposeRenderPreviewDto {
  renderedYaml: string;
  files?: TemplateFileDto[];
  values?: Record<string, unknown>;
  issues?: ComposeValidationIssueDto[];
}

export type ComposeServiceStatus = 'draft' | 'deployed' | 'running' | 'stopped' | 'failed' | string;
export type ComposeServiceSyncStatus = 'synced' | 'drifted' | 'pending' | 'unknown' | string;

export interface ComposeServiceDto {
  id: string;
  name: string;
  templateId: string;
  templateName?: string;
  serverId: string;
  serverName?: string;
  remotePath: string;
  values: Record<string, unknown>;
  status?: ComposeServiceStatus;
  syncStatus?: ComposeServiceSyncStatus;
  drift?: boolean;
  runtimeStatus?: string | null;
  lastAppliedTemplateVersion?: number | null;
  templateVersion?: number | null;
  lastTaskId?: string | null;
  createdAt: string;
  updatedAt: string;
}

export interface ComposeServiceInputDto {
  name: string;
  templateId: string;
  serverId: string;
  remotePath: string;
  values: Record<string, unknown>;
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

export interface TaskListDto {
  items: TaskDto[];
  total: number;
  page: number;
  pageSize: number;
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
