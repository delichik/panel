<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import { useI18n } from '@/i18n';
import { applicationsApi } from '@/api/applications';
import { serversApi } from '@/api/servers';
import type { ApplicationDto, ServerDto } from '@/types/api';
import ApplicationRuntimePanel from './ApplicationRuntimePanel.vue';
import RuntimeLogsDialog from '@/components/RuntimeLogsDialog.vue';

const props = defineProps<{ application: ApplicationDto }>();
const emit = defineEmits<{ changed: [ApplicationDto] }>();
const { formatDateTime, t } = useI18n();
const downloading = ref(false);
const downloadingPersistent = ref(false);
const restoringPersistent = ref(false);
const restoreDialog = ref(false);
const restoreFile = ref<File | File[] | null>(null);
const migrateDialog = ref(false);
const migrating = ref(false);
const loadingServers = ref(false);
const servers = ref<ServerDto[]>([]);
const migrationForm = ref({ sourceServerId: '', targetServerId: '' });
const imageAction = ref('');
const error = ref('');
const message = ref('');
const lastTaskId = ref('');
const logTarget = ref<{ instanceId: string; containerName: string } | null>(null);
const logsDialog = ref(false);
const selectedRestoreFile = computed(() => Array.isArray(restoreFile.value) ? restoreFile.value[0] : restoreFile.value);
const imageTargets = computed(() => props.application.imageUpdateTargets ?? []);
const imageUpdateTargetCount = computed(() => imageTargets.value.filter((target) => target.updateAvailable).length);
const imageTargetHasError = computed(() => imageTargets.value.some((target) => !!target.lastError));
const serverOptions = computed(() => servers.value.map((server) => ({
  title: `${server.name || server.id} (${server.id})`,
  value: server.id,
})));
const imageStatusColor = computed(() => {
  if (props.application.imageLastError || imageTargetHasError.value) return 'error';
  if (props.application.imageUpdateAvailable) return 'warning';
  if (props.application.imageDigest || imageTargets.value.some((target) => target.checkedAt)) return 'success';
  return 'grey';
});
const imageStatusLabel = computed(() => {
  if (props.application.imageLastError || imageTargetHasError.value) return t('applicationDetail.checkFailedStatus');
  if (props.application.imageUpdateAvailable) return t('applicationDetail.updateTargetCount', { count: imageUpdateTargetCount.value });
  if (props.application.imageDigest || imageTargets.value.some((target) => target.checkedAt)) return t('applicationDetail.upToDate');
  return t('applicationDetail.notChecked');
});

function shortDigest(value?: string) {
  if (!value) return t('common.notAvailable');
  const [algo, digest] = value.split(':');
  if (!digest) return value.length > 18 ? `${value.slice(0, 18)}...` : value;
  return `${algo}:${digest.slice(0, 12)}`;
}

function formatCheckedAt(value?: string) {
  if (!value) return t('common.never');
  return formatDateTime(value);
}

function imageTargetStatusColor(updateAvailable: boolean, lastError?: string) {
  if (lastError) return 'error';
  return updateAvailable ? 'warning' : 'success';
}

function imageTargetStatusLabel(updateAvailable: boolean, lastError?: string) {
  if (lastError) return t('applicationDetail.checkFailedStatus');
  return updateAvailable ? t('applicationDetail.updateAvailable') : t('applicationDetail.upToDate');
}

async function downloadPackage() {
  downloading.value = true;
  try {
    const result = await applicationsApi.package(props.application.id);
    const url = URL.createObjectURL(result.blob);
    const link = document.createElement('a');
    link.href = url;
    link.download = result.filename;
    link.click();
    URL.revokeObjectURL(url);
    error.value = '';
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('applicationDetail.downloadFailed');
  } finally {
    downloading.value = false;
  }
}

async function updateImage() {
  imageAction.value = 'update';
  try {
    const result = await applicationsApi.updateImage(props.application.id);
    if (result.application) emit('changed', result.application);
    lastTaskId.value = result.taskId || '';
    message.value = t('applicationDetail.updateStarted');
    error.value = '';
  } catch (err) {
    lastTaskId.value = '';
    message.value = '';
    error.value = err instanceof Error ? err.message : t('applicationDetail.updateFailed');
  } finally {
    imageAction.value = '';
  }
}

function taskRoute(taskId = lastTaskId.value) {
  return taskId ? { path: '/tasks', query: { task: taskId } } : '/tasks';
}

function selectLogs(target: { instanceId: string; containerName: string }) {
  logTarget.value = target;
  logsDialog.value = true;
}

