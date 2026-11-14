import type { ActivityReceipt } from './activity';
import type { AnyAccessConfig, ApplicationEditPreviewResult, ApplicationRuntime, Diagnostic, HttpRouteOptions, LifecycleOperation, PreviewToken, ResourceVersion } from './applications';

export type StaticSourceType = 'uploaded_file' | 'uploaded_bundle';
export type StaticRuleType = 'static' | 'redirect' | 'proxy_pass';
export interface FacilityRoutePath {
  path: string;
  ruleType?: StaticRuleType | string;
  sourceType: StaticSourceType | string;
  assetName?: string;
  redirectUrl?: string;
  redirectCode?: number;
  proxyUrl?: string;
  proxySourceMode?: string;
  options?: HttpRouteOptions;
}

export interface FacilityRouteDomain {
  domain: string;
  /** Optimistic-lock version of the owning config record; mirrors ConfigRecord.version. */
  version?: number;
  originServerIds: string[];
  anyAccess: AnyAccessConfig;
  paths: FacilityRoutePath[];
}

export interface StaticAsset {
  name: string;
  kind: string;
  contentMode: 'text' | 'binary';
  filename: string;
  size: number;
  sha256: string;
  createdAt: string;
  updatedAt: string;
}

/** One independently versioned reverse-proxy configuration record. */
export type ConfigRecordKind = 'facility_domain' | 'application_route';

export interface ConfigRecord {
  /** Lowercase domain for facility records; the route domain for application records. */
  recordId: string;
  kind: ConfigRecordKind;
  /** Independent optimistic lock for this record only. */
  version: number;
  domain: FacilityRouteDomain;
  staticAssets?: StaticAsset[];
  applicationId?: string;
  applicationName?: string;
  updatedAt: string;
}

/** Proxy plane: gateway membership and container deployment state (no route content). */
export interface ProxyPlaneState {
  id: string;
  deploymentServers: string[];
  deployments: FacilityDeployment[];
  reconcileStopped?: boolean;
  lastError?: string;
  updatedAt: string;
}

export interface ProxyPlaneSaveInput {
  deploymentServers: string[];
}

export interface RouteSummary {
  domain: string;
  path: string;
  source: string;
  serverIds: string[];
  httpsStatus: string;
  certificateId?: string;
  certificateName?: string;
  matchedDomains?: string[];
  applicationId?: string;
  applicationName?: string;
}

export interface ApplicationRouteSummary {
  applicationId: string;
  applicationName: string;
  deploymentMode: string;
  deploymentServers: string[];
  routes: Array<{ domain: string; targetPort: number; originServerIds: string[]; paths: Array<{ path: string }> }>;
}

export interface ReverseProxyConfig {
	deployments?: FacilityDeployment[];
  id: string;
  version: number;
  deploymentServers: string[];
  domains: FacilityRouteDomain[];
  staticAssets: StaticAsset[];
  routeSummaries: RouteSummary[];
  applicationRoutes: ApplicationRouteSummary[];
  operation?: LifecycleOperation;
  runtime?: ApplicationRuntime;
  reconcileStopped?: boolean;
  lastError?: string;
  updatedAt: string;
  routes: number;
  enabledServers: string[];
  dnsSync?: Record<string, FacilityDnsSyncState>;
}

export interface FacilityDeployment {
  serverId: string;
  serverName: string;
  observedState: string;
  observedAt?: string;
  operation?: LifecycleOperation;
}

export interface ProxyRequestDiagnostic {
  code: string;
  domain?: string;
  upstream?: string;
  count: number;
  lastSeen?: string;
  evidence?: string;
}

export interface ProxyDiagnostics {
  serverId: string;
  checkedAt: string;
  status: 'sampled' | 'unavailable';
  issues: ProxyRequestDiagnostic[];
  truncated: boolean;
}

export interface FacilityDnsSyncState {
  state: 'pending' | 'synced' | 'failed' | 'skipped' | string;
  updatedAt?: string;
  error?: string;
}

export interface ReverseProxySaveInput {
  deploymentServers: string[];
  domains: FacilityRouteDomain[];
}

export interface FacilityEditAsset {
  name: string;
  kind: string;
  contentMode: 'text' | 'binary';
  filename: string;
  size: number;
  sha256: string;
  createdAt: string;
  updatedAt: string;
}

export interface FacilityEditSession {
  id: string;
  /** When set to the record being edited; every draft/asset operation is scoped to it. */
  clientDraftKey?: string;
  recordId: string;
  state: string;
  baseResourceVersion: ResourceVersion;
  draft: FacilityRouteDomain;
  revision: number;
  assets: FacilityEditAsset[];
  previewToken?: PreviewToken;
  idleExpiresAt: string;
  absoluteExpiresAt: string;
  createdAt: string;
  updatedAt: string;
  committedAt?: string;
  commitResult?: FacilityEditCommitResult;
}

export interface FacilityEditValidationResult {
  valid: boolean;
  revision: number;
  diagnostics: Diagnostic[];
}

export type FacilityEditPreviewResult = ApplicationEditPreviewResult;

export interface FacilityEditCommitResult extends ActivityReceipt {
  /** The single record that was just committed. */
  record: ConfigRecord;
  /** Aggregate snapshot; the backend may still return it, but commit only changes `record`. */
  config?: ReverseProxyConfig;
  resourceVersion: ResourceVersion;
  applyRequested: boolean;
  applyNeeded?: boolean;
  applyServerIds?: string[];
  stopServerIds?: string[];
  diagnostics?: Diagnostic[];
}

export interface ReverseProxyReconcileInput {
  serverIds?: string[];
  stopServerIds?: string[];
  apply?: boolean;
}

export interface StorageServerSetting {
  serverId: string;
  root: string;
}

export interface StorageShareConfig {
  id: string;
  version: number;
  servers: StorageServerSetting[];
  enabled: boolean;
  partitions: StorageSharePartition[];
  references?: Array<{ applicationId: string; applicationName: string }>;
  lastError?: string;
  updatedAt: string;
}

export interface StorageSharePartition {
  id: string;
  applicationId: string;
  applicationName: string;
  serverId: string;
  serverName: string;
  storageServerId?: string;
  storageServerName?: string;
  path: string;
  target?: string;
  volumeName?: string;
  createdAt: string;
  updatedAt: string;
}

export interface StorageShareSaveInput {
  servers: StorageServerSetting[];
  version: number;
}

export interface StorageServerStatus {
  serverId: string;
  root: string;
  agentOnline: boolean;
  serverInstalled: boolean;
  rootExists: boolean;
  exportLive: boolean;
  detail?: string;
  lastError?: string;
}

export interface StoragePartitionStatus extends StorageSharePartition {
  volumeExists: boolean;
  mounted: boolean;
  writable: boolean;
  mountDetail?: string;
}

export interface StorageShareReconcileResult extends ActivityReceipt {
  taskId: string;
  config: StorageShareConfig;
}

export interface StorageShareStatus {
  servers: StorageServerStatus[];
  partitions: StoragePartitionStatus[];
}
