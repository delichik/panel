<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue';
import { backupsApi } from '@/api/backups';
import { useI18n } from '@/i18n';
import type { BackupStatusDto } from '@/types/api';

const { t, formatDateTime } = useI18n();

const status = ref<BackupStatusDto | null>(null);
const loading = ref(false);
const error = ref('');
const finished = ref(false);
const password = ref('');
let timer: number | undefined;

const phaseText = computed(() => t(`backupRestore.phases.${status.value?.phase || 'idle'}`));
const canDownload = computed(() => Boolean(status.value?.downloadAvailable && status.value.exportId));
const isFinished = computed(() => status.value?.phase === 'completed' || status.value?.phase === 'failed');
const needsPassword = computed(() => status.value?.phase === 'password_required');
const canStart = computed(() => status.value?.phase === 'ready');

async function loadStatus() {
  try {
    status.value = await backupsApi.status();
    error.value = '';
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('backupRestore.statusLoadFailed');
  }
}

async function downloadBackup() {
  if (!status.value?.exportId) return;
  loading.value = true;
  try {
    const result = await backupsApi.downloadExport(status.value.exportId);
    const url = URL.createObjectURL(result.blob);
    const link = document.createElement('a');
    link.href = url;
    link.download = result.filename;
    link.click();
    URL.revokeObjectURL(url);
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('backupRestore.downloadFailed');
  } finally {
    loading.value = false;
  }
}

async function exitMaintenance() {
  loading.value = true;
  try {
    status.value = await backupsApi.exitExportMaintenance();
    finished.value = true;
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('backupRestore.exitFailed');
  } finally {
    loading.value = false;
  }
}

async function submitPassword() {
  loading.value = true;
  try {
    await backupsApi.submitExportPassword(password.value);
    password.value = '';
    await loadStatus();
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('backupRestore.passwordSubmitFailed');
  } finally {
    loading.value = false;
  }
}

async function startExport() {
  loading.value = true;
  try {
    status.value = await backupsApi.startPreparedExport();
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('backupRestore.startPreparedExportFailed');
  } finally {
    loading.value = false;
  }
}

onMounted(() => {
  void loadStatus();
  timer = window.setInterval(loadStatus, 1500);
});

onBeforeUnmount(() => {
  if (timer) window.clearInterval(timer);
});
</script>

<template>
  <div class="maintenance-page">
    <v-card class="maintenance-panel" variant="outlined">
      <div class="maintenance-header">
        <v-icon size="34" color="primary">mdi-archive-sync-outline</v-icon>
        <div>
          <div class="text-overline text-medium-emphasis">{{ t('backupRestore.maintenanceEyebrow') }}</div>
          <h1>{{ t('backupRestore.exportMaintenanceTitle') }}</h1>
        </div>
      </div>

      <v-alert v-if="error || status?.error" type="error" variant="tonal" class="mb-4">
        {{ error || status?.error }}
      </v-alert>

      <v-progress-linear
        :model-value="status?.progress || 0"
        height="10"
        rounded
        color="primary"
        class="mb-5"
      />

      <div class="status-grid">
        <div>
          <span>{{ t('common.status') }}</span>
          <strong>{{ phaseText }}</strong>
        </div>
        <div>
          <span>{{ t('backupRestore.startedAt') }}</span>
          <strong>{{ status?.startedAt ? formatDateTime(status.startedAt) : t('common.notAvailable') }}</strong>
        </div>
        <div>
          <span>{{ t('backupRestore.backupCreatedAt') }}</span>
          <strong>{{ status?.manifest?.createdAt ? formatDateTime(status.manifest.createdAt) : t('common.notAvailable') }}</strong>
        </div>
        <div>
          <span>{{ t('backupRestore.encrypted') }}</span>
          <strong>{{ status?.manifest?.encrypted ? t('common.yes') : t('common.no') }}</strong>
        </div>
      </div>

      <div v-if="needsPassword" class="password-panel">
        <v-text-field
          v-model="password"
          type="password"
          :label="t('backupRestore.backupPassword')"
          variant="outlined"
          density="comfortable"
          hide-details="auto"
        />
        <v-btn
          color="primary"
          prepend-icon="mdi-lock-open-outline"
          :disabled="!password"
          :loading="loading"
          class="text-none font-weight-bold"
          @click="submitPassword"
        >
          {{ t('backupRestore.continueExport') }}
        </v-btn>
      </div>

      <div class="maintenance-actions">
        <v-btn
          color="primary"
          prepend-icon="mdi-play"
          :disabled="!canStart"
          :loading="loading"
          class="text-none font-weight-bold"
          @click="startExport"
        >
          {{ t('backupRestore.startPreparedExport') }}
        </v-btn>
        <v-btn
          color="primary"
          prepend-icon="mdi-download"
          :disabled="!canDownload"
          :loading="loading"
          class="text-none font-weight-bold"
          @click="downloadBackup"
        >
          {{ t('backupRestore.downloadBackup') }}
        </v-btn>
        <v-btn
          variant="text"
          :disabled="!isFinished"
          :loading="loading"
          class="text-none"
          @click="exitMaintenance"
        >
          {{ t('backupRestore.finishExport') }}
        </v-btn>
      </div>
      <v-alert v-if="finished" type="success" variant="tonal" class="mt-4">
        {{ t(status?.restartSupported ? 'backupRestore.restartingAfterExport' : 'backupRestore.restartAfterExport') }}
      </v-alert>
    </v-card>
  </div>
</template>

<style scoped>
.maintenance-page {
  display: grid;
  min-height: 100dvh;
  place-items: center;
  padding: 24px;
}

.maintenance-panel {
  width: min(760px, 100%);
  padding: 24px;
  border-color: var(--lp-border) !important;
  background: var(--lp-surface) !important;
}

.maintenance-header {
  display: flex;
  align-items: center;
  gap: 14px;
  margin-bottom: 22px;
}

.maintenance-header h1 {
  margin: 0;
  font-size: 1.28rem;
  font-weight: 760;
  letter-spacing: 0;
}

.status-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
}

.status-grid > div {
  display: grid;
  gap: 4px;
  padding: 12px;
  border: 1px solid var(--lp-border);
  border-radius: 8px;
}

.status-grid span {
  color: var(--lp-text-muted);
  font-size: 0.76rem;
  font-weight: 700;
}

.status-grid strong {
  min-width: 0;
  overflow-wrap: anywhere;
}

.maintenance-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  margin-top: 22px;
}

.password-panel {
  display: grid;
  gap: 12px;
  margin-top: 18px;
}

@media (max-width: 640px) {
  .status-grid {
    grid-template-columns: 1fr;
  }
}
</style>
