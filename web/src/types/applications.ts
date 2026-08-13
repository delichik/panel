export type DeploymentMode = 'all' | 'selected';
export type FileKind = 'binary' | 'template' | 'archive';
export type ReverseProxyTargetType = 'local' | 'container';

export interface AnyAccessConfig {
  enabled: boolean;
  strategy?: string;
  primaryOriginServerId?: string;
  relayServerIds?: string[];
}

export interface HttpHeader {
  name: string;
  value: string;
}

export interface HttpRouteOptions {
  gzipMode?: string;
  clientMaxBodySizeMb?: number;
  connectTimeoutSeconds?: number;
  readTimeoutSeconds?: number;
  sendTimeoutSeconds?: number;
  bufferingMode?: string;
  webSocketMode?: string;
  requestHeaders?: HttpHeader[];
  responseHeaders?: HttpHeader[];
}

export interface ReverseProxyPath {
  path: string;
  webSocket?: boolean;
  options?: HttpRouteOptions;
}

export interface ReverseProxyRule {
  domain: string;
  targetType?: ReverseProxyTargetType;
  targetPort: number;
  originServerIds: string[];
  anyAccess: AnyAccessConfig;
  paths: ReverseProxyPath[];
}

export interface ImageUpdateTarget {
  serverId: string;
  serverName?: string;
  reference: string;
  localDigest?: string;
  latestDigest?: string;
  updateAvailable: boolean;
  checkedAt?: string;
  lastError?: string;
}

export interface ApplicationDto {
  id: string;
  version: number;
  kind: string;
  name: string;
  enabled: boolean;
  deletionRequested?: boolean;
  reconcileStopped?: boolean;
  specYaml: string;
  persistentPath?: string;
  deploymentMode: DeploymentMode | string;
  deploymentServers: string[];
  reverseProxy: ReverseProxyRule[];
  generation: number;
  specHash: string;
  imageReference?: string;
  imageDigest?: string;
  imageLatestDigest?: string;
  imageCheckedAt?: string;
  imageUpdateAvailable: boolean;
  imageUpdateTargets?: ImageUpdateTarget[];
  imageLastError?: string;
  jobId: string;
  namespace: string;
  lastEvalId?: string;
  lastDeploymentId?: string;
  lastError?: string;
  runtimeStatus?: string;
  createdAt: string;
  updatedAt: string;
}

export interface ApplicationSummaryDto {
  id: string;
  name: string;
  enabled: boolean;
  reconcileStopped?: boolean;
  imageReference?: string;
  instanceCount?: number;
  jobId: string;
  namespace: string;
  runtimeStatus?: string;
  imageUpdateAvailable: boolean;
  lastError?: string;
  updatedAt: string;
}

export interface ApplicationSaveInput {
  name: string;
  enabled: boolean;
  specYaml: string;
  deploymentMode: DeploymentMode;
  deploymentServers: string[];
  reverseProxy: ReverseProxyRule[];
}

export interface ApplicationFile {
  applicationId?: string;
  name: string;
  kind: FileKind | string;
  contentType: string;
  size: number;
  sha256: string;
  contentBase64?: string;
  createdAt: string;
  updatedAt: string;
}

export interface ApplicationEditSessionFileContent {
	name: string;
  kind: FileKind | string;
  contentType: string;
  size: number;
  sha256: string;
  contentBase64: string;
}

export interface ResourceVersion {
  value: string;
  updatedAt: string;
}

export interface Diagnostic {
  code: string;
  severity: 'info' | 'warning' | 'error' | string;
  field?: string;
  message: string;
  details?: Record<string, unknown>;
}

export interface PreviewToken {
  value: string;
  action: string;
  subjectVersion: string;
}

export interface ApplicationEditSession {
  id: string;
  applicationId?: string;
  clientDraftKey?: string;
  state: string;
  baseResourceVersion: ResourceVersion;
  draft: ApplicationSaveInput;
  revision: number;
  files: ApplicationFile[];
  previewToken?: PreviewToken;
  idleExpiresAt: string;
  absoluteExpiresAt: string;
  createdAt: string;
  updatedAt: string;
  committedAt?: string;
  commitResult?: ApplicationEditCommitResult;
}

export interface ApplicationEditValidationResult {
  valid: boolean;
  revision: number;
  diagnostics: Diagnostic[];
}

export interface ApplicationEditPreviewResult {
  revision: number;
  diagnostics: Diagnostic[];
  token: PreviewToken;
  expiresAt: string;
}

export interface ApplicationEditCommitResult {
  application: ApplicationDto;
  resourceVersion: ResourceVersion;
  applyRequested: boolean;
  diagnostics?: Diagnostic[];
}

export interface ApplicationRuntimeInstance {
  id?: string;
  instanceId?: string;
  containerName?: string;
  containerId?: string;
  serverId?: string;
  serverName?: string;
  state?: string;
  status?: string;
  image?: string;
  desiredGeneration?: number;
  observedGeneration?: number;
  updatedAt?: string;
  error?: string;
}

export interface LifecycleTarget {
  id: string;
  serverId: string;
  serverName?: string;
  action?: string;
  state?: string;
  status: string;
  desiredState: string;
  stage?: string;
  error?: string;
  updatedAt: string;
}

export interface LifecycleOperation {
  id: string;
  applicationId: string;
  type: string;
  status: string;
  taskId?: string;
  generation: number;
  trigger?: string;
  error?: string;
  targets?: LifecycleTarget[];
  createdAt: string;
  updatedAt: string;
}

export interface ApplicationRuntime {
  applicationId: string;
  runtimeId: string;
  status: string;
  operation?: LifecycleOperation;
  instances: ApplicationRuntimeInstance[];
  observedAt: string;
}

export interface OperationResult {
  taskId?: string;
  evalId?: string;
  deploymentId?: string;
  application: ApplicationDto;
  runtime?: ApplicationRuntime;
}

export interface LogResult {
  instanceId?: string;
  containerName?: string;
  type?: string;
  logs: string;
}
