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
  <div class="service-detail">
    <v-alert v-if="error" type="error" variant="tonal" class="mb-3">{{ error }}</v-alert>
    <RuntimeStatusPanel :service="service" :runtime="runtime" />

    <div class="detail-grid mt-4">
      <v-card variant="outlined" :loading="loading" class="detail-card">
        <v-card-title class="detail-title">
          <v-icon size="18">mdi-graph-outline</v-icon>
          <span>Dependency Graph</span>
        </v-card-title>
        <v-card-text>
          <div class="text-caption text-medium-emphasis">Depends on</div>
          <div class="chip-row mb-3">
            <v-chip v-for="name in service?.dependencyNames || []" :key="name" size="small" variant="tonal" label>{{ name }}</v-chip>
            <span v-if="!(service?.dependencyNames?.length)" class="text-medium-emphasis">None</span>
          </div>
          <div class="text-caption text-medium-emphasis">Dependents</div>
          <div class="chip-row">
            <v-chip v-for="name in service?.dependentNames || []" :key="name" size="small" variant="tonal" label>{{ name }}</v-chip>
            <span v-if="!(service?.dependentNames?.length)" class="text-medium-emphasis">None</span>
          </div>
        </v-card-text>
      </v-card>

      <v-card variant="outlined" class="detail-card">
        <v-card-title class="detail-title">
          <v-icon size="18">mdi-clipboard-clock-outline</v-icon>
          <span>Recent Tasks</span>
        </v-card-title>
        <v-card-text>
          <v-list density="compact" class="task-list">
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

    <v-card variant="outlined" class="mt-4 logs-card">
      <v-card-title class="d-flex align-center justify-space-between text-subtitle-1 font-weight-bold">
        <span class="detail-title pa-0">
          <v-icon size="18">mdi-console-line</v-icon>
          <span>Live Logs</span>
        </span>
        <v-btn size="small" variant="outlined" prepend-icon="mdi-refresh" class="text-none" :loading="loading" @click="load">Refresh</v-btn>
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

.service-detail {
  min-width: 0;
}

.detail-card {
  min-height: 174px;
}

.detail-title {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  font-size: 0.96rem;
  font-weight: 700;
}

.chip-row {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
  min-height: 28px;
  align-items: center;
}

.task-list {
  background: transparent;
}

.logs-card {
  overflow: hidden;
}

.log-output {
  max-height: 300px;
  overflow: auto;
  white-space: pre-wrap;
  font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
  font-size: 12px;
  line-height: 1.55;
  margin: 0;
  padding: 12px;
  border-radius: 8px;
  background: rgba(var(--v-theme-surface-variant), 0.5);
  color: rgba(var(--v-theme-on-surface), 0.86);
  font-variant-numeric: tabular-nums;
}

@media (max-width: 760px) {
  .detail-grid {
    grid-template-columns: 1fr;
  }
}
</style>
