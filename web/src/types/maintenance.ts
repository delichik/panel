import type { BackupManifest } from './settings';

export type MaintenanceMode = 'normal' | 'backup_exporting' | 'restore_pending' | 'restore_running' | string;
export type MaintenancePhase = 'idle' | 'ready' | 'password_required' | 'checkpointing' | 'archiving' | 'encrypting' | 'extracting' | 'applying' | 'completed' | 'failed' | string;

export interface MaintenanceCapabilities {
  canStart: boolean;
  canSubmitPassword: boolean;
  canRetry: boolean;
  canClearPending: boolean;
  canDownload: boolean;
  canExit: boolean;
}

export interface MaintenanceError {
  code: string;
  message: string;
  retryable: boolean;
}

export interface MaintenanceStatus {
  schemaVersion: number;
  revision: number;
  mode: MaintenanceMode;
  phase: MaintenancePhase;
  progress: number;
  startedAt?: string;
  finishedAt?: string;
  error?: string;
  errorDetail?: MaintenanceError;
  capabilities: MaintenanceCapabilities;
  retryable: boolean;
  pollAfterMs: number;
  exportId?: string;
  downloadAvailable: boolean;
  restartSupported: boolean;
  manifest?: BackupManifest;
}

export interface MaintenanceSession {
  token: string;
  username: string;
  expiresAt: string;
}
