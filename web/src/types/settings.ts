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

/** 面板容器内 tailscaled 的观测状态；`lastError` 是后端给出的稳定英文文案。 */
export interface RuntimeTailscaleContainerState {
  available: boolean;
  running: boolean;
  loggedIn: boolean;
  hostname: string;
  ipv4: string;
  ipv6: string;
  version: string;
  backendState: string;
  lastError: string;
  updatedAt: string;
}

export interface RuntimeTailscaleSettings {
  /** 密钥只写不读：响应只暴露“是否已配置”，永不返回密钥本身。 */
  authKeyConfigured: boolean;
  tags: string[];
  container: RuntimeTailscaleContainerState;
}

/** PUT /settings/tailscale 的请求体：字段缺省表示保持当前值。 */
export interface RuntimeTailscaleUpdate {
  authKey?: string;
  clearAuthKey?: boolean;
  tags?: string[];
}

/** PUT /settings/runtime/general：运行时/常规分区。 */
export interface RuntimeGeneralUpdate {
  containerReportIntervalSeconds: number;
  language: string;
  logLevel: string;
  reconcileTraceEnabled: boolean;
}

/** PUT /settings/runtime/security：安全分区。 */
export interface RuntimeSecurityUpdate {
  tokenExpiration: string;
  remoteCommandTimeoutSeconds: number;
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
  reconcileTraceEnabled: boolean;
  branding: RuntimeBrandingSettings;
  certificates: RuntimeCertificateSettings;
  panel: RuntimePanelSettings;
  agent: RuntimeAgentSettings;
  tailscale: RuntimeTailscaleSettings;
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
