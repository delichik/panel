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
  credentialId: string;
  dockerHost: string;
  traits?: Record<string, string>;
  notes?: string;
  os?: OSInfoDto | null;
  sudo?: SudoInfoDto | null;
  reachable: boolean;
  loadAverage: string | null;
  lastCheckedAt: string | null;
  lastError?: string;
  initialTaskId?: string;
  createdAt: string;
  updatedAt: string;
}

export interface AgentCertificateBundleDto {
  ca: string;
  certificate: string;
  privateKey: string;
  listenAddress: string;
  agentUrl: string;
  dockerHost: string;
}

export type FirewallProtocol = 'tcp' | 'udp' | 'any';

export interface UfwRuleDto {
  number: number;
  to: string;
  action: string;
  from: string;
}

export interface UfwStateDto {
  serverId: string;
  supported: boolean;
  installed: boolean;
  active: boolean;
  status: string;
  defaultPolicy: string;
  rules: UfwRuleDto[];
}

export interface UfwAllowInput {
  port: number;
  protocol: FirewallProtocol;
  from?: string;
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

export interface SelfSignedCertificateDto {
  id: string;
  parentCaId?: string;
  kind: 'ca' | 'leaf';
  name: string;
  commonName: string;
  dnsNames: string[];
  ipAddresses: string[];
  fingerprint: string;
  notBefore: string;
  notAfter: string;
  createdAt: string;
  updatedAt: string;
}

export interface SelfSignedCAInput {
  name: string;
  commonName: string;
  years: number;
}

export interface SelfSignedLeafInput {
  name: string;
  caId: string;
  commonName: string;
  dnsNames: string[];
  ipAddresses: string[];
  days: number;
}

export type KeyAssetType = 'ca_certificate' | 'tls_certificate' | 'ssh_key_pair';
export type KeyAssetAlgorithm = 'ed25519' | 'rsa' | string;
export type KeyAssetFileKind = 'certificate' | 'private_key' | 'public_key' | 'ssh_public_key';
export type KeyAssetImportConflictStrategy = 'skip_existing' | 'generate_new_id' | 'overwrite_existing';
export type KeyAssetImportConflictAction = 'skip_existing' | 'generate_new_id' | 'overwrite_existing';
export type KeyAssetImportConflictType = 'id_conflict' | 'name_conflict' | 'missing_parent_ca' | 'overwrite_in_use';

export interface KeyAssetReferenceDto {
  resourceType: string;
  resourceId: string;
  resourceName: string;
  relation: string;
}

export interface KeyAssetSummaryDto {
  id: string;
  type: KeyAssetType;
  name: string;
  parentAssetId?: string | null;
  algorithm?: KeyAssetAlgorithm | null;
  keySize?: number | null;
  commonName?: string | null;
  dnsNames: string[];
  ipAddresses: string[];
  fingerprint: string;
  notBefore?: string | null;
  notAfter?: string | null;
  hasCertificate: boolean;
  hasPrivateKey: boolean;
  hasPublicKey: boolean;
  downloadKinds: KeyAssetFileKind[];
  childCount: number;
  referenceCount: number;
  references: KeyAssetReferenceDto[];
  canReissue: boolean;
  canRegenerate: boolean;
  canDelete: boolean;
  createdAt: string;
  updatedAt: string;
}

export interface KeyAssetDetailDto extends KeyAssetSummaryDto {
  metadata?: Record<string, unknown>;
}

export interface KeyAssetCaGenerateInput {
  name: string;
  commonName: string;
  validityDays: number;
  algorithm?: KeyAssetAlgorithm | null;
  keySize?: number | null;
}

export interface KeyAssetTlsGenerateInput {
  name: string;
  caId: string;
  commonName: string;
  dnsNames: string[];
  ipAddresses: string[];
  validityDays: number;
  algorithm?: KeyAssetAlgorithm | null;
  keySize?: number | null;
}

export interface KeyAssetSshGenerateInput {
  name: string;
  algorithm: 'ed25519' | 'rsa';
  keySize?: number | null;
}

export interface KeyAssetCaImportInput {
  type: 'ca_certificate';
  name: string;
  certificatePem: string;
  privateKeyPem: string;
  publicKeyPem?: string;
}

export interface KeyAssetTlsImportInput {
  type: 'tls_certificate';
  name: string;
  parentAssetId?: string | null;
  certificatePem: string;
  privateKeyPem: string;
  publicKeyPem?: string;
}

export interface KeyAssetSshImportInput {
  type: 'ssh_key_pair';
  name: string;
  privateKeyPem: string;
  publicKey?: string;
}

export type KeyAssetImportInput =
  | KeyAssetCaImportInput
  | KeyAssetTlsImportInput
  | KeyAssetSshImportInput;

export interface KeyAssetMutationDto {
  asset?: KeyAssetSummaryDto;
  taskId?: string;
  operationId?: string;
}

export interface KeyAssetExportInput {
  assetIds: string[];
  password: string;
}

export interface KeyAssetExportDto {
  taskId: string;
  operationId?: string;
}

export interface KeyAssetImportPlanAssetDto {
  assetId: string;
  type: KeyAssetType;
  name: string;
  parentAssetId?: string | null;
  algorithm?: KeyAssetAlgorithm | null;
  keySize?: number | null;
  commonName?: string | null;
  fingerprint?: string | null;
  standalone: boolean;
  conflictTypes: KeyAssetImportConflictType[];
}

export interface KeyAssetImportConflictCandidateDto {
  assetId: string;
  name: string;
  type: KeyAssetType;
}

export interface KeyAssetImportConflictDto {
  assetId: string;
  assetName: string;
  assetType: KeyAssetType;
  conflictType: KeyAssetImportConflictType;
  existingAssetId?: string;
  existingAssetName?: string;
  missingParentAssetId?: string;
  overwriteCandidates?: KeyAssetImportConflictCandidateDto[];
  affectedReferences?: KeyAssetReferenceDto[];
}

export interface KeyAssetImportPlanSummaryDto {
  totalAssets: number;
  caCount: number;
  tlsCount: number;
  sshCount: number;
  standaloneTlsCount: number;
  conflictCount: number;
}

export interface KeyAssetImportPreflightDto {
  planId: string;
  expiresAt: string;
  summary: KeyAssetImportPlanSummaryDto;
  assets: KeyAssetImportPlanAssetDto[];
  conflicts: KeyAssetImportConflictDto[];
  requiresDangerConfirm: boolean;
}

export interface KeyAssetImportConflictResolutionDto {
  assetId: string;
  action: KeyAssetImportConflictAction;
  targetAssetId?: string;
}

export interface KeyAssetImportExecuteInput {
  strategy: KeyAssetImportConflictStrategy;
  confirmDangerousOverwrite: boolean;
  resolutions: KeyAssetImportConflictResolutionDto[];
}

export interface KeyAssetImportExecuteDto {
  taskId: string;
  operationId?: string;
}

export interface DnsDomainDto {
  id: string;
  name: string;
  provider: 'cloudflare';
  createdAt: string;
  updatedAt: string;
}

export interface DnsDomainInput {
  name: string;
  provider: 'cloudflare';
  apiToken?: string;
}

export type DnsRecordType = 'A' | 'AAAA' | 'CNAME' | 'TXT' | 'MX' | 'SRV' | 'CAA' | 'NS';

export interface DnsRecordDto {
  id: string;
  name: string;
  type: DnsRecordType | string;
  value: string;
  ttl?: number;
  proxied?: boolean;
}

export interface DnsRecordInput {
  name: string;
  type: DnsRecordType;
  value: string;
  ttl?: number;
  proxied?: boolean;
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
export type OverviewCardKind = 'cpu' | 'memory' | 'disk' | 'network' | 'packageUpdates' | 'containerUpdates' | 'placeholder';
export type OverviewCardNetworkDirection = 'rx' | 'tx' | 'both';

export interface OverviewCardDto {
  id: string;
  kind: OverviewCardKind;
  width: number;
  height: number;
  range: MetricsRange;
  networkDirection: OverviewCardNetworkDirection;
  serverIds: string[];
}

export interface OverviewCardConfigurationDto {
  cards: OverviewCardDto[];
}

export interface OverviewCardDataDto {
  card: OverviewCardDto;
  metricsByServer: Record<string, MetricsSeriesDto>;
}

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
  taskId?: string;
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

export interface ApplicationTemplateVariableDto {
  key: string;
  category: string;
  specExpression: string;
  templateExpression: string;
}

export interface ApplicationPanelFileDto {
  id: string;
  resourceId: string;
  resourceType: string;
  name: string;
  kind: string;
  source: string;
}

export interface ApplicationTemplateCatalogDto {
  variables: ApplicationTemplateVariableDto[];
  panelFiles: ApplicationPanelFileDto[];
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

export interface RuntimePortDto {
  label?: string;
  containerPort: number;
  hostPort?: number;
  protocol?: string;
}

export interface RuntimeResourcesDto {
  cpu?: number;
  memoryMb?: number;
}

export interface RuntimeMountDto {
  type: string;
  source: string;
  target: string;
  readOnly?: boolean;
}

export interface RuntimeManagedFileDto {
  path: string;
  content: string;
  mode?: string;
}

export interface RuntimeRestartDto {
  policy: string;
  attempts?: number;
  intervalSeconds?: number;
  delaySeconds?: number;
  mode?: string;
}

export interface RuntimeServiceDto {
  name: string;
  port: string;
  tags?: string[];
}

export interface RuntimeCheckDto {
  name: string;
  type: string;
  port?: string;
  path?: string;
  intervalSeconds?: number;
  timeoutSeconds?: number;
  command?: string;
}

export interface ApplicationRuntimeSpecDto {
  id: string;
  applicationId: string;
  instanceId?: string;
  containerName?: string;
  name: string;
  image: string;
  command?: string[];
  args?: string[];
  env?: Record<string, string>;
  ports?: RuntimePortDto[];
  networkMode: string;
  resources: RuntimeResourcesDto;
  privileged?: boolean;
  mounts?: RuntimeMountDto[];
  files?: RuntimeManagedFileDto[];
  restart: RuntimeRestartDto;
  services?: RuntimeServiceDto[];
  checks?: RuntimeCheckDto[];
  generation: number;
  specHash: string;
}

export interface ApplicationRuntimePlanDto {
  instanceCount: number;
  targetServers: string[];
  warnings?: string[];
}

export interface ApplicationPlanDto {
  application: ApplicationDto;
  spec: ApplicationRuntimeSpecDto;
  plan: ApplicationRuntimePlanDto;
}

export interface ApplicationOperationDto {
  taskId?: string;
  evalId?: string;
  deploymentId?: string;
  application?: ApplicationDto;
  runtime?: ApplicationRuntimeDto;
}

export interface ApplicationRuntimeInstanceDto {
  instanceId: string;
  serverId: string;
  containerName: string;
  containerId?: string;
  status: string;
  desiredState: string;
  image?: string;
  startedAt?: string;
  finishedAt?: string;
  exitCode?: number;
  lastError?: string;
  observedAt: string;
}

export interface ApplicationRuntimeDto {
  applicationId: string;
  runtimeId: string;
  status: string;
  instances: ApplicationRuntimeInstanceDto[];
  observedAt: string;
}

export interface ApplicationLogsDto {
  instanceId: string;
  containerName: string;
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
  logLevel: LogLevel;
  remoteCommandTimeoutSeconds: number;
  branding: RuntimeBrandingSettingsDto;
  certificates: RuntimeCertificateSettingsDto;
  jwtSecretConfigured: boolean;
}

export interface SystemVersionDto {
  version: string;
  channel: 'release' | 'dev';
  commit?: string;
  repository?: string;
  latestVersion?: string;
  updateAvailable: boolean;
  checkedAt?: string;
}

export type TokenExpiration = '10m' | '1h' | '1d' | '5d' | '30d' | 'never';
export type LogLevel = 'debug' | 'info' | 'warn' | 'error';

export interface RuntimeCertificateSettingsDto {
  email: string;
  dnsPropagationDelaySeconds: number;
}

export interface RuntimeBrandingSettingsDto {
  loginTitle: string;
  loginSubtitle: string;
}

export interface RuntimeSettingsUpdate {
  metricsRetentionDays: number;
  metricsCollectionIntervalSeconds: number;
  cleanupSchedule: string;
  tokenExpiration: TokenExpiration;
  language: string;
  logLevel: LogLevel;
  remoteCommandTimeoutSeconds: number;
  branding: RuntimeBrandingSettingsDto;
  certificates: RuntimeCertificateSettingsDto;
}
