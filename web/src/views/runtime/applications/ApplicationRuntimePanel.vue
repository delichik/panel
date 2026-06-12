<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue';
import { useI18n } from '@/i18n';
import { applicationsApi } from '@/api/applications';
import type { ApplicationDto, ApplicationRuntimeDto, NomadAllocationDto } from '@/types/api';
import AppPagination from '@/components/AppPagination.vue';
import { usePagination } from '@/composables/usePagination';

const props = defineProps<{ application: ApplicationDto }>();
const emit = defineEmits<{ logs: [{ allocId: string; task: string }] }>();
const { formatDateTime, t, translateNomadAllocationDesiredStatus, translateNomadRuntimeStatus } = useI18n();
const runtime = ref<ApplicationRuntimeDto | null>(null);
const loading = ref(false);
const error = ref('');
const allocations = computed(() => runtime.value?.allocations ?? []);
const evaluations = computed(() => runtime.value?.evaluations ?? []);
const {
  page: allocationPage,
  pageSize: allocationPageSize,
  total: allocationTotal,
  pageItems: pagedAllocations,
} = usePagination(allocations);
const {
  page: evaluationPage,
  pageSize: evaluationPageSize,
  total: evaluationTotal,
  pageItems: pagedEvaluations,
} = usePagination(evaluations);

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

watch(() => [props.application.id, props.application.generation, props.application.lastEvalId], loadRuntime);
onMounted(loadRuntime);

function failedAllocSummary(evaluation: NonNullable<ApplicationRuntimeDto['evaluationDetails']>[number]) {
  const groups = Object.entries(evaluation.FailedTGAllocs ?? {});
  if (groups.length === 0) return '';
  return groups.map(([group, alloc]) => {
    const exhausted = Object.entries(alloc.DimensionExhausted ?? {}).map(([key, count]) => `${key}: ${count}`).join(', ');
    const filtered = Object.entries(alloc.ConstraintFiltered ?? {}).map(([key, count]) => `${key}: ${count}`).join(', ');
    const parts = [
      alloc.NodesEvaluated !== undefined ? t('applicationRuntime.nodesEvaluated', { count: alloc.NodesEvaluated }) : '',
      alloc.NodesFiltered !== undefined ? t('applicationRuntime.filtered', { count: alloc.NodesFiltered }) : '',
      alloc.NodesExhausted !== undefined ? t('applicationRuntime.exhausted', { count: alloc.NodesExhausted }) : '',
      exhausted ? t('applicationRuntime.exhaustedDetails', { value: exhausted }) : '',
      filtered ? t('applicationRuntime.constraintDetails', { value: filtered }) : '',
    ].filter(Boolean);
    return `${group}: ${parts.join(', ')}`;
  }).join('\n');
}

function taskNames(alloc: NomadAllocationDto) {
  return Object.keys(alloc.TaskStates ?? {});
}

function defaultTaskName(alloc: NomadAllocationDto) {
  return taskNames(alloc)[0] || props.application.name;
}

function openLogs(alloc: NomadAllocationDto, task = defaultTaskName(alloc)) {
  if (!alloc.ID || !task) return;
  emit('logs', { allocId: alloc.ID, task });
}
</script>

