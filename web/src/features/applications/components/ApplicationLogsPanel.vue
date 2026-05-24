<script setup lang="ts">
import { computed, ref } from 'vue';
import { applicationsApi } from '@/api/applications';
import type { ApplicationDto } from '@/types/api';

const props = defineProps<{ application: ApplicationDto }>();
const allocId = ref('');
const task = ref(props.application.name);
const tail = ref(200);
const logs = ref('');
const loading = ref(false);
const error = ref('');
const canLoad = computed(() => allocId.value.trim() && task.value.trim());

async function loadLogs() {
  if (!canLoad.value) return;
  loading.value = true;
  try {
    const result = await applicationsApi.logs(props.application.id, { allocId: allocId.value, task: task.value, type: 'stdout', tail: tail.value });
    logs.value = result.logs;
    error.value = '';
  } catch (err) {
    error.value = err instanceof Error ? err.message : 'Unable to load logs';
  } finally {
    loading.value = false;
  }
}
</script>

<template>
  <v-card variant="outlined" class="logs-card">
    <div class="text-subtitle-1 font-weight-bold mb-3">Logs</div>
    <v-alert v-if="error" type="error" variant="tonal" class="mb-3">{{ error }}</v-alert>
    <div class="logs-controls">
      <v-text-field v-model="allocId" label="Allocation ID" density="compact" variant="outlined" hide-details />
      <v-text-field v-model="task" label="Task" density="compact" variant="outlined" hide-details />
      <v-text-field v-model.number="tail" label="Tail" type="number" density="compact" variant="outlined" hide-details />
      <v-btn color="primary" variant="flat" class="text-none" :disabled="!canLoad" :loading="loading" @click="loadLogs">Load</v-btn>
    </div>
    <pre class="log-output">{{ logs || 'Select an allocation and task to load logs.' }}</pre>
  </v-card>
</template>

<style scoped>
.logs-card { padding: 16px; }
.logs-controls { display: grid; grid-template-columns: minmax(0, 1fr) 120px 92px auto; gap: 8px; align-items: center; }
.logs-controls .v-btn { min-height: 40px; }
.log-output { min-height: 120px; max-height: 260px; overflow: auto; margin: 12px 0 0; padding: 12px; border-radius: 8px; background: rgba(var(--v-theme-on-surface), 0.04); font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; font-size: 0.78rem; white-space: pre-wrap; }
@media (max-width: 760px) { .logs-controls { grid-template-columns: 1fr; } }
</style>