async function downloadPersistentData() {
  if (!props.application.persistentPath) return;
  downloadingPersistent.value = true;
  try {
    const result = await applicationsApi.persistentData(props.application.id);
    const url = URL.createObjectURL(result.blob);
    const link = document.createElement('a');
    link.href = url;
    link.download = result.filename;
    link.click();
    URL.revokeObjectURL(url);
    error.value = '';
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('applicationDetail.downloadPersistentFailed');
  } finally {
    downloadingPersistent.value = false;
  }
}

async function restorePersistentData() {
  const file = selectedRestoreFile.value;
  if (!file || !props.application.persistentPath) return;
  restoringPersistent.value = true;
  try {
    const result = await applicationsApi.restorePersistentData(props.application.id, file);
    lastTaskId.value = result.taskId || '';
    message.value = t('applicationDetail.restorePersistentStarted');
    restoreDialog.value = false;
    restoreFile.value = null;
    error.value = '';
  } catch (err) {
    lastTaskId.value = '';
    message.value = '';
    error.value = err instanceof Error ? err.message : t('applicationDetail.restorePersistentFailed');
  } finally {
    restoringPersistent.value = false;
  }
}

async function loadServersForMigration() {
  loadingServers.value = true;
  try {
    servers.value = await serversApi.listServers();
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('applicationDetail.loadServersFailed');
  } finally {
    loadingServers.value = false;
  }
}

async function openMigrateDialog() {
  migrationForm.value = {
    sourceServerId: props.application.deploymentServers?.[0] || '',
    targetServerId: '',
  };
  migrateDialog.value = true;
  if (servers.value.length === 0) {
    await loadServersForMigration();
  }
}

async function migrateApplication() {
  const sourceServerId = migrationForm.value.sourceServerId.trim();
  const targetServerId = migrationForm.value.targetServerId.trim();
  if (!sourceServerId || !targetServerId) return;
  migrating.value = true;
  try {
    const result = await applicationsApi.migrate(props.application.id, sourceServerId, targetServerId);
    if (result.application) emit('changed', result.application);
    lastTaskId.value = result.taskId || '';
    message.value = t('applicationDetail.migrationStarted');
    migrateDialog.value = false;
    error.value = '';
  } catch (err) {
    lastTaskId.value = '';
    message.value = '';
    error.value = err instanceof Error ? err.message : t('applicationDetail.migrationFailed');
  } finally {
    migrating.value = false;
  }
}

async function loadSelectedLogs(tail: number) {
  if (!logTarget.value) return '';
  const result = await applicationsApi.logs(props.application.id, {
    instanceId: logTarget.value.instanceId,
    containerName: logTarget.value.containerName,
    type: 'stdout',
    tail,
  });
  return result.logs;
}

watch(() => props.application.id, () => {
  logTarget.value = null;
  logsDialog.value = false;
  message.value = '';
  lastTaskId.value = '';
});
</script>