<template>
  <v-card variant="outlined" :loading="loading" class="runtime-card">
    <div class="d-flex align-center justify-space-between mb-3">
      <div class="text-subtitle-1 font-weight-bold">{{ t('applicationRuntime.runtime') }}</div>
    </div>
    <v-alert v-if="error" type="error" variant="tonal" class="mb-3">{{ error }}</v-alert>
    <div v-if="runtime" class="runtime-stack">
      <div class="d-flex align-center ga-2">
        <v-chip color="primary" size="small" variant="tonal" label>{{ translateNomadRuntimeStatus(runtime.jobStatus) }}</v-chip>
        <span class="text-caption text-medium-emphasis">{{ formatDateTime(runtime.observedAt) }}</span>
      </div>
      <div>
        <div class="text-caption text-medium-emphasis mb-1">{{ t('applicationRuntime.deployment') }}</div>
        <div v-if="runtime.deployment?.StatusDescription" class="text-caption text-medium-emphasis">{{ runtime.deployment.StatusDescription }}</div>
        <div class="text-body-2">{{ runtime.deployment?.ID || t('common.notAvailable') }} / {{ translateNomadRuntimeStatus(runtime.deployment?.Status) }}</div>
      </div>
      <v-alert
        v-for="evaluation in (runtime.evaluationDetails ?? []).filter(item => item.StatusDescription || item.FailedTGAllocs)"
        :key="evaluation.ID"
        type="warning"
        variant="tonal"
        density="compact"
      >
        <div class="font-weight-bold">{{ evaluation.ID || t('applicationRuntime.evaluation') }} / {{ translateNomadRuntimeStatus(evaluation.Status) }}</div>
        <div v-if="evaluation.StatusDescription">{{ evaluation.StatusDescription }}</div>
        <pre v-if="failedAllocSummary(evaluation)" class="failure-pre">{{ failedAllocSummary(evaluation) }}</pre>
      </v-alert>
      <v-table density="compact">
        <thead><tr><th>{{ t('applicationRuntime.allocation') }}</th><th>{{ t('applicationRuntime.node') }}</th><th>{{ t('applicationRuntime.group') }}</th><th>{{ t('applicationRuntime.client') }}</th><th>{{ t('applicationRuntime.desired') }}</th><th class="text-right">{{ t('applicationRuntime.logs') }}</th></tr></thead>
        <tbody>
          <tr v-for="alloc in pagedAllocations" :key="alloc.ID">
            <td class="mono">{{ alloc.ID }}</td><td class="mono">{{ alloc.NodeID }}</td><td>{{ alloc.TaskGroup }}</td><td>{{ translateNomadRuntimeStatus(alloc.ClientStatus) }}</td><td>{{ translateNomadAllocationDesiredStatus(alloc.DesiredStatus) }}</td>
            <td class="text-right">
              <v-menu v-if="taskNames(alloc).length > 1">
                <template #activator="{ props: menuProps }">
                  <v-btn v-bind="menuProps" size="small" icon="mdi-text-box-search-outline" variant="text" :title="t('applicationRuntime.logs')" />
                </template>
                <v-list density="compact">
                  <v-list-item v-for="taskName in taskNames(alloc)" :key="taskName" :title="taskName" @click="openLogs(alloc, taskName)" />
                </v-list>
              </v-menu>
              <v-btn v-else size="small" icon="mdi-text-box-search-outline" variant="text" :title="t('applicationRuntime.logs')" :disabled="!alloc.ID" @click="openLogs(alloc)" />
            </td>
          </tr>
          <tr v-if="runtime.allocations.length === 0"><td colspan="6" class="text-center text-medium-emphasis py-4">{{ t('applicationRuntime.noAllocations') }}</td></tr>
        </tbody>
      </v-table>
      <AppPagination v-model:page="allocationPage" v-model:page-size="allocationPageSize" :total="allocationTotal" />
      <v-table density="compact">
        <thead><tr><th>{{ t('applicationRuntime.evaluationColumn') }}</th><th>{{ t('taskCenter.type') }}</th><th>{{ t('taskCenter.status') }}</th></tr></thead>
        <tbody>
          <tr v-for="evaluation in pagedEvaluations" :key="evaluation.ID">
            <td class="mono">{{ evaluation.ID }}</td><td>{{ evaluation.Type }}</td><td>{{ translateNomadRuntimeStatus(evaluation.Status) }}</td>
          </tr>
          <tr v-if="runtime.evaluations.length === 0"><td colspan="3" class="text-center text-medium-emphasis py-4">{{ t('applicationRuntime.noEvaluations') }}</td></tr>
        </tbody>
      </v-table>
      <AppPagination v-model:page="evaluationPage" v-model:page-size="evaluationPageSize" :total="evaluationTotal" />
    </div>
  </v-card>
</template>

<style scoped>
.runtime-card { padding: 16px; }
.runtime-stack { display: grid; gap: 12px; }
.mono { font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; font-size: 0.78rem; }
.failure-pre { margin: 6px 0 0; white-space: pre-wrap; font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; font-size: 0.76rem; }
</style>
