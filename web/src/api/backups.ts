import { apiClient, type ApiClient } from './client';
import type {
  BackupExportDto,
  BackupExportInput,
  BackupStatusDto,
  RestoreConfirmDto,
  RestorePreflightDto,
} from '@/types/api';

export function createBackupsApi(client: ApiClient = apiClient) {
  return {
    status() {
      return client.get<BackupStatusDto>('/backups/export/current');
    },
    startExport(input: BackupExportInput) {
      return client.post<BackupExportDto>('/backups/export', input);
    },
    exitExportMaintenance() {
      return client.post<BackupStatusDto>('/backups/export/exit');
    },
    downloadExport(exportId: string) {
      return client.download(`/backups/export/${encodeURIComponent(exportId)}/download`);
    },
    submitExportPassword(password: string) {
      return client.post<BackupStatusDto>('/backups/export/password', { password });
    },
    preflightRestore(file: File, password: string) {
      const formData = new FormData();
      formData.set('file', file);
      formData.set('password', password);
      return client.postForm<RestorePreflightDto>('/backups/restore/preflight', formData);
    },
    confirmRestore(file: File, password: string) {
      const formData = new FormData();
      formData.set('file', file);
      formData.set('password', password);
      formData.set('confirmOverwrite', 'true');
      return client.postForm<RestoreConfirmDto>('/backups/restore/confirm', formData);
    },
  };
}

export const backupsApi = createBackupsApi();
