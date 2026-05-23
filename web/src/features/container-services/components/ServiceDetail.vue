<script setup lang="ts">
import { onMounted, ref, watch } from 'vue';
import { containerServicesApi } from '@/api/containerServices';
import type { ContainerServiceDto, ContainerServiceRuntimeDto, TaskDto } from '@/types/api';
import RuntimeStatusPanel from './RuntimeStatusPanel.vue';

const props = defineProps<{ service: ContainerServiceDto | null }>();
const runtime = ref<ContainerServiceRuntimeDto | null>(null);
const logs = ref<string[]>([]);
const tasks = ref<TaskDto[]>([]);
const loading = ref(false);
const error = ref('');

async function load() {
  if (!props.service?.id) {
    runtime.value = null;
    logs.value = [];
    tasks.value = [];
    return;
  }
  loading.value = true;
  try {
    const [runtimeRow, logRows] = await Promise.all([
      containerServicesApi.runtime(props.service.id),
      containerServicesApi.logs(props.service.id, 200),
    ]);
    runtime.value = runtimeRow;
    logs.value = logRows.lines ?? [];
    tasks.value = props.service.lastTask ? [props.service.lastTask] : [];
    error.value = '';
  } catch (err) {
    error.value = err instanceof Error ? err.message : 'Unable to load service detail';
  } finally {
    loading.value = false;
  }
}

watch(() => props.service?.id, load);
onMounted(load);
</script>

<template>
  <div>
    <v-alert v-if="error" type="error" variant="tonal" class="mb-3">{{ error }}</v-alert>
    <RuntimeStatusPanel :service="service" :runtime="runtime" />

    <div class="detail-grid mt-4">
      <v-card variant="outlined" :loading="loading">
        <v-card-title class="text-subtitle-1 font-weight-bold">Dependency Graph</v-card-title>
        <v-card-text>
          <div class="text-caption text-medium-emphasis">Depends on</div>
          <div class="chip-row mb-3">
            <v-chip v-for="name in service?.dependencyNames || []" :key="name" size="small" label>{{ name }}</v-chip>
            <span v-if="!(service?.dependencyNames?.length)" class="text-medium-emphasis">None</span>
          </div>
          <div class="text-caption text-medium-emphasis">Dependents</div>
          <div class="chip-row">
            <v-chip v-for="name in service?.dependentNames || []" :key="name" size="small" label>{{ name }}</v-chip>
            <span v-if="!(service?.dependentNames?.length)" class="text-medium-emphasis">None</span>
          </div>
        </v-card-text>
      </v-card>

      <v-card variant="outlined">
        <v-card-title class="text-subtitle-1 font-weight-bold">Recent Tasks</v-card-title>
        <v-card-text>
          <v-list density="compact">
            <v-list-item
              v-for="task in tasks"
              :key="task.id"
              :title="task.summary || task.type"
              :subtitle="`${task.status} - ${task.stage || 'queued'} - ${task.operationId || task.id}`"
            />
            <v-list-item v-if="tasks.length === 0" title="No recent task data" />
          </v-list>
        </v-card-text>
      </v-card>
    </div>

    <v-card variant="outlined" class="mt-4">
      <v-card-title class="d-flex align-center justify-space-between text-subtitle-1 font-weight-bold">
        <span>Live Logs</span>
        <v-btn size="small" variant="outlined" prepend-icon="mdi-refresh" :loading="loading" @click="load">Refresh</v-btn>
      </v-card-title>
      <v-card-text>
        <pre class="log-output">{{ logs.join('\n') || 'No logs returned' }}</pre>
      </v-card-text>
    </v-card>
  </div>
</template>

<style scoped>
.detail-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 16px;
}

.chip-row {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
}

.log-output {
  max-height: 260px;
  overflow: auto;
  white-space: pre-wrap;
  font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
  font-size: 12px;
}
</style>
