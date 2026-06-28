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
const { formatDateTime, t, translateRuntimeStatus } = useI18n();
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
const persistentRestoreWillRestart = computed(() => (props.application.allocationCount ?? 0) > 0);
const persistentRestoreTitle = computed(() => persistentRestoreWillRestart.value ? t('applicationDetail.restorePersistentData') : t('applicationDetail.importPersistentData'));
const persistentRestoreAction = computed(() => persistentRestoreWillRestart.value ? t('applicationDetail.restoreAndRestart') : t('applicationDetail.importPersistentData'));
const persistentRestoreWarning = computed(() => persistentRestoreWillRestart.value ? t('applicationDetail.restorePersistentWarning') : t('applicationDetail.importPersistentWarning'));
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
const runtimeStatus = computed(() => props.application.runtimeStatus || (props.application.enabled ? 'pending' : 'stopped'));

function runtimeStatusColor(status: string) {
  if (status === 'running') return 'success';
  if (status === 'pending' || status === 'deploying') return 'warning';
  if (status === 'failed' || status === 'unknown') return 'error';
  return 'grey';
}

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
    if (result.application) emit('changed', result.application);
    lastTaskId.value = result.taskId || '';
    message.value = result.taskId ? t('applicationDetail.restorePersistentStarted') : t('applicationDetail.importPersistentStarted');
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
  <div class="application-detail">
    <v-card variant="outlined" class="detail-card">
      <div class="detail-header">
        <div class="min-width-0">
          <div class="text-h6 font-weight-bold text-truncate">{{ application.name }}</div>
          <div class="text-body-2 text-medium-emphasis text-truncate">{{ application.jobId }} / {{ application.namespace }}</div>
          <div class="detail-statuses">
            <v-chip :color="runtimeStatusColor(runtimeStatus)" size="small" variant="tonal" label>{{ translateRuntimeStatus(runtimeStatus) }}</v-chip>
            <v-chip :color="application.enabled ? 'success' : 'grey'" size="small" variant="tonal" label>{{ application.enabled ? t('common.enabled') : t('common.disabled') }}</v-chip>
          </div>
        </div>
        <div class="detail-actions">
          <slot name="actions" />
          <v-menu location="bottom end">
            <template #activator="{ props: menuProps }">
              <v-btn v-bind="menuProps" icon="mdi-dots-vertical" variant="text" size="small" :aria-label="t('common.more')" />
            </template>
            <v-list density="compact">
              <v-list-item prepend-icon="mdi-package-down" :title="t('applicationDetail.downloadPackage')" :disabled="downloading" @click="downloadPackage" />
              <v-list-item prepend-icon="mdi-database-arrow-down-outline" :title="t('applicationDetail.downloadPersistentData')" :disabled="!application.persistentPath || downloadingPersistent" @click="downloadPersistentData" />
              <v-list-item prepend-icon="mdi-database-arrow-up-outline" :title="persistentRestoreTitle" :disabled="!application.persistentPath" @click="restoreDialog = true" />
              <v-list-item prepend-icon="mdi-swap-horizontal" :title="t('applicationDetail.migrateApplication')" :disabled="!!application.persistentPath" @click="openMigrateDialog" />
              <slot name="more-actions" />
            </v-list>
          </v-menu>
        </div>
      </div>

      <div class="detail-body">
        <v-alert v-if="message" type="info" variant="tonal" density="compact" closable @click:close="message = ''">
          <div class="task-alert">
            <span>{{ message }}</span>
            <v-btn v-if="lastTaskId" size="small" variant="text" :to="taskRoute()" class="text-none">{{ t('taskCenter.task') }}</v-btn>
          </div>
        </v-alert>
        <v-alert v-if="error || application.lastError" type="error" variant="tonal" density="compact">
          {{ error || application.lastError }}
        </v-alert>

        <section class="detail-section">
          <div class="section-title">{{ t('applicationDetail.basicInfo') }}</div>
          <div class="property-grid">
            <div><span>{{ t('applicationDetail.generation') }}</span><strong class="font-tabular">{{ application.generation }}</strong></div>
            <div><span>{{ t('applicationDetail.specHash') }}</span><strong class="mono">{{ application.specHash || t('common.notAvailable') }}</strong></div>
            <div><span>{{ t('applicationDetail.lastEval') }}</span><strong class="mono">{{ application.lastEvalId || t('common.notAvailable') }}</strong></div>
            <div><span>{{ t('applicationDetail.persistentPath') }}</span><strong class="mono">{{ application.persistentPath || t('common.notAvailable') }}</strong></div>
          </div>
        </section>

        <section class="detail-section">
          <div class="section-heading">
            <div class="min-width-0">
              <div class="section-title">{{ t('applicationDetail.imageUpdate') }}</div>
              <div class="section-subtitle text-truncate">{{ application.imageReference || t('applicationDetail.imageHint') }}</div>
            </div>
            <div class="section-actions">
              <v-chip size="small" variant="tonal" :color="imageStatusColor" label>{{ imageStatusLabel }}</v-chip>
              <v-btn size="small" prepend-icon="mdi-package-up" color="primary" variant="flat" class="text-none" :disabled="!application.enabled || !application.imageUpdateAvailable" :loading="imageAction === 'update'" @click="updateImage">{{ t('applicationDetail.updateImage') }}</v-btn>
            </div>
          </div>
          <div class="property-grid property-grid--three">
            <div><span>{{ t('applicationDetail.applied') }}</span><strong class="mono">{{ shortDigest(application.imageDigest) }}</strong></div>
            <div><span>{{ t('applicationDetail.latest') }}</span><strong class="mono">{{ shortDigest(application.imageLatestDigest) }}</strong></div>
            <div><span>{{ t('applicationDetail.checked') }}</span><strong>{{ formatCheckedAt(application.imageCheckedAt) }}</strong></div>
          </div>
          <div v-if="imageTargets.length" class="image-targets">
            <v-table density="compact" class="image-target-table">
              <thead><tr><th>{{ t('applicationDetail.server') }}</th><th>{{ t('common.status') }}</th><th>{{ t('applicationDetail.applied') }}</th><th>{{ t('applicationDetail.latest') }}</th><th>{{ t('applicationDetail.checked') }}</th></tr></thead>
              <tbody>
                <tr v-for="target in imageTargets" :key="target.serverId">
                  <td><div class="font-weight-medium text-truncate">{{ target.serverName || target.serverId }}</div><div class="text-caption text-medium-emphasis text-truncate">{{ target.reference }}</div></td>
                  <td><v-chip size="x-small" variant="tonal" :color="imageTargetStatusColor(target.updateAvailable, target.lastError)" label>{{ imageTargetStatusLabel(target.updateAvailable, target.lastError) }}</v-chip><div v-if="target.lastError" class="text-caption text-error">{{ target.lastError }}</div></td>
                  <td class="mono">{{ shortDigest(target.localDigest) }}</td>
                  <td class="mono">{{ shortDigest(target.latestDigest) }}</td>
                  <td>{{ formatCheckedAt(target.checkedAt) }}</td>
                </tr>
              </tbody>
            </v-table>
          </div>
          <v-alert v-if="application.imageLastError" type="warning" variant="tonal" density="compact">{{ application.imageLastError }}</v-alert>
        </section>

        <ApplicationRuntimePanel embedded :application="application" @logs="selectLogs" />
      </div>
    </v-card>

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
          <span class="app-dialog-title-text">{{ persistentRestoreTitle }}</span>
          <v-btn icon="mdi-close" variant="text" :aria-label="t('common.close')" @click="restoreDialog = false" />
        </v-card-title>
        <v-divider />
        <v-card-text class="app-dialog-body">
          <v-alert type="warning" variant="tonal" density="compact" class="mb-4">
            {{ persistentRestoreWarning }}
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
            {{ persistentRestoreAction }}
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
.application-detail { display: flex; min-width: 0; min-height: 0; height: 100%; }
.detail-card { display: flex; flex: 1 1 auto; flex-direction: column; min-width: 0; min-height: 0; overflow: hidden; }
.detail-header { display: flex; flex: 0 0 auto; align-items: flex-start; justify-content: space-between; gap: 14px; padding: 16px; border-bottom: 1px solid var(--lp-border); }
.detail-actions { display: flex; align-items: center; justify-content: flex-end; gap: 8px; flex-wrap: wrap; }
.detail-statuses { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; margin-top: 8px; }
.detail-body { display: grid; flex: 1 1 auto; gap: 18px; align-content: start; min-height: 0; padding: 16px; overflow: auto; }
.detail-section { display: grid; gap: 12px; }
.section-heading { display: flex; align-items: flex-start; justify-content: space-between; gap: 12px; }
.section-title { font-size: 0.92rem; font-weight: 700; }
.section-subtitle { margin-top: 2px; color: var(--lp-text-muted); font-size: 0.78rem; }
.section-actions { display: flex; align-items: center; justify-content: flex-end; gap: 8px; flex-wrap: wrap; }
.min-width-0 { min-width: 0; }
.property-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 10px; }
.property-grid--three { grid-template-columns: repeat(3, minmax(0, 1fr)); }
.property-grid > div { display: grid; gap: 4px; min-width: 0; padding: 10px 12px; border: 1px solid var(--lp-border); border-radius: 8px; }
.property-grid span { color: var(--lp-text-muted); font-size: 0.76rem; }
.property-grid strong { min-width: 0; overflow-wrap: anywhere; font-size: 0.86rem; }
.image-targets { overflow-x: auto; }
.image-target-table { min-width: 680px; background: transparent; }
.image-target-table :deep(td) { vertical-align: middle; }
.task-alert { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.mono { font-family: "Cascadia Code", "SFMono-Regular", Consolas, monospace; font-size: 0.8rem; }
.migrate-dialog-body { display: grid; gap: 14px; }
@media (max-width: 900px) {
  .property-grid--three { grid-template-columns: 1fr; }
}

@media (max-width: 600px) {
  .application-detail { height: auto; }
  .detail-card, .detail-body { overflow: visible; }
  .detail-header,
  .section-heading {
    align-items: stretch;
    flex-direction: column;
  }
  .detail-actions,
  .section-actions {
    justify-content: flex-start;
  }
  .property-grid { grid-template-columns: 1fr; }
}
</style>
