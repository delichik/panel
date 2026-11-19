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
