import type { MaintenanceSession, MaintenanceStatus } from '@/types/maintenance';

let phase = 0;

export const maintenanceSession: MaintenanceSession = {
  token: 'panel_maintenance_export_mock_token',
  username: 'admin',
  expiresAt: '2026-07-21T10:00:00.000Z',
};

export function exportStatus(): MaintenanceStatus {
  const phases = ['ready', 'checkpointing', 'archiving', 'password_required', 'encrypting', 'completed'];
  const current = phases[Math.min(phase, phases.length - 1)];
  return status('backup_exporting', current, current === 'completed' ? 100 : 10 + phase * 18, {
    exportId: 'export-maintenance-demo',
    downloadAvailable: current === 'completed',
    capabilities: {
      canStart: current === 'ready',
      canSubmitPassword: current === 'password_required',
      canRetry: false,
      canClearPending: false,
      canDownload: current === 'completed',
      canExit: current === 'completed',
    },
  });
}

export function restoreStatus(): MaintenanceStatus {
  return status('restore_running', 'failed', 100, {
    error: 'Unable to apply restored data; previous data was restored',
    errorDetail: { code: 'restore_apply_failed', message: 'Unable to apply restored data; previous data was restored', retryable: true },
    capabilities: { canStart: false, canSubmitPassword: false, canRetry: true, canClearPending: true, canDownload: false, canExit: false },
  });
}

export function advanceExport() {
  phase += 1;
  return exportStatus();
}

export function resetExport() {
  phase = 0;
}

function status(mode: string, phaseName: string, progress: number, extra: Partial<MaintenanceStatus>): MaintenanceStatus {
  return {
    schemaVersion: 1,
    revision: phase + 1,
    mode,
    phase: phaseName,
    progress,
    startedAt: '2026-07-21T07:45:00.000Z',
    capabilities: { canStart: false, canSubmitPassword: false, canRetry: false, canClearPending: false, canDownload: false, canExit: false },
    retryable: Boolean(extra.errorDetail?.retryable),
    pollAfterMs: 1500,
    downloadAvailable: false,
    restartSupported: true,
    manifest: { formatVersion: 1, panelVersion: 'alpha', createdAt: '2026-07-21T07:40:00.000Z', encrypted: true, includes: ['app', 'logs'], files: [{ path: 'app.db', size: 42000, sha256: 'abc123' }] },
    ...extra,
  };
}