<template>
  <div class="detail-stack">
    <v-card variant="outlined" class="detail-card">
      <div class="detail-heading">
        <div class="min-width-0">
          <div class="text-subtitle-1 font-weight-bold text-truncate">{{ application.name }}</div>
          <div class="text-caption text-medium-emphasis text-truncate">{{ application.jobId }} / {{ application.namespace }}</div>
        </div>
        <div class="detail-heading-actions">
          <v-btn size="small" icon="mdi-package-down" variant="text" :title="t('applicationDetail.downloadPackage')" :loading="downloading" @click="downloadPackage" />
          <v-btn
            size="small"
            icon="mdi-database-arrow-down-outline"
            variant="text"
            :title="t('applicationDetail.downloadPersistentData')"
            :disabled="!application.persistentPath"
            :loading="downloadingPersistent"
            @click="downloadPersistentData"
          />
          <v-btn
            size="small"
            icon="mdi-database-arrow-up-outline"
            variant="text"
            :title="t('applicationDetail.restorePersistentData')"
            :disabled="!application.persistentPath"
            @click="restoreDialog = true"
          />
          <v-btn
            size="small"
            icon="mdi-swap-horizontal"
            variant="text"
            :title="t('applicationDetail.migrateApplication')"
            :disabled="!!application.persistentPath"
            @click="openMigrateDialog"
          />
          <v-chip :color="application.enabled ? 'success' : 'grey'" size="small" variant="tonal" label>{{ application.enabled ? t('common.enabled') : t('common.disabled') }}</v-chip>
        </div>
      </div>
      <v-divider class="my-3" />
      <div class="meta-grid">
        <div><div class="text-caption text-medium-emphasis">{{ t('applicationDetail.generation') }}</div><div class="font-weight-bold font-tabular">{{ application.generation }}</div></div>
        <div><div class="text-caption text-medium-emphasis">{{ t('applicationDetail.specHash') }}</div><div class="mono text-truncate">{{ application.specHash || t('common.notAvailable') }}</div></div>
        <div><div class="text-caption text-medium-emphasis">{{ t('applicationDetail.lastEval') }}</div><div class="mono text-truncate">{{ application.lastEvalId || t('common.notAvailable') }}</div></div>
        <div><div class="text-caption text-medium-emphasis">{{ t('applicationDetail.persistentPath') }}</div><div class="mono text-truncate">{{ application.persistentPath || t('common.notAvailable') }}</div></div>
      </div>
      <v-divider class="my-3" />
      <div class="image-panel">
        <div class="min-width-0">
          <div class="d-flex align-center ga-2 mb-1">
            <div class="text-subtitle-2 font-weight-bold">{{ t('applicationDetail.imageUpdate') }}</div>
            <v-chip size="x-small" variant="tonal" :color="imageStatusColor" label>
              {{ imageStatusLabel }}
            </v-chip>
          </div>
          <div class="text-caption text-medium-emphasis text-truncate">{{ application.imageReference || t('applicationDetail.imageHint') }}</div>
          <div class="digest-grid mt-2">
            <div><div class="text-caption text-medium-emphasis">{{ t('applicationDetail.applied') }}</div><div class="mono text-truncate">{{ shortDigest(application.imageDigest) }}</div></div>
            <div><div class="text-caption text-medium-emphasis">{{ t('applicationDetail.latest') }}</div><div class="mono text-truncate">{{ shortDigest(application.imageLatestDigest) }}</div></div>
            <div><div class="text-caption text-medium-emphasis">{{ t('applicationDetail.checked') }}</div><div class="text-body-2">{{ formatCheckedAt(application.imageCheckedAt) }}</div></div>
          </div>
        </div>
        <div class="image-actions">
          <v-btn size="small" prepend-icon="mdi-package-up" color="primary" variant="flat" class="text-none" :disabled="!application.enabled || !application.imageUpdateAvailable" :loading="imageAction === 'update'" @click="updateImage">{{ t('applicationDetail.updateImage') }}</v-btn>
        </div>
      </div>
      <div v-if="imageTargets.length" class="image-targets mt-3">
        <v-table density="compact" class="image-target-table">
          <thead>
            <tr>
              <th>{{ t('applicationDetail.server') }}</th>
              <th>{{ t('common.status') }}</th>
              <th>{{ t('applicationDetail.applied') }}</th>
              <th>{{ t('applicationDetail.latest') }}</th>
              <th>{{ t('applicationDetail.checked') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="target in imageTargets" :key="target.serverId">
              <td>
                <div class="font-weight-medium text-truncate">{{ target.serverName || target.serverId }}</div>
                <div class="text-caption text-medium-emphasis text-truncate">{{ target.reference }}</div>
              </td>
              <td>
                <v-chip size="x-small" variant="tonal" :color="imageTargetStatusColor(target.updateAvailable, target.lastError)" label>
                  {{ imageTargetStatusLabel(target.updateAvailable, target.lastError) }}
                </v-chip>
                <div v-if="target.lastError" class="text-caption text-error text-truncate">{{ target.lastError }}</div>
              </td>
              <td class="mono text-truncate">{{ shortDigest(target.localDigest) }}</td>
              <td class="mono text-truncate">{{ shortDigest(target.latestDigest) }}</td>
              <td class="text-body-2">{{ formatCheckedAt(target.checkedAt) }}</td>
            </tr>
          </tbody>
        </v-table>
      </div>
      <v-alert v-if="application.imageLastError" type="warning" variant="tonal" class="mt-3">{{ application.imageLastError }}</v-alert>
      <v-alert v-if="message" type="info" variant="tonal" class="mt-3" closable @click:close="message = ''">
        <div class="task-alert">
          <span>{{ message }}</span>
          <v-btn v-if="lastTaskId" size="small" variant="text" :to="taskRoute()" class="text-none">{{ t('taskCenter.task') }}</v-btn>
        </div>
      </v-alert>
      <v-alert v-if="error" type="error" variant="tonal" class="mt-3">{{ error }}</v-alert>
      <v-alert v-if="application.lastError" type="error" variant="tonal" class="mt-3">{{ application.lastError }}</v-alert>
    </v-card>
    <ApplicationRuntimePanel :application="application" @logs="selectLogs" />
    <RuntimeLogsDialog
      v-model:open="logsDialog"
      :title="t('applicationLogs.logs')"
      :subtitle="logTarget ? t('applicationLogs.selectedTarget', { instance: logTarget.instanceId, container: logTarget.containerName || '-' }) : ''"
      :target-key="logTarget ? `${application.id}:${logTarget.instanceId}:${logTarget.containerName}` : ''"
      :loader="loadSelectedLogs"
    />

    <v-dialog v-model="restoreDialog" width="560">
      <v-card class="app-dialog-card">
        <v-card-title class="app-dialog-title">
          <span class="app-dialog-title-text">{{ t('applicationDetail.restorePersistentData') }}</span>
          <v-btn icon="mdi-close" variant="text" :aria-label="t('common.close')" @click="restoreDialog = false" />
        </v-card-title>
        <v-divider />
        <v-card-text class="app-dialog-body">
          <v-alert type="warning" variant="tonal" density="compact" class="mb-4">
            {{ t('applicationDetail.restorePersistentWarning') }}
          </v-alert>
          <v-file-input
            v-model="restoreFile"
            :label="t('applicationDetail.persistentArchive')"
            accept=".zip,application/zip"
            variant="outlined"
            density="comfortable"
            prepend-icon="mdi-archive-arrow-up-outline"
          />
        </v-card-text>
        <v-divider />
        <v-card-actions class="app-dialog-actions">
          <v-btn variant="text" class="text-none" :disabled="restoringPersistent" @click="restoreDialog = false">{{ t('common.cancel') }}</v-btn>
          <v-btn color="warning" variant="flat" class="text-none" :loading="restoringPersistent" :disabled="!selectedRestoreFile" @click="restorePersistentData">
            {{ t('applicationDetail.restoreAndRestart') }}
          </v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <v-dialog v-model="migrateDialog" width="620">
      <v-card class="app-dialog-card">
        <v-card-title class="app-dialog-title">
          <span class="app-dialog-title-text">{{ t('applicationDetail.migrateApplication') }}</span>
          <v-btn icon="mdi-close" variant="text" :aria-label="t('common.close')" @click="migrateDialog = false" />
        </v-card-title>
        <v-divider />
        <v-card-text class="app-dialog-body migrate-dialog-body">
          <v-alert type="info" variant="tonal" density="compact">
            {{ t('applicationDetail.migrationHint') }}
          </v-alert>
          <v-select
            v-model="migrationForm.sourceServerId"
            :items="serverOptions"
            :label="t('applicationDetail.sourceServer')"
            item-title="title"
            item-value="value"
            variant="outlined"
            density="comfortable"
            :loading="loadingServers"
          />
          <v-select
            v-model="migrationForm.targetServerId"
            :items="serverOptions"
            :label="t('applicationDetail.targetServer')"
            item-title="title"
            item-value="value"
            variant="outlined"
            density="comfortable"
            :loading="loadingServers"
          />
        </v-card-text>
        <v-divider />
        <v-card-actions class="app-dialog-actions">
          <v-btn variant="text" class="text-none" :disabled="migrating" @click="migrateDialog = false">{{ t('common.cancel') }}</v-btn>
          <v-btn
            color="primary"
            variant="flat"
            class="text-none"
            :loading="migrating"
            :disabled="!migrationForm.sourceServerId || !migrationForm.targetServerId || migrationForm.sourceServerId === migrationForm.targetServerId"
            @click="migrateApplication"
          >
            {{ t('applicationDetail.startMigration') }}
          </v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>
  </div>
</template>

<style scoped>
.detail-stack { display: grid; gap: 14px; }
.detail-card { padding: 16px; }
.detail-heading { display: flex; align-items: flex-start; justify-content: space-between; gap: 12px; }
.detail-heading-actions { display: flex; align-items: center; justify-content: flex-end; gap: 8px; flex-wrap: wrap; }
.min-width-0 { min-width: 0; }
.meta-grid { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 12px; }
.image-panel { display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: 14px; align-items: start; }
.image-actions { display: flex; gap: 8px; flex-wrap: wrap; justify-content: flex-end; }
.digest-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 10px; }
.image-targets { overflow-x: auto; }
.image-target-table { min-width: 680px; background: transparent; }
.image-target-table :deep(td) { vertical-align: middle; }
.task-alert { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.mono { font-size: 0.8rem; }
.migrate-dialog-body { display: grid; gap: 14px; }
@media (max-width: 900px) {
  .meta-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .image-panel { grid-template-columns: 1fr; }
  .image-actions { justify-content: flex-start; }
  .digest-grid { grid-template-columns: 1fr; }
}

@media (max-width: 600px) {
  .detail-heading {
    align-items: stretch;
    flex-direction: column;
  }

  .detail-heading-actions {
    justify-content: flex-start;
  }
}
</style>
