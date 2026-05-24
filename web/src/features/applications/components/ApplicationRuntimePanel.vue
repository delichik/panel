<script setup lang="ts">
import { onMounted, ref, watch } from 'vue';
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
    error.value = err instanceof Error ? err.message : 'Unable to load runtime';
  } finally {
    loading.value = false;
  }
}

watch(() => props.application.id, loadRuntime);
onMounted(loadRuntime);
</script>

<template>
  <v-card variant="outlined" :loading="loading" class="runtime-card">
    <div class="d-flex align-center justify-space-between mb-3">
      <div class="text-subtitle-1 font-weight-bold">Runtime</div>
      <v-btn size="small" icon="mdi-refresh" variant="text" @click="loadRuntime" />
    </div>
    <v-alert v-if="error" type="error" variant="tonal" class="mb-3">{{ error }}</v-alert>
    <div v-if="runtime" class="runtime-stack">
      <div class="d-flex align-center ga-2">
        <v-chip color="primary" size="small" variant="tonal" label>{{ runtime.jobStatus }}</v-chip>
        <span class="text-caption text-medium-emphasis">{{ runtime.observedAt }}</span>
      </div>
      <div>
        <div class="text-caption text-medium-emphasis mb-1">Deployment</div>
        <div class="text-body-2">{{ runtime.deployment?.ID || '-' }} · {{ runtime.deployment?.Status || 'unknown' }}</div>
      </div>
      <v-table density="compact">
        <thead><tr><th>Allocation</th><th>Node</th><th>Group</th><th>Client</th><th>Desired</th></tr></thead>
        <tbody>
          <tr v-for="alloc in runtime.allocations" :key="alloc.ID">
            <td class="mono">{{ alloc.ID }}</td><td class="mono">{{ alloc.NodeID }}</td><td>{{ alloc.TaskGroup }}</td><td>{{ alloc.ClientStatus }}</td><td>{{ alloc.DesiredStatus }}</td>
          </tr>
          <tr v-if="runtime.allocations.length === 0"><td colspan="5" class="text-center text-medium-emphasis py-4">No allocations</td></tr>
        </tbody>
      </v-table>
      <v-table density="compact">
        <thead><tr><th>Evaluation</th><th>Type</th><th>Status</th></tr></thead>
        <tbody>
          <tr v-for="evaluation in runtime.evaluations" :key="evaluation.ID">
            <td class="mono">{{ evaluation.ID }}</td><td>{{ evaluation.Type }}</td><td>{{ evaluation.Status }}</td>
          </tr>
          <tr v-if="runtime.evaluations.length === 0"><td colspan="3" class="text-center text-medium-emphasis py-4">No evaluations</td></tr>
        </tbody>
      </v-table>
    </div>
  </v-card>
</template>

<style scoped>
.runtime-card { padding: 16px; }
.runtime-stack { display: grid; gap: 12px; }
.mono { font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; font-size: 0.78rem; }
</style>
