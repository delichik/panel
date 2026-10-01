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

export interface RuntimeSettings {
  listenAddress: string;
  appDatabase: string;
  metricsDatabase: string;
  dataRoot: string;
  metricsRetentionDays: number;
  metricsCollectionIntervalSeconds: number;
  containerReportIntervalSeconds: number;
  cleanupSchedule: string;
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

export interface RuntimeUpdate {
  metricsRetentionDays: number;
  metricsCollectionIntervalSeconds: number;
  containerReportIntervalSeconds: number;
  cleanupSchedule: string;
  tokenExpiration: string;
  language: string;
  logLevel: string;
  remoteCommandTimeoutSeconds: number;
  reconcileTraceEnabled?: boolean;
  branding?: RuntimeBrandingSettings;
  certificates?: RuntimeCertificateSettings;
  panel?: RuntimePanelSettings;
  agent?: RuntimeAgentSettings;
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
