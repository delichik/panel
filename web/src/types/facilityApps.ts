import type { AnyAccessConfig, ApplicationEditPreviewResult, ApplicationRuntime, Diagnostic, HttpRouteOptions, LifecycleOperation, PreviewToken, ResourceVersion } from './applications';

export type StaticSourceType = 'host_path' | 'uploaded_file' | 'uploaded_bundle';
export type StaticRuleType = 'static' | 'redirect' | 'proxy_pass';
export interface PanelEntry {
  enabled: boolean;
  serverId?: string;
  domain?: string;
}

export interface FacilityRoutePath {
  path: string;
  ruleType?: StaticRuleType | string;
  rootPath?: string;
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
  id: string;
  version: number;
  deploymentServers: string[];
  panelHostServerId?: string;
  panelEntry: PanelEntry;
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

export interface FacilityDnsSyncState {
  state: 'pending' | 'synced' | 'failed' | 'skipped' | string;
  updatedAt?: string;
  error?: string;
}

export interface ReverseProxySaveInput {
  deploymentServers: string[];
  panelEntry: PanelEntry;
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
  clientDraftKey?: string;
  state: string;
  baseResourceVersion: ResourceVersion;
  draft: ReverseProxySaveInput;
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

export interface FacilityEditCommitResult {
  config: ReverseProxyConfig;
  resourceVersion: ResourceVersion;
  applyRequested: boolean;
  diagnostics?: Diagnostic[];
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

export interface StorageShareReconcileResult {
  taskId: string;
  config: StorageShareConfig;
}

export interface StorageShareStatus {
  servers: StorageServerStatus[];
  partitions: StoragePartitionStatus[];
}