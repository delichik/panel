<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import TaskLogPanel from '@/components/tasks/TaskLogPanel.vue';
import { tasksApi } from '@/api/tasks';
import { serversApi } from '@/api/servers';
import type { ServerDto, TaskDto, TaskStatus, TaskStepDto } from '@/types/api';
import { groupTasksByOperation } from '../taskOperations';

const tasks = ref<TaskDto[]>([]);
const steps = ref<TaskStepDto[]>([]);
const servers = ref<ServerDto[]>([]);
const selectedTaskId = ref('');
const selectedOperationId = ref('');
const statusFilter = ref<TaskStatus | 'all'>('all');
const operationFilter = ref('');
const typeFilter = ref('');
const loading = ref(false);
const error = ref('');
const page = ref(1);
const pageSize = ref(20);
const total = ref(0);
let timer: number | undefined;

const operationGroups = computed(() => groupTasksByOperation(tasks.value));
const selectedTask = computed(() => tasks.value.find((task) => task.id === selectedTaskId.value) ?? null);
const selectedOperation = computed(() => operationGroups.value.find((group) => group.operationId === selectedOperationId.value) ?? null);
const taskTypeOptions = computed(() => Array.from(new Set(tasks.value.map((task) => task.type))).sort());
const totalPages = computed(() => Math.max(1, Math.ceil(total.value / pageSize.value)));

async function loadTasks() {
  loading.value = true;
  try {
    const [taskPage, serverRows] = await Promise.all([
      tasksApi.list({
        status: statusFilter.value,
        type: typeFilter.value.trim(),
        operationId: operationFilter.value.trim(),
        page: page.value,
        pageSize: pageSize.value,
      }),
      serversApi.listServers(),
    ]);
    tasks.value = taskPage.items ?? [];
    total.value = taskPage.total ?? 0;
    servers.value = serverRows ?? [];
    if (!selectedOperationId.value && operationGroups.value.length) selectedOperationId.value = operationGroups.value[0].operationId;
    if (selectedOperationId.value && !operationGroups.value.some((group) => group.operationId === selectedOperationId.value)) {
      selectedOperationId.value = operationGroups.value[0]?.operationId ?? '';
    }
    if (!selectedTaskId.value && selectedOperation.value?.tasks.length) selectedTaskId.value = selectedOperation.value.tasks[0].id;
    if (selectedTaskId.value && !tasks.value.some((task) => task.id === selectedTaskId.value)) {
      selectedTaskId.value = selectedOperation.value?.tasks[0]?.id ?? tasks.value[0]?.id ?? '';
    }
    error.value = '';
  } catch (err) {
    error.value = err instanceof Error ? err.message : 'Unable to load tasks';
  } finally {
    loading.value = false;
  }
}

async function loadSteps() {
  if (!selectedTaskId.value) {
    steps.value = [];
    return;
  }
  try {
    steps.value = await tasksApi.steps(selectedTaskId.value);
  } catch {
    steps.value = [];
  }
}

function reloadFirstPage() {
  page.value = 1;
  void loadTasks();
}

function serverName(serverId?: string | null) {
  if (!serverId) return 'No node';
  return servers.value.find((server) => server.id === serverId)?.name || serverId;
}

function formatTaskType(value: string) {
  return value.replace(/_/g, ' ');
}

function taskStatusColor(status: TaskStatus | string) {
  if (status === 'failed' || status === 'blocked') return 'error';
  if (status === 'completed') return 'success';
  if (status === 'running') return 'primary';
  if (status === 'failed_retryable' || status === 'scheduled') return 'warning';
  return 'info';
}

function selectOperation(operationId: string) {
  selectedOperationId.value = operationId;
  selectedTaskId.value = selectedOperation.value?.tasks[0]?.id ?? '';
}

