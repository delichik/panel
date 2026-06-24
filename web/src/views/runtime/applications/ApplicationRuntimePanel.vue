<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue';
import { useI18n } from '@/i18n';
import { applicationsApi } from '@/api/applications';
import type { ApplicationDto, ApplicationRuntimeDto, ApplicationRuntimeInstanceDto } from '@/types/api';
import AppPagination from '@/components/AppPagination.vue';
import { usePagination } from '@/composables/usePagination';

const props = withDefaults(defineProps<{ application: ApplicationDto; embedded?: boolean }>(), {
  embedded: false,
});
const emit = defineEmits<{ logs: [{ instanceId: string; containerName: string }] }>();
const { formatDateTime, t, translateLifecycleStage, translateRuntimeDesiredState, translateRuntimeStatus } = useI18n();
const runtime = ref<ApplicationRuntimeDto | null>(null);
const loading = ref(false);
const error = ref('');
const instances = computed(() => runtime.value?.instances ?? []);
const failedInstances = computed(() => instances.value.filter((instance) => instance.lastError));
const {
  page: instancePage,
  pageSize: instancePageSize,
  total: instanceTotal,
  pageItems: pagedInstances,
} = usePagination(instances);

async function loadRuntime() {
  loading.value = true;
  try {
    runtime.value = await applicationsApi.runtime(props.application.id);
    error.value = '';
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('applicationRuntime.loadFailed');
  } finally {
    loading.value = false;
  }
}

watch(() => [props.application.id, props.application.generation], loadRuntime);
onMounted(loadRuntime);

function statusColor(status?: string | null) {
  switch (status) {
    case 'running':
    case 'deployed':
      return 'success';
    case 'failed':
    case 'partially_deployed':
      return 'error';
    case 'deploying':
    case 'pending':
      return 'warning';
    case 'stopped':
      return 'secondary';
    default:
      return 'primary';
  }
}

function openLogs(instance: ApplicationRuntimeInstanceDto) {
  if (!instance.instanceId) return;
  emit('logs', { instanceId: instance.instanceId, containerName: instance.containerName });
}
</script>

<template>
  <component :is="embedded ? 'section' : 'v-card'" :variant="embedded ? undefined : 'outlined'" :loading="embedded ? undefined : loading" class="runtime-card" :class="{ 'runtime-card--embedded': embedded }">
    <div class="runtime-header">
      <div class="text-subtitle-1 font-weight-bold">{{ t('applicationRuntime.runtime') }}</div>
      <v-btn size="small" variant="text" icon="mdi-refresh" :title="t('common.refresh')" :loading="loading" @click="loadRuntime" />
    </div>
    <v-alert v-if="error" type="error" variant="tonal" class="mb-3">{{ error }}</v-alert>
    <div v-if="runtime" class="runtime-stack">
      <div class="runtime-summary">
        <v-chip :color="statusColor(runtime.status)" size="small" variant="tonal" label>{{ translateRuntimeStatus(runtime.status) }}</v-chip>
        <span class="text-caption text-medium-emphasis">{{ formatDateTime(runtime.observedAt) }}</span>
        <span class="text-caption text-medium-emphasis mono">{{ runtime.runtimeId || t('common.notAvailable') }}</span>
      </div>
      <div v-if="runtime.operation" class="runtime-summary">
        <span class="text-caption text-medium-emphasis">{{ t('applicationRuntime.latestOperation') }}</span>
        <v-chip :color="statusColor(runtime.operation.status)" size="small" variant="tonal" label>{{ translateRuntimeStatus(runtime.operation.status) }}</v-chip>
        <span class="text-caption text-medium-emphasis mono">{{ runtime.operation.id }}</span>
      </div>

      <v-alert
        v-for="instance in failedInstances"
        :key="instance.instanceId"
        type="warning"
        variant="tonal"
        density="compact"
      >
        <div class="font-weight-bold">{{ instance.containerName || instance.instanceId }}</div>
        <div>{{ instance.lastError }}</div>
      </v-alert>

      <v-table density="compact">
        <thead>
          <tr>
            <th>{{ t('applicationRuntime.instance') }}</th>
            <th>{{ t('applicationRuntime.server') }}</th>
            <th>{{ t('applicationRuntime.container') }}</th>
            <th>{{ t('common.status') }}</th>
            <th>{{ t('applicationRuntime.desired') }}</th>
            <th>{{ t('applicationRuntime.stage') }}</th>
            <th>{{ t('applicationRuntime.image') }}</th>
            <th class="text-right">{{ t('applicationRuntime.logs') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="instance in pagedInstances" :key="instance.instanceId">
            <td class="mono">{{ instance.instanceId }}</td>
            <td>
              <div class="font-weight-medium">{{ instance.serverName || instance.serverId }}</div>
              <div v-if="instance.serverName" class="text-caption text-medium-emphasis mono">{{ instance.serverId }}</div>
            </td>
            <td class="mono">{{ instance.containerName }}</td>
            <td>
              <v-chip :color="statusColor(instance.status)" size="small" variant="tonal" label>
                {{ translateRuntimeStatus(instance.status) }}
              </v-chip>
            </td>
            <td>{{ translateRuntimeDesiredState(instance.desiredState) }}</td>
            <td>{{ instance.stage ? translateLifecycleStage(instance.stage) : t('common.notAvailable') }}</td>
            <td class="text-truncate image-cell">{{ instance.image || t('common.notAvailable') }}</td>
            <td class="text-right">
              <v-btn size="small" icon="mdi-text-box-search-outline" variant="text" :title="t('applicationRuntime.logs')" :disabled="!instance.instanceId || !instance.containerName" @click="openLogs(instance)" />
            </td>
          </tr>
          <tr v-if="instances.length === 0">
            <td colspan="8" class="text-center text-medium-emphasis py-4">{{ t('applicationRuntime.noInstances') }}</td>
          </tr>
        </tbody>
      </v-table>
      <AppPagination v-model:page="instancePage" v-model:page-size="instancePageSize" :total="instanceTotal" />
    </div>
  </component>
</template>

<style scoped>
.runtime-card { padding: 16px; }
.runtime-card--embedded { padding: 0; }
.runtime-header { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-bottom: 12px; }
.runtime-stack { display: grid; gap: 12px; }
.runtime-summary { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; min-width: 0; }
.mono { font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; font-size: 0.78rem; }
.image-cell { max-width: 260px; }
</style>
