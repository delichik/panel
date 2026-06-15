<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import { useI18n } from '@/i18n';
import { applicationsApi } from '@/api/applications';
import type { ApplicationDto } from '@/types/api';

const props = defineProps<{ application: ApplicationDto; instanceId?: string; containerName?: string }>();
const { t } = useI18n();
const instanceId = ref(props.instanceId || '');
const containerName = ref(props.containerName || props.application.name);
const tail = ref(200);
const logs = ref('');
const loading = ref(false);
const error = ref('');
const canLoad = computed(() => instanceId.value.trim());

async function loadLogs() {
  if (!canLoad.value) return;
  loading.value = true;
  try {
    const result = await applicationsApi.logs(props.application.id, { instanceId: instanceId.value, containerName: containerName.value, type: 'stdout', tail: tail.value });
    logs.value = result.logs;
    error.value = '';
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('applicationLogs.loadFailed');
  } finally {
    loading.value = false;
  }
}

watch(() => props.application.id, () => {
  instanceId.value = props.instanceId || '';
  containerName.value = props.containerName || props.application.name;
  logs.value = '';
  error.value = '';
});

watch(() => [props.instanceId, props.containerName], () => {
  if (props.instanceId) instanceId.value = props.instanceId;
  if (props.containerName) containerName.value = props.containerName;
  logs.value = '';
  if (canLoad.value) void loadLogs();
});
</script>

<template>
  <v-card variant="outlined" class="logs-card">
    <div class="text-subtitle-1 font-weight-bold mb-3">{{ t('applicationLogs.logs') }}</div>
    <v-alert v-if="error" type="error" variant="tonal" class="mb-3">{{ error }}</v-alert>
    <v-alert v-if="instanceId" type="info" variant="tonal" density="compact" class="mb-3">
      {{ t('applicationLogs.selectedTarget', { instance: instanceId, container: containerName || '-' }) }}
    </v-alert>
    <div class="logs-controls">
      <v-text-field v-model="instanceId" :label="t('applicationLogs.instanceId')" density="compact" variant="outlined" hide-details />
      <v-text-field v-model="containerName" :label="t('applicationLogs.container')" density="compact" variant="outlined" hide-details />
      <v-text-field v-model.number="tail" :label="t('applicationLogs.tail')" type="number" density="compact" variant="outlined" hide-details />
      <v-btn color="primary" variant="flat" class="text-none" :disabled="!canLoad" :loading="loading" @click="loadLogs">{{ t('common.load') }}</v-btn>
    </div>
    <pre class="log-output">{{ logs || t('applicationLogs.empty') }}</pre>
  </v-card>
</template>

<style scoped>
.logs-card { padding: 16px; }
.logs-controls { display: grid; grid-template-columns: minmax(0, 1fr) 120px 92px auto; gap: 8px; align-items: center; }
.logs-controls .v-btn { min-height: 40px; }
.log-output { min-height: 120px; max-height: 260px; overflow: auto; margin: 12px 0 0; padding: 12px; border: 1px solid var(--lp-border); border-radius: 8px; background: var(--lp-log-background); color: var(--lp-log-text); font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; font-size: 0.78rem; white-space: pre-wrap; }
@media (max-width: 760px) { .logs-controls { grid-template-columns: 1fr; } }
</style>
