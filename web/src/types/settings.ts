export interface RuntimeCertificateSettings {
  email: string;
  dnsPropagationDelaySeconds: number;
}

export interface RuntimeBrandingSettings {
  loginTitle: string;
  loginSubtitle: string;
}

export interface RuntimePanelSettings {
  domain: string;
  tlsCertificateId: string;
}

export interface RuntimeAgentSettings {
  downloadBaseUrl: string;
  downloadVerifyTls: boolean;
  transferTimeoutSeconds: number;
}

/** Scheme used by Panel when it resolves image digests against a registry host. */
export type ImageRegistryScheme = 'https' | 'http';

/**
 * Runtime settings for the "Image registry" partition (SET-RUN-024).
 * `endpoint` is a bare `host[:port]`; an empty value means the official registry.
 */
export interface RuntimeImageRegistrySettings {
  endpoint: string;
  scheme: ImageRegistryScheme;
}

/**
 * PUT /settings/runtime/image-registry：指针语义，省略字段保留当前值，
 * 显式空串 `endpoint: ""` 表示恢复官方仓库。
 */
export interface RuntimeImageRegistryUpdate {
  endpoint?: string;
  scheme?: ImageRegistryScheme;
}

/** Packaging scope for software package updates. */
export type AutoUpdatePackagesMode = 'security' | 'all';

/**
 * Runtime settings for the "Automatic updates" partition (SET-RUN-023).
 * `appsEnabled` covers both user application images and the entrance-proxy nginx
 * image; the read-only `lastRun*`/`lastAutoRunAt` fields are written by the
 * coordinator task and must never be submitted back in the PUT payload.
 */
export interface RuntimeAutoUpdateSettings {
  appsEnabled: boolean;
  packagesEnabled: boolean;
  packagesMode: AutoUpdatePackagesMode;
  /** Daily execution time in the Panel local time zone (`HH:MM`, 24h). */
  scheduleTime: string;
  /** Start time of the most recent run; may be a manual "Run now" run. */
  lastRunAt?: string;
  lastRunStatus?: string;
  lastRunTaskId?: string;
  /** Human-readable summary written when the most recent run finishes. */
  lastRunSummary?: string;
  /** Start time of the most recent *scheduled* automatic run (never manual). */
  lastAutoRunAt?: string;
}

/** PUT /settings/runtime/auto-update：只提交本分区四个可编辑字段。 */
export type RuntimeAutoUpdateUpdate = Pick<RuntimeAutoUpdateSettings, 'appsEnabled' | 'packagesEnabled' | 'packagesMode' | 'scheduleTime'>;

/**
 * POST /settings/runtime/auto-update/run 的 202 响应。
 * 后端在已有活跃运行任务时幂等返回该任务 id；没有 id 时 UI 只提示已受理。
 */
export interface RuntimeAutoUpdateRunResponse {
  taskId?: string;
}

/** PUT /settings/runtime/general：运行时/常规分区。 */
export interface RuntimeGeneralUpdate {
  containerReportIntervalSeconds: number;
  language: string;
  logLevel: string;
  reconcileTraceEnabled: boolean;
}

/** PUT /settings/runtime/security：安全分区。 */
export interface RuntimeSSHAccessSettings {
  successRetentionDays: number;
  failureRetentionDays: number;
}

export interface RuntimeSecurityUpdate {
  tokenExpiration: string;
  remoteCommandTimeoutSeconds: number;
  /** Optional so older payloads/tests stay valid. */
  sshAccess?: RuntimeSSHAccessSettings;
}

/** PUT /settings/runtime/certificates：证书/Panel HTTPS 分区。 */
export interface RuntimeCertificatesUpdate {
  panel: RuntimePanelSettings;
  certificates: RuntimeCertificateSettings;
}

/** PUT /settings/runtime/agent：Agent 投递分区。 */
export type RuntimeAgentUpdate = RuntimeAgentSettings;

/** PUT /settings/runtime/branding：登录品牌分区。 */
export type RuntimeBrandingUpdate = RuntimeBrandingSettings;

export interface RuntimeSettings {
  listenAddress: string;
  appDatabase: string;
  metricsDatabase: string;
  dataRoot: string;
  containerReportIntervalSeconds: number;
  tokenExpiration: string;
  language: string;
  logLevel: string;
  remoteCommandTimeoutSeconds: number;
  /** SSH access retention; optional so older payloads/tests stay valid. */
  sshAccess?: RuntimeSSHAccessSettings;
  reconcileTraceEnabled: boolean;
  branding: RuntimeBrandingSettings;
  certificates: RuntimeCertificateSettings;
  panel: RuntimePanelSettings;
  agent: RuntimeAgentSettings;
  /** Automatic updates partition; optional so older payloads/tests stay valid. */
  autoUpdate?: RuntimeAutoUpdateSettings;
  /** Image registry partition; optional so older payloads/tests stay valid. */
  imageRegistry?: RuntimeImageRegistrySettings;
  jwtSecretConfigured: boolean;
}

export interface ServerVariableDefinition {
  name: string;
  key: string;
  required: boolean;
}

export interface BackupExportResponse {
  exportId: string;
  restartSupported: boolean;
}

export interface RestorePreflightResponse {
  manifest: BackupManifest;
  encrypted: boolean;
  passwordRequired: boolean;
}

export interface RestoreConfirmResponse {
  pending: boolean;
  restartSupported: boolean;
}

export interface BackupManifest {
  formatVersion: number;
  panelVersion: string;
  createdAt: string;
  encrypted: boolean;
  includes: string[];
  files: Array<{ path: string; size: number; sha256: string }>;
  metadata?: Record<string, string>;
}
