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

export type CertificateScope = 'single' | 'wildcard';

export interface CertificateDto {
  id: string;
  name: string;
  domainId: string;
  domain: string;
  prefix: string;
  scope: CertificateScope;
  domains: string[];
  variableName: string;
  certificatePath: string;
  privateKeyPath: string;
  issuer: string;
  status: 'pending' | 'issuing' | 'issued' | 'failed' | string;
  lastError?: string;
  autoRenew: boolean;
  nextRenewAt?: string;
  notBefore?: string;
  notAfter?: string;
  createdAt: string;
  updatedAt: string;
}

export interface CertificateIssueInput {
  name: string;
  domainId: string;
  prefix: string;
  scope: CertificateScope;
  variableName: string;
}

export interface CertificateIssueDto {
  certificate: CertificateDto;
  taskId?: string;
}

export interface DnsDomainDto {
  id: string;
  name: string;
  provider: 'cloudflare';
  accountId?: string;
  createdAt: string;
  updatedAt: string;
}

export interface DnsDomainInput {
  name: string;
  provider: 'cloudflare';
  apiToken?: string;
  accountId?: string;
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

export type MetricsRange = '1h' | '6h' | '1d' | '7d';

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

export interface ApplicationDto {
  id: string;
  name: string;
  enabled: boolean;
  specYaml: string;
  variables: Record<string, string>;
  resolvedVariables?: Record<string, unknown>;
  persistentPath?: string;
  deploymentMode?: 'all' | 'selected' | string;
  deploymentServers?: string[];
  reverseProxy?: ApplicationReverseProxyRuleDto[];
  generation: number;
  specHash: string;
  imageReference?: string;
  imageDigest?: string;
  imageLatestDigest?: string;
  imageCheckedAt?: string;
  imageUpdateAvailable?: boolean;
  imageLastError?: string;
  jobId: string;
  namespace: string;
  lastEvalId?: string;
  lastDeploymentId?: string;
  lastError?: string;
  runtimeStatus?: string;
  allocationCount?: number;
  createdAt: string;
  updatedAt: string;
}

export interface ApplicationSaveDto {
  name: string;
  enabled: boolean;
  specYaml: string;
  variables: Record<string, string>;
  persistentPath?: string;
  deploymentMode?: 'all' | 'selected' | string;
  deploymentServers?: string[];
  reverseProxy?: ApplicationReverseProxyRuleDto[];
}

export interface ApplicationReverseProxyRuleDto {
  domain: string;
  targetPort: number;
  paths: ApplicationReverseProxyPathDto[];
}

export interface ApplicationReverseProxyPathDto {
  path: string;
  webSocket: boolean;
}

export type ApplicationFileKind = 'binary' | 'template';

export interface ApplicationFileDto {
  id: string;
  applicationId: string;
  path: string;
  kind: ApplicationFileKind;
  contentType: string;
  size: number;
  sha256: string;
  createdAt: string;
  updatedAt: string;
}

export interface ApplicationFileSaveDto {
  path: string;
  kind: ApplicationFileKind;
  contentType?: string;
  contentBase64: string;
}

export interface ApplicationFileDeleteDto {
  path: string;
}

export interface ApplicationSaveSessionBeginDto {
  applicationId?: string;
  save: ApplicationSaveDto;
}

export interface ApplicationSaveSessionDto {
  id: string;
  applicationId?: string;
  expiresAt: string;
  files: ApplicationFileDto[];
}

export interface ApplicationValidationIssueDto {
  field?: string;
  path?: string;
  severity?: 'error' | 'warning' | string;
  message: string;
}

export interface ApplicationValidationDto {
  valid: boolean;
  issues: ApplicationValidationIssueDto[];
}

export interface NomadPortMappingDto {
  Label?: string;
  Value?: number;
  To?: number;
}

export interface NomadNetworkDto {
  Mode?: string;
  ReservedPorts?: NomadPortMappingDto[];
  DynamicPorts?: NomadPortMappingDto[];
}

export interface NomadCheckDto {
  Name?: string;
  Type?: string;
  Path?: string;
  PortLabel?: string;
  Interval?: number;
  Timeout?: number;
}

export interface NomadServiceDto {
  Name?: string;
  PortLabel?: string;
  Tags?: string[];
  Checks?: NomadCheckDto[];
}

export interface NomadTaskDto {
  Name?: string;
  Driver?: string;
  Config?: Record<string, unknown>;
  Env?: Record<string, string>;
  Resources?: { CPU?: number; MemoryMB?: number };
  Services?: NomadServiceDto[];
  Templates?: Array<{ EmbeddedTmpl?: string; DestPath?: string; Perms?: string; ChangeMode?: string }>;
  Lifecycle?: { Hook?: string; Sidecar?: boolean };
}

export interface NomadTaskGroupDto {
  Name?: string;
  Count?: number;
  Networks?: NomadNetworkDto[];
  Tasks?: NomadTaskDto[];
  Services?: NomadServiceDto[];
}

export interface NomadJobDto {
  ID?: string;
  Name?: string;
  Type?: string;
  Status?: string;
  Region?: string;
  Namespace?: string;
  Datacenters?: string[];
  Meta?: Record<string, string>;
  TaskGroups?: NomadTaskGroupDto[];
}

export interface NomadStatusDto {
  connected: boolean;
  leader?: string;
}

export type NomadControlPlaneStatus = 'unconfigured' | 'bootstrapping' | 'connected' | 'degraded';
export type ProjectedNomadNodeKind = 'managed' | 'missing' | 'pending' | 'unmanaged';
export type ProjectedNomadNodeRole = 'server' | 'client' | 'unknown';
export type ProjectedNomadNodeStatus = 'bootstrapping' | 'joining' | 'registering' | 'removing' | 'ready' | 'down' | 'failed' | 'missing' | 'nomad_unreachable' | 'unmanaged' | string;

export interface ProjectedNomadNodeDto {
  kind: ProjectedNomadNodeKind;
  serverId?: string;
  nodeId?: string;
  name: string;
  host?: string;
  role: ProjectedNomadNodeRole;
  status: ProjectedNomadNodeStatus;
  reverseProxy: boolean;
  reverseProxyStatic: boolean;
  reverseProxyStaticSites: NomadReverseProxyStaticSiteDto[];
  joinEligible?: boolean;
  taskId?: string;
  error?: string;
}

export interface NomadReverseProxyRouteDto {
  domain: string;
  targetPort: number;
  paths: NomadReverseProxyPathDto[];
}

export interface NomadReverseProxyPathDto {
  path: string;
  webSocket: boolean;
}

export interface NomadReverseProxyStaticSiteDto {
  domain: string;
  root: string;
  index: string;
}

export interface NomadControlPlaneDto {
  status: NomadControlPlaneStatus;
  leader?: string;
  nodes: ProjectedNomadNodeDto[];
  joinCandidates: ServerDto[];
  bootstrapCandidates: ServerDto[];
}

export interface NomadNodeDto {
  ID?: string;
  Name?: string;
  Address?: string;
  Datacenter?: string;
  Status?: string;
  SchedulingEligibility?: string;
  Eligibility?: string;
  Meta?: Record<string, string>;
}

export interface NomadEvaluationDto {
  ID?: string;
  Namespace?: string;
  JobID?: string;
  Status?: string;
  Type?: string;
  TriggeredBy?: string;
  StatusDescription?: string;
  FailedTGAllocs?: Record<string, NomadFailedTGAllocDto>;
}

export interface NomadFailedTGAllocDto {
  NodesEvaluated?: number;
  NodesFiltered?: number;
  NodesExhausted?: number;
  ClassFiltered?: Record<string, number>;
  ConstraintFiltered?: Record<string, number>;
  DimensionExhausted?: Record<string, number>;
  QuotaExhausted?: string[];
  ResourcesExhausted?: Record<string, unknown>;
  CoalescedFailures?: number;
}

export interface NomadDeploymentDto {
  ID?: string;
  JobID?: string;
  Namespace?: string;
  Status?: string;
  StatusDescription?: string;
}

export interface NomadAllocationDto {
  ID?: string;
  EvalID?: string;
  Name?: string;
  NodeID?: string;
  JobID?: string;
  TaskGroup?: string;
  ClientStatus?: string;
  DesiredStatus?: string;
  TaskStates?: Record<string, unknown>;
  AllocatedResources?: unknown;
  ModifyIndex?: number;
  CreateIndex?: number;
}

export interface NomadServiceRegistrationDto {
  ID?: string;
  ServiceName?: string;
  Namespace?: string;
  NodeID?: string;
  Datacenter?: string;
  JobID?: string;
  AllocID?: string;
  Tags?: string[];
  Port?: number;
}

export interface ApplicationPlanDto {
  application: ApplicationDto;
  job: NomadJobDto;
  plan: Record<string, unknown>;
}

export interface ApplicationOperationDto {
  taskId?: string;
  evalId?: string;
  deploymentId?: string;
  application?: ApplicationDto;
  runtime?: ApplicationRuntimeDto;
}

export interface ApplicationRuntimeDto {
  applicationId: string;
  jobId: string;
  jobStatus: string;
  deployment?: NomadDeploymentDto;
  evaluations: NomadEvaluationDto[];
  evaluationDetails?: NomadEvaluationDto[];
  allocations: NomadAllocationDto[];
  services?: NomadServiceRegistrationDto[];
  observedAt: string;
}

export interface ApplicationLogsDto {
  allocId: string;
  task: string;
  type: string;
  logs: string;
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
  tokenExpiration: TokenExpiration;
  language: string;
}

export type TokenExpiration = '10m' | '1h' | '1d' | '5d' | '30d' | 'never';

export interface RuntimeSettingsUpdate {
  metricsRetentionDays: number;
  metricsCollectionIntervalSeconds: number;
  cleanupSchedule: string;
  tokenExpiration: TokenExpiration;
  language: string;
}
