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
  /** Effective HTTPS after the per-domain switch and certificate availability are combined. */
  effectiveHTTPS?: boolean;
  httpsEnabled?: boolean;
  http2Enabled?: boolean;
  http3Enabled?: boolean;
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

export type ProxyHttpPortMode = 'serve' | 'redirect';
export type ProxyServerTokens = 'on' | 'off';
export type ProxyTlsMinVersion = '1.2' | '1.3';

export interface ProxyHstsSettings {
  enabled: boolean;
  maxAgeSeconds: number;
  includeSubDomains: boolean;
}

/**
 * Global entrance-proxy parameters stored in `facility_app_configs.settings_json`.
 * `version` is optional: the backend accepts it as a CAS token on save and may
 * expose it in the aggregate config, but the documented GET shape omits it.
 */
export interface FacilitySettings {
  version?: number;
  /** 80-port behavior for domains whose HTTPS switch is on and a certificate exists. */
  httpPortMode: ProxyHttpPortMode;
  hsts: ProxyHstsSettings;
  /** Object-level entrance-proxy auto-update opt-out (default true). */
  imageAutoUpdateEnabled: boolean;
  /** Seconds to wait between adjacent gateways during a rolling image update (0 = no wait). */
  imageUpdateIntervalSeconds: number;
  /** Advanced single-line nginx directives for the main `http {}` block. */
  customHttpDirectives: string[];
  /** P1 safety parameter: nginx `server_tokens` (default off). */
  serverTokens?: ProxyServerTokens;
  /** P1 safety parameter: nginx `client_max_body_size` (default `50m`). */
  clientMaxBodySize?: string;
  /** P1 safety parameter: minimum TLS protocol for generated servers (default `1.2`). */
  tlsMinVersion?: ProxyTlsMinVersion;
}

export interface FacilitySettingsSaveInput {
  /** Optional compare-and-swap token; omitted when the loaded settings do not expose one. */
  version?: number;
  httpPortMode: ProxyHttpPortMode;
  hsts: ProxyHstsSettings;
  imageAutoUpdateEnabled: boolean;
  /** Seconds to wait between adjacent gateways during a rolling image update (0 = no wait). */
  imageUpdateIntervalSeconds: number;
  customHttpDirectives: string[];
  /** P1 safety parameters; always submitted with the settings partition. */
  serverTokens: ProxyServerTokens;
  clientMaxBodySize: string;
  tlsMinVersion: ProxyTlsMinVersion;
}

export type ProxyImageStatus = 'up_to_date' | 'update_available' | 'updating' | 'failed' | 'pinned' | 'unknown' | string;

export interface ProxyImageServerState {
  serverId: string;
  serverName?: string;
  reference?: string;
  targetDigest?: string;
  observedDigest?: string;
  previousDigest?: string;
  status: ProxyImageStatus;
  pinned?: boolean;
  /** Actual running nginx version reported by Agent (e.g. `1.28.3`); empty when uncollected. */
  imageVersion?: string;
  /** P1 failure backoff: consecutive failures for the same target digest. */
  autoUpdateFailures?: number;
  /** P1 failure backoff: target digest that kept failing. */
  failedDigest?: string;
  /** Backend projection: this gateway is currently skipped by automatic updates. */
  autoUpdatePaused?: boolean;
  /** Backend projection: human-readable pause reason (contains failure count/digest). */
  autoUpdateSkipReason?: string;
  lastError?: string;
  checkedAt?: string | null;
  updatedAt?: string | null;
}

/** Projected entrance-proxy nginx image state (per gateway plus the aggregate). */
export interface ProxyImageState {
  reference: string;
  targetDigest?: string;
  observedDigest?: string;
  previousDigest?: string;
  status: ProxyImageStatus;
  updateAvailable?: boolean;
  pinned?: boolean;
  /** Actual running nginx version reported by Agent (aggregate/first non-empty). */
  imageVersion?: string;
  /** P1 failure backoff: consecutive failures for the same target digest. */
  autoUpdateFailures?: number;
  /** P1 failure backoff: target digest that kept failing. */
  failedDigest?: string;
  /** Backend projection: at least one gateway is paused after repeated failures. */
  autoUpdatePaused?: boolean;
  /** Backend projection: pause reason for the aggregate (when exposed). */
  autoUpdateSkipReason?: string;
  checkedAt?: string | null;
  updatedAt?: string | null;
  lastError?: string;
  servers: ProxyImageServerState[];
}

/**
 * Response for the manual image check/update/rollback routes. 202 carries a
 * task id; an idempotent 200 rollback reports `skipped` instead.
 */
export interface ProxyImageActionResponse {
  taskId?: string;
  skipped?: boolean;
  pinned?: boolean;
  status?: string;
}

export interface ProxyImageUpdateInput {
  serverIds: string[];
  /** The backend rejects the request unless this is explicitly true. */
  confirm: true;
}

/** POST .../image/rollback：digest 缺省时由后端取该节点的 previous_digest。 */
export interface ProxyImageRollbackInput {
  serverIds?: string[];
  digest?: string;
  /** The backend rejects the request unless this is explicitly true. */
  confirm: true;
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
  /** Global entrance parameters (80 policy, HSTS, custom http directives, image auto-update). */
  settings?: FacilitySettings;
  /** Entrance-proxy nginx image status projection. */
  image?: ProxyImageState;
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
