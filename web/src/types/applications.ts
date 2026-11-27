import type { ActivityReceipt } from './activity';
export type DeploymentMode = 'all' | 'selected';
export type FileKind = 'binary' | 'template' | 'archive';

export interface AnyAccessConfig {
  enabled: boolean;
  strategy?: string;
  primaryOriginServerId?: string;
  relayServerIds?: string[];
  originPriority?: string[];
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
  options?: HttpRouteOptions;
}

export interface ReverseProxyRule {
  domain: string;
  targetPort: number;
  originServerIds: string[];
  anyAccess: AnyAccessConfig;
  paths: ReverseProxyPath[];
  /** Per-domain transport switches; omitted means the backend defaults (HTTPS/H2 on, H3 off). */
  httpsEnabled?: boolean;
  http2Enabled?: boolean;
  http3Enabled?: boolean;
  /** Port 80 override for this domain; '' or omitted inherits the facility settings. */
  httpPortMode?: '' | 'serve' | 'redirect';
  /** Per-domain HSTS switch; nil/omitted inherits the facility HSTS setting. */
  hstsEnabled?: boolean;
  /** Per-domain `client_max_body_size`; '' or omitted inherits the facility-wide limit. */
  clientMaxBodySize?: string;
  /** Advanced single-line nginx directives appended to this domain's `server {}` block. */
  customServerDirectives?: string[];
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

export interface DeploymentPlanningError {
  code: string;
  message: string;
  field?: string;
  fileName?: string;
  retryable: boolean;
  operationId: string;
  occurredAt: string;
  configVersion: number;
}

export interface ApplicationDto {
  id: string;
  version: number;
  kind: string;
  name: string;
  enabled: boolean;
  /** Object-level auto-update opt-out (default true). Manual update is never blocked. */
  autoUpdateEnabled?: boolean;
  deletionRequested?: boolean;
  reconcileStopped?: boolean;
  specYaml: string;
  persistentPath?: string;
  persistentServers?: string[];
  hasPersistentData?: boolean;
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
  planningError?: DeploymentPlanningError;
  createdAt: string;
  updatedAt: string;
}

export interface ApplicationSummaryDto {
  planningError?: DeploymentPlanningError;
  id: string;
  name: string;
  enabled: boolean;
  reconcileStopped?: boolean;
  imageReference?: string;
  instanceCount?: number;
  healthyInstanceCount?: number;
  abnormalInstanceCount?: number;
  deployingInstanceCount?: number;
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
  /** Object-level auto-update opt-out (default true in the backend too). */
  autoUpdateEnabled?: boolean;
  specYaml: string;
  deploymentMode: DeploymentMode;
  deploymentServers: string[];
  reverseProxy: ReverseProxyRule[];
}

export interface TemplateVariableDefinition {
  key: string;
  category: string;
  specExpression: string;
  templateExpression: string;
  resourceId?: string;
  resourceName?: string;
}

export interface PanelFileDefinition {
  id: string;
  resourceId: string;
  resourceType: string;
  name: string;
  kind: string;
  source: string;
}

export interface ApplicationTemplateCatalog {
  variables: TemplateVariableDefinition[];
  panelFiles: PanelFileDefinition[];
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

export interface ApplicationEditCommitResult extends ActivityReceipt {
  application: ApplicationDto;
  resourceVersion: ResourceVersion;
  applyRequested: boolean;
  applyNeeded?: boolean;
  applyAction?: 'deploy' | 'stop' | '';
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
  stage?: string;
  image?: string;
  desiredGeneration?: number;
  observedGeneration?: number;
  updatedAt?: string;
  lastError?: string;
  operation?: LifecycleOperation;
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
  operationId?: string;
  executionId?: string;
  applicationId: string;
  type: string;
  status: string;
  taskId?: string;
  generation: number;
  trigger?: string;
  stage?: string;
  errorCode?: string;
  errorClass?: string;
  error?: string;
  errorDetail?: string;
  monitorReasonCode?: string;
  evidenceDeadlineAt?: string;
  graceDeadlineAt?: string;
  attempt?: number;
  nextRunAt?: string;
  targets?: LifecycleTarget[];
  createdAt: string;
  updatedAt: string;
}

export interface ApplicationRuntime {
  planningError?: DeploymentPlanningError;
  applicationId: string;
  runtimeId: string;
  status: string;
  operation?: LifecycleOperation;
  healthyInstanceCount?: number;
  abnormalInstanceCount?: number;
  deployingInstanceCount?: number;
  instances: ApplicationRuntimeInstance[];
  observedAt: string;
}

export interface OperationResult extends ActivityReceipt {
  taskId?: string;
  evalId?: string;
  deploymentId?: string;
  noChange?: boolean;
  application: ApplicationDto;
  runtime?: ApplicationRuntime;
}

export interface LogResult {
  instanceId?: string;
  containerName?: string;
  type?: string;
  logs: string;
}
