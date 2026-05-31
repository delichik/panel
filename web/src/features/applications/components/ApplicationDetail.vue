<script setup lang="ts">
import { computed, ref } from 'vue';
import { formatDateTime, t } from '@/i18n';
import { applicationsApi } from '@/api/applications';
import type { ApplicationDto } from '@/types/api';
import ApplicationRuntimePanel from './ApplicationRuntimePanel.vue';
import ApplicationLogsPanel from './ApplicationLogsPanel.vue';

const props = defineProps<{ application: ApplicationDto }>();
const emit = defineEmits<{ changed: [ApplicationDto] }>();
const downloading = ref(false);
const imageAction = ref('');
const error = ref('');
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

async function checkImage() {
  imageAction.value = 'check';
  try {
    const app = await applicationsApi.checkImage(props.application.id);
    emit('changed', app);
    error.value = '';
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('applicationDetail.checkFailed');
  } finally {
    imageAction.value = '';
  }
}

async function updateImage() {
  imageAction.value = 'update';
  try {
    const result = await applicationsApi.updateImage(props.application.id);
    if (result.application) emit('changed', result.application);
    error.value = '';
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('applicationDetail.updateFailed');
  } finally {
    imageAction.value = '';
  }
}
</script>

<template>
  <div class="detail-stack">
    <v-card variant="outlined" class="detail-card">
      <div class="d-flex align-start justify-space-between ga-3">
        <div class="min-width-0">
          <div class="text-subtitle-1 font-weight-bold text-truncate">{{ application.name }}</div>
          <div class="text-caption text-medium-emphasis text-truncate">{{ application.jobId }} / {{ application.namespace }}</div>
        </div>
        <div class="d-flex align-center ga-2">
          <v-btn size="small" icon="mdi-package-down" variant="text" :title="t('applicationDetail.downloadPackage')" :loading="downloading" @click="downloadPackage" />
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
          <v-btn size="small" prepend-icon="mdi-cloud-search-outline" variant="outlined" class="text-none" :loading="imageAction === 'check'" @click="checkImage">{{ t('common.check') }}</v-btn>
          <v-btn size="small" prepend-icon="mdi-package-up" color="primary" variant="flat" class="text-none" :disabled="!application.enabled" :loading="imageAction === 'update'" @click="updateImage">{{ t('common.update') }}</v-btn>
        </div>
      </div>
      <v-alert v-if="application.imageLastError" type="warning" variant="tonal" class="mt-3">{{ application.imageLastError }}</v-alert>
      <v-alert v-if="error" type="error" variant="tonal" class="mt-3">{{ error }}</v-alert>
      <v-alert v-if="application.lastError" type="error" variant="tonal" class="mt-3">{{ application.lastError }}</v-alert>
    </v-card>
    <ApplicationRuntimePanel :application="application" />
    <ApplicationLogsPanel :application="application" />
  </div>
</template>

<style scoped>
.detail-stack { display: grid; gap: 14px; }
.detail-card { padding: 16px; }
.min-width-0 { min-width: 0; }
.meta-grid { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 12px; }
.image-panel { display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: 14px; align-items: start; }
.image-actions { display: flex; gap: 8px; flex-wrap: wrap; justify-content: flex-end; }
.digest-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 10px; }
.mono { font-size: 0.8rem; }
@media (max-width: 900px) {
  .meta-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .image-panel { grid-template-columns: 1fr; }
  .image-actions { justify-content: flex-start; }
  .digest-grid { grid-template-columns: 1fr; }
}
</style>
