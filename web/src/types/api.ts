export type TaskStatus = 'queued' | 'scheduled' | 'running' | 'completed' | 'failed' | 'failed_retryable' | 'blocked' | 'cancelled';
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
  traits?: Record<string, string>;
  notes?: string;
  os?: OSInfoDto | null;
  sudo?: SudoInfoDto | null;
  reachable: boolean;
  loadAverage: string | null;
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
  loadAverage: string | null;
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
  refreshing: boolean;
}

export interface PackageRefreshDto {
  serverId: string;
  refreshing: boolean;
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

export type RuntimeStatus = 'missing' | 'starting' | 'running' | 'healthy' | 'unhealthy' | 'exited' | 'unknown' | 'stale' | string;

export interface ValidationIssueDto {
  path?: string;
  message: string;
  severity?: 'error' | 'warning' | string;
}

export interface ContainerServiceDto {
  id: string;
  name: string;
  enabled: boolean;
  composeServiceYaml: string;
  variables?: Record<string, string>;
  selector?: Record<string, string>;
  generation: number;
  specRevision?: string;
  specHash?: string;
  runtimeStatus?: RuntimeStatus | null;
  runtimeGeneration?: number | null;
  runtimeSpecRevision?: string | null;
  nodeId?: string | null;
  nodeName?: string | null;
  dependencyNames?: string[];
  dependentNames?: string[];
  lastTask?: TaskDto | null;
  lastTaskId?: string | null;
  lastError?: string | null;
  createdAt: string;
  updatedAt: string;
}

export interface ContainerServiceInputDto {
  name?: string;
  enabled: boolean;
  composeServiceYaml: string;
  variables?: Record<string, string>;
  selector?: Record<string, string>;
}

export interface ContainerServiceFileDto {
  id: string;
  serviceId?: string;
  path: string;
  kind: 'template' | 'binary' | string;
  contentType?: string;
  size?: number;
  sha256?: string;
  content?: string;
  base64Content?: string;
  createdAt?: string;
  updatedAt?: string;
}

export interface ContainerServiceFileInputDto {
  path: string;
  kind: 'template' | 'binary' | string;
  content?: string;
  base64Content?: string;
  contentType?: string;
}

export interface ContainerServiceValidationResultDto {
  valid: boolean;
  issues?: ValidationIssueDto[];
  dependencyNames?: string[];
  dangerousMountWarnings?: ValidationIssueDto[];
}

export interface RenderPreviewDto {
  composeYaml?: string;
  overrideYaml?: string;
  manifestJson?: string;
  renderedYaml?: string;
  files?: ContainerServiceFileDto[];
  issues?: ValidationIssueDto[];
}

export interface SchedulePreviewCandidateDto {
  nodeId: string;
  nodeName?: string;
  eligible: boolean;
  reasons?: string[];
}

export interface SchedulePreviewDto {
  selectedNodeId?: string | null;
  selectedNodeName?: string | null;
  candidates?: SchedulePreviewCandidateDto[];
  errors?: ValidationIssueDto[];
  warnings?: ValidationIssueDto[];
}

export interface DependencyImpactPreviewDto {
  operation?: 'enable' | 'disable' | string;
  targetServiceId?: string;
  targetServiceName?: string;
  affectedServices?: ContainerServiceDto[];
  dependencyOrder?: string[];
  disableOrder?: string[];
  expectedTasks?: TaskDto[];
  validationErrors?: ValidationIssueDto[];
  operationId?: string;
  tasks?: TaskDto[];
}

export interface ContainerServiceRuntimeDto {
  serviceId: string;
  serviceName: string;
  nodeId?: string | null;
  nodeName?: string | null;
  status: RuntimeStatus;
  observedGeneration?: number | null;
  observedSpecRevision?: string | null;
  labels?: Record<string, string>;
  ports?: string[];
  containerId?: string | null;
  stale?: boolean;
  error?: string | null;
  observedAt?: string | null;
}

export interface ContainerServiceLogsDto {
  serviceId: string;
  tail: number;
  lines: string[];
}

export interface ContainerServiceRuntimeOperationDto {
  operationId?: string;
  taskId?: string;
  tasks?: TaskDto[];
}

export interface RuntimeExplorerContainerDto {
  id: string;
  name: string;
  image: string;
  state: string;
  status: string;
  health?: string | null;
  ports?: string[] | string;
  labels?: Record<string, string>;
  managed: boolean;
  serviceId?: string | null;
  serviceName?: string | null;
  observedAt?: string | null;
  stale?: boolean;
  error?: string | null;
}

export interface RuntimeExplorerNetworkDto {
  id: string;
  name: string;
  driver?: string;
  scope?: string;
  labels?: Record<string, string>;
  managed: boolean;
}

export interface RuntimeExplorerVolumeDto {
  name: string;
  driver?: string;
  mountpoint?: string;
  labels?: Record<string, string>;
  managed: boolean;
}

export interface RuntimeExplorerImageDto {
  id: string;
  repository: string;
  tag: string;
  size?: string;
  labels?: Record<string, string>;
  managed: boolean;
}

export interface RuntimeExplorerNodeDto {
  nodeId: string;
  nodeName?: string;
  capability?: DockerCapabilityDto | null;
  containers: RuntimeExplorerContainerDto[];
  networks: RuntimeExplorerNetworkDto[];
  volumes: RuntimeExplorerVolumeDto[];
  images: RuntimeExplorerImageDto[];
  stale?: boolean;
  error?: string | null;
  observedAt?: string | null;
}

export interface RuntimeExplorerOperationDto {
  operationId?: string;
  taskId?: string;
  tasks?: TaskDto[];
}

export interface TaskDto {
  id: string;
  operationId?: string;
  type: string;
  serverId: string | null;
  nodeId?: string | null;
  resourceType?: string;
  resourceId?: string;
  triggerType?: string;
  triggerResourceType?: string;
  triggerResourceId?: string;
  triggerTaskId?: string;
  triggeredBy?: string;
  status: TaskStatus;
  stage: TaskStage;
  percentage: number | null;
  summary: string;
  error?: string;
  retryCount: number;
  maxRetries: number;
  nextRunAt?: string | null;
  createdAt: string;
  startedAt: string | null;
  finishedAt: string | null;
}

export interface TaskStepDto {
  id: string;
  taskId: string;
  step: string;
  status: TaskStatus | string;
  percentage?: number | null;
  metadata?: Record<string, unknown>;
  startedAt?: string | null;
  finishedAt?: string | null;
  error?: string | null;
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
