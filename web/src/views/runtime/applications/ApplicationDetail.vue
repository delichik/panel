<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import { useI18n } from '@/i18n';
import { applicationsApi } from '@/api/applications';
import type { ApplicationDto } from '@/types/api';
import ApplicationRuntimePanel from './ApplicationRuntimePanel.vue';
import RuntimeLogsDialog from '@/components/RuntimeLogsDialog.vue';

const props = defineProps<{ application: ApplicationDto }>();
const emit = defineEmits<{ changed: [ApplicationDto] }>();
const { formatDateTime, t } = useI18n();
const downloading = ref(false);
const downloadingPersistent = ref(false);
const imageAction = ref('');
const error = ref('');
const message = ref('');
const lastTaskId = ref('');
const logTarget = ref<{ instanceId: string; containerName: string } | null>(null);
const logsDialog = ref(false);
const imageStatusColor = computed(() => {
  if (props.application.imageLastError) return 'error';
  if (props.application.imageUpdateAvailable) return 'warning';
  if (props.application.imageDigest) return 'success';
  return 'grey';
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
              {{ application.imageUpdateAvailable ? t('applicationDetail.updateAvailable') : application.imageDigest ? t('applicationDetail.tracked') : t('applicationDetail.notChecked') }}
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
          <v-btn size="small" prepend-icon="mdi-package-up" color="primary" variant="flat" class="text-none" :disabled="!application.enabled" :loading="imageAction === 'update'" @click="updateImage">{{ t('common.update') }}</v-btn>
        </div>
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
.task-alert { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.mono { font-size: 0.8rem; }
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
