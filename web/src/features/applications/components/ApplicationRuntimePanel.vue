<script setup lang="ts">
import { onMounted, ref, watch } from 'vue';
import {
  formatDateTime,
  t,
  translateNomadAllocationDesiredStatus,
  translateNomadRuntimeStatus,
} from '@/i18n';
import { applicationsApi } from '@/api/applications';
import type { ApplicationDto, ApplicationRuntimeDto } from '@/types/api';

const props = defineProps<{ application: ApplicationDto }>();
const runtime = ref<ApplicationRuntimeDto | null>(null);
const loading = ref(false);
const error = ref('');

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
        <thead><tr><th>{{ t('applicationRuntime.allocation') }}</th><th>{{ t('applicationRuntime.node') }}</th><th>{{ t('applicationRuntime.group') }}</th><th>{{ t('applicationRuntime.client') }}</th><th>{{ t('applicationRuntime.desired') }}</th></tr></thead>
        <tbody>
          <tr v-for="alloc in runtime.allocations" :key="alloc.ID">
            <td class="mono">{{ alloc.ID }}</td><td class="mono">{{ alloc.NodeID }}</td><td>{{ alloc.TaskGroup }}</td><td>{{ translateNomadRuntimeStatus(alloc.ClientStatus) }}</td><td>{{ translateNomadAllocationDesiredStatus(alloc.DesiredStatus) }}</td>
          </tr>
          <tr v-if="runtime.allocations.length === 0"><td colspan="5" class="text-center text-medium-emphasis py-4">{{ t('applicationRuntime.noAllocations') }}</td></tr>
        </tbody>
      </v-table>
      <v-table density="compact">
        <thead><tr><th>{{ t('applicationRuntime.evaluationColumn') }}</th><th>{{ t('taskCenter.type') }}</th><th>{{ t('taskCenter.status') }}</th></tr></thead>
        <tbody>
          <tr v-for="evaluation in runtime.evaluations" :key="evaluation.ID">
            <td class="mono">{{ evaluation.ID }}</td><td>{{ evaluation.Type }}</td><td>{{ translateNomadRuntimeStatus(evaluation.Status) }}</td>
          </tr>
          <tr v-if="runtime.evaluations.length === 0"><td colspan="3" class="text-center text-medium-emphasis py-4">{{ t('applicationRuntime.noEvaluations') }}</td></tr>
        </tbody>
      </v-table>
    </div>
  </v-card>
</template>

<style scoped>
.runtime-card { padding: 16px; }
.runtime-stack { display: grid; gap: 12px; }
.mono { font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; font-size: 0.78rem; }
.failure-pre { margin: 6px 0 0; white-space: pre-wrap; font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; font-size: 0.76rem; }
</style>