async function retryTask(task: TaskDto) {
  try {
    const updated = await tasksApi.retry(task.id);
    const idx = tasks.value.findIndex((item) => item.id === updated.id);
    if (idx >= 0) tasks.value[idx] = updated;
    selectedTaskId.value = updated.id;
  } catch (err) {
    error.value = err instanceof Error ? err.message : 'Unable to retry task';
  }
}

function canRetry(task: TaskDto) {
  return ['failed', 'failed_retryable', 'blocked'].includes(task.status);
}

function clearFilters() {
  statusFilter.value = 'all';
  operationFilter.value = '';
  typeFilter.value = '';
  reloadFirstPage();
}

function startPolling() {
  if (timer) window.clearInterval(timer);
  void loadTasks();
  timer = window.setInterval(loadTasks, 5000);
}

watch(selectedTaskId, loadSteps);
onMounted(startPolling);
onBeforeUnmount(() => {
  if (timer) window.clearInterval(timer);
});
</script>

<template>
  <div>
    <div class="d-flex justify-space-between align-center mb-6">
      <div>
        <h1 class="text-h4 font-weight-bold">Task Center</h1>
        <p class="text-subtitle-1 text-medium-emphasis">Operations grouped by operation_id with trigger metadata, steps, and logs.</p>
      </div>
      <v-btn prepend-icon="mdi-refresh" :loading="loading" color="primary" class="text-none font-weight-bold" @click="loadTasks">Refresh</v-btn>
    </div>

    <v-card variant="outlined" class="pa-4 mb-5">
      <div class="d-flex flex-wrap align-center" style="gap: 12px;">
        <v-text-field v-model="operationFilter" label="Operation ID" variant="outlined" density="compact" hide-details style="max-width: 240px;" @keydown.enter="reloadFirstPage" />
        <v-select v-model="typeFilter" :items="taskTypeOptions" label="Type" variant="outlined" density="compact" hide-details clearable style="max-width: 220px;" @update:model-value="reloadFirstPage" />
        <v-select
          v-model="statusFilter"
          :items="[
            { title: 'All', value: 'all' },
            { title: 'Queued', value: 'queued' },
            { title: 'Scheduled', value: 'scheduled' },
            { title: 'Running', value: 'running' },
            { title: 'Completed', value: 'completed' },
            { title: 'Failed', value: 'failed' },
            { title: 'Retrying', value: 'failed_retryable' },
            { title: 'Blocked', value: 'blocked' },
            { title: 'Cancelled', value: 'cancelled' }
          ]"
          item-title="title"
          item-value="value"
          label="Status"
          variant="outlined"
          density="compact"
          hide-details
          style="max-width: 170px;"
          @update:model-value="reloadFirstPage"
        />
        <v-btn variant="outlined" class="text-none" @click="clearFilters">Clear</v-btn>
      </div>
    </v-card>

    <v-alert v-if="error" type="error" variant="tonal" class="mb-4">{{ error }}</v-alert>

    <div class="task-layout">
      <v-card variant="outlined" :loading="loading" class="operation-list">
        <v-list lines="three" density="compact">
          <v-list-item
            v-for="group in operationGroups"
            :key="group.operationId"
            :active="group.operationId === selectedOperationId"
            @click="selectOperation(group.operationId)"
          >
            <template #prepend>
              <v-chip :color="taskStatusColor(group.status)" size="small" label>{{ group.status }}</v-chip>
            </template>
            <v-list-item-title class="font-weight-bold">{{ group.operationId }}</v-list-item-title>
            <v-list-item-subtitle>
              trigger={{ group.triggerType || '-' }} resource={{ group.triggerResourceType || '-' }} tasks={{ group.tasks.length }}
            </v-list-item-subtitle>
          </v-list-item>
          <v-list-item v-if="operationGroups.length === 0" title="No task operations" />
        </v-list>
        <v-divider />
        <div class="d-flex align-center justify-end pa-3" style="gap: 12px;">
          <v-select v-model="pageSize" :items="[10, 20, 50, 100]" density="compact" hide-details variant="outlined" style="width: 90px;" @update:model-value="reloadFirstPage" />
          <v-pagination v-model="page" :length="totalPages" density="compact" total-visible="4" @update:model-value="loadTasks" />
        </div>
      </v-card>

      <v-card variant="outlined" class="task-list">
        <v-card-title class="text-subtitle-1 font-weight-bold">Tasks</v-card-title>
        <v-table density="compact">
          <thead>
            <tr>
              <th>Type</th>
              <th>Node</th>
              <th>Status</th>
              <th>Trigger</th>
              <th>Progress</th>
              <th class="text-right">Action</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="!(selectedOperation?.tasks.length)">
              <td colspan="6" class="text-center py-6 text-medium-emphasis">Select an operation</td>
            </tr>
            <tr
              v-for="task in selectedOperation?.tasks || []"
              :key="task.id"
              class="cursor-pointer"
              :class="{ selected: task.id === selectedTaskId }"
              @click="selectedTaskId = task.id"
            >
              <td>{{ formatTaskType(task.type) }}</td>
              <td>{{ serverName(task.nodeId || task.serverId) }}</td>
              <td><v-chip :color="taskStatusColor(task.status)" size="x-small" label>{{ task.status }}</v-chip></td>
              <td class="text-caption">{{ task.triggerType || '-' }}</td>
              <td class="font-tabular">{{ task.percentage ?? 0 }}%</td>
              <td class="text-right">
                <v-btn v-if="canRetry(task)" size="x-small" variant="outlined" prepend-icon="mdi-reload" @click.stop="retryTask(task)">Retry</v-btn>
              </td>
            </tr>
          </tbody>
        </v-table>
      </v-card>

      <v-card variant="outlined" class="task-detail">
        <v-card-title class="text-subtitle-1 font-weight-bold">Steps & Logs</v-card-title>
        <v-card-text>
          <div v-if="selectedTask" class="mb-4">
            <div class="text-caption text-medium-emphasis">Resource</div>
            <div class="font-weight-bold">{{ selectedTask.resourceType || '-' }} / {{ selectedTask.resourceId || '-' }}</div>
            <div class="text-caption text-medium-emphasis mt-2">Trigger</div>
            <div>{{ selectedTask.triggerType || '-' }} {{ selectedTask.triggeredBy ? `by ${selectedTask.triggeredBy}` : '' }}</div>
          </div>

          <v-timeline v-if="steps.length" side="end" density="compact" class="mb-4">
            <v-timeline-item v-for="step in steps" :key="step.id" :dot-color="taskStatusColor(step.status)" size="small">
              <div class="font-weight-bold">{{ step.step }}</div>
              <div class="text-caption text-medium-emphasis">{{ step.status }} - {{ step.percentage ?? 0 }}%</div>
              <div v-if="step.error" class="text-caption text-error">{{ step.error }}</div>
            </v-timeline-item>
          </v-timeline>
          <div v-else class="text-medium-emphasis mb-4">No task steps returned</div>

          <TaskLogPanel v-if="selectedTaskId" :key="selectedTaskId" :task-id="selectedTaskId" :server-name="serverName(selectedTask?.nodeId || selectedTask?.serverId)" @finished="loadTasks" />
          <div v-else class="text-center py-8 text-medium-emphasis">Select a task to view logs</div>
        </v-card-text>
      </v-card>
    </div>
  </div>
</template>

<style scoped>
.task-layout {
  display: grid;
  grid-template-columns: minmax(320px, 0.8fr) minmax(480px, 1.1fr);
  grid-template-areas:
    "operations tasks"
    "operations detail";
  gap: 16px;
  align-items: start;
}

.operation-list {
  grid-area: operations;
}

.task-list {
  grid-area: tasks;
}

.task-detail {
  grid-area: detail;
}

.cursor-pointer {
  cursor: pointer;
}

tr.selected {
  background: rgba(var(--v-theme-primary), 0.06);
}
</style>
