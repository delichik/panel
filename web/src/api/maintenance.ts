import { ApiError, fetchJson } from './client';
import { fetchDownload, type DownloadResult } from './download';
import type { MaintenanceSession, MaintenanceStatus } from '@/types/maintenance';

type MaintenanceMode = 'export' | 'restore';

function tokenKey(mode: MaintenanceMode) {
  return `panel.maintenance.${mode}.token`;
}

function token(mode: MaintenanceMode) {
  return sessionStorage.getItem(tokenKey(mode)) || '';
}

function headers(mode: MaintenanceMode, json = false): HeadersInit {
  return {
    Accept: 'application/json',
    ...(json ? { 'Content-Type': 'application/json' } : {}),
    ...(token(mode) ? { Authorization: `Bearer ${token(mode)}` } : {}),
  };
}

/**
 * Maintenance requests use their own maintenance session token, so they must
 * never trigger the panel-wide 401 handler (a stale maintenance token must not
 * sign the panel user out).
 */
async function request<T>(mode: MaintenanceMode, method: string, path: string, body?: unknown): Promise<T> {
  return fetchJson<T>(`/api/v1${path}`, {
    method,
    headers: headers(mode, body !== undefined),
    body,
    triggerUnauthorized: false,
  });
}

export const maintenanceApi = {
  storedToken(mode: MaintenanceMode) {
    return token(mode);
  },
  async login(mode: MaintenanceMode, username: string, password: string) {
    const session = await request<MaintenanceSession>(mode, 'POST', '/auth/login', { username, password });
    sessionStorage.setItem(tokenKey(mode), session.token);
    return session;
  },
  async logout(mode: MaintenanceMode) {
    try {
      if (token(mode)) await request<void>(mode, 'POST', '/auth/logout');
    } finally {
      sessionStorage.removeItem(tokenKey(mode));
    }
  },
  session(mode: MaintenanceMode) {
    return request<MaintenanceSession>(mode, 'GET', '/auth/session');
  },
  exportStatus() {
    return request<MaintenanceStatus>('export', 'GET', '/backups/export/current');
  },
  startExport(status: MaintenanceStatus) {
    return request<MaintenanceStatus>('export', 'POST', '/backups/export/start', { expectedRevision: status.revision, clientOperationId: `export-${Date.now()}` });
  },
  submitExportPassword(status: MaintenanceStatus, password: string) {
    return request<MaintenanceStatus>('export', 'POST', '/backups/export/password', { expectedRevision: status.revision, clientOperationId: `password-${Date.now()}`, password });
  },
  exitExport() {
    return request<MaintenanceStatus>('export', 'POST', '/backups/export/exit');
  },
  restoreStatus() {
    return request<MaintenanceStatus>('restore', 'GET', '/restore/status');
  },
  submitRestorePassword(status: MaintenanceStatus, password: string) {
    return request<MaintenanceStatus>('restore', 'POST', '/restore/password', { expectedRevision: status.revision, clientOperationId: `restore-password-${Date.now()}`, password });
  },
  retryRestore(status: MaintenanceStatus) {
    return request<MaintenanceStatus>('restore', 'POST', '/restore/retry', { expectedRevision: status.revision, clientOperationId: `restore-retry-${Date.now()}` });
  },
  clearRestorePending(status: MaintenanceStatus) {
    return request<MaintenanceStatus>('restore', 'POST', '/restore/clear-pending', { expectedRevision: status.revision, clientOperationId: `restore-clear-${Date.now()}` });
  },
  downloadExport(status: MaintenanceStatus): Promise<DownloadResult> {
    if (!status.exportId) throw new ApiError('Export archive is not ready.', 400, 'export_not_ready');
    return fetchDownload(`/api/v1/backups/export/${encodeURIComponent(status.exportId)}/download`, {
      headers: { Authorization: `Bearer ${token('export')}` },
      triggerUnauthorized: false,
    }, `${status.exportId}.zip`);
  },
};