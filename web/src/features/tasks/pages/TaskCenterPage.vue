<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { formatDateTime, formatTime, t, translateTaskStatus, useI18n } from '@/i18n';
import TaskLogPanel from '@/components/tasks/TaskLogPanel.vue';
import { tasksApi } from '@/api/tasks';
import { serversApi } from '@/api/servers';
import type { ServerDto, TaskDto, TaskStatus, TaskStepDto } from '@/types/api';
import { groupTasksByOperation } from '../taskOperations';

useI18n();

const tasks = ref<TaskDto[]>([]);
const steps = ref<TaskStepDto[]>([]);
const servers = ref<ServerDto[]>([]);
const selectedTaskId = ref('');
const selectedOperationId = ref('');
const statusFilter = ref<TaskStatus | 'all'>('all');
const operationFilter = ref<string | null>('');
const typeFilter = ref<string | null>('');
const loading = ref(false);
const actionLoading = ref('');
const error = ref('');
const detailsDialog = ref(false);
const page = ref(1);
const pageSize = ref(20);
const total = ref(0);
let timer: number | undefined;

const operationGroups = computed(() => groupTasksByOperation(tasks.value));
const selectedTask = computed(() => tasks.value.find((task) => task.id === selectedTaskId.value) ?? null);
const selectedOperation = computed(() => operationGroups.value.find((group) => group.operationId === selectedOperationId.value) ?? null);
const taskTypeOptions = computed(() => Array.from(new Set(tasks.value.map((task) => task.type))).sort());
const totalPages = computed(() => Math.max(1, Math.ceil(total.value / pageSize.value)));
const statusFilterItems = computed(() => [
  { title: t('common.all'), value: 'all' },
  { title: translateTaskStatus('queued'), value: 'queued' },
  { title: translateTaskStatus('scheduled'), value: 'scheduled' },
  { title: translateTaskStatus('running'), value: 'running' },
  { title: translateTaskStatus('completed'), value: 'completed' },
  { title: translateTaskStatus('failed'), value: 'failed' },
  { title: translateTaskStatus('failed_retryable'), value: 'failed_retryable' },
  { title: translateTaskStatus('blocked'), value: 'blocked' },
  { title: translateTaskStatus('cancelled'), value: 'cancelled' },
]);

const taskCounts = computed(() => ({
  active: tasks.value.filter((task) => ['queued', 'scheduled', 'running', 'failed_retryable'].includes(task.status)).length,
  queued: tasks.value.filter((task) => task.status === 'queued' || task.status === 'scheduled').length,
  running: tasks.value.filter((task) => task.status === 'running').length,
  failed: tasks.value.filter((task) => task.status === 'failed' || task.status === 'blocked').length,
  completed: tasks.value.filter((task) => task.status === 'completed').length,
}));

async function loadTasks() {
  loading.value = true;
  try {
    const [taskPage, serverRows] = await Promise.all([
      tasksApi.list({
        status: statusFilter.value,
        type: typeFilter.value,
        operationId: operationFilter.value,
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
    error.value = err instanceof Error ? err.message : t('taskCenter.loadFailed');
  } finally {
    loading.value = false;
  }
}

async function loadSteps() {
  if (!detailsDialog.value || !selectedTaskId.value) {
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
  if (!serverId) return t('taskCenter.noNode');
  return servers.value.find((server) => server.id === serverId)?.name || serverId;
}

function formatTaskType(value?: string) {
  return value ? value.replace(/_/g, ' ') : '-';
}

function shortId(value?: string | null) {
  if (!value) return t('common.notAvailable');
  return value.length > 18 ? `${value.slice(0, 10)}...${value.slice(-6)}` : value;
}

function formatClock(value?: string | null) {
  return value ? formatTime(value) : t('common.notAvailable');
}

function durationBetween(start?: string | null, end?: string | null) {
  if (!start) return t('common.notAvailable');
  const startMs = new Date(start).getTime();
  const endMs = end ? new Date(end).getTime() : Date.now();
  const seconds = Math.max(0, Math.floor((endMs - startMs) / 1000));
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m ${seconds % 60}s`;
  const hours = Math.floor(minutes / 60);
  return `${hours}h ${minutes % 60}m`;
}

function taskProgress(task: TaskDto) {
  if (task.status === 'completed') return 100;
  return Math.round(task.percentage ?? 0);
}

function progressText(task: TaskDto) {
  if (task.status === 'running' && task.percentage === null) return t('taskCenter.progressRunning');
  return `${taskProgress(task)}%`;
}

function taskStatusColor(status: TaskStatus | string) {
  if (status === 'failed' || status === 'blocked') return 'error';
  if (status === 'completed') return 'success';
  if (status === 'running') return 'primary';
  if (status === 'failed_retryable' || status === 'scheduled') return 'warning';
  if (status === 'queued') return 'info';
  return 'default';
}

function statusLabel(status: TaskStatus | string) {
  return translateTaskStatus(status);
}

function queueReason(task: TaskDto) {
  if (task.status === 'scheduled') return task.nextRunAt ? t('taskCenter.waitingScheduledStart') : t('taskCenter.scheduledByPolicy');
  if (task.status === 'failed_retryable') return task.nextRunAt ? t('taskCenter.retryAfterBackoff', { retry: task.retryCount, max: task.maxRetries || '-' }) : t('taskCenter.retryReady');
  if (task.status === 'queued') {
    if (task.nextRunAt) return t('taskCenter.queuedUntilPlannedStart');
    if (task.triggerTaskId) return t('taskCenter.queuedBy', { id: shortId(task.triggerTaskId) });
    return t('taskCenter.waitingWorker');
  }
  if (task.status === 'running') return task.stage ? t('taskCenter.runningStage', { stage: task.stage }) : t('taskCenter.runningPlain');
  if (task.status === 'completed') return t('taskCenter.finishedSuccessfully');
  if (task.status === 'blocked') return task.error || t('taskCenter.manualAttention');
  if (task.status === 'failed') return task.error || t('taskCenter.finishedWithError');
  return t('common.notAvailable');
}

function selectOperation(operationId: string) {
  selectedOperationId.value = operationId;
  const group = operationGroups.value.find((item) => item.operationId === operationId);
  selectedTaskId.value = group?.tasks[0]?.id ?? '';
}

async function retryTask(task: TaskDto) {
  actionLoading.value = `retry:${task.id}`;
  try {
    const updated = await tasksApi.retry(task.id);
    await loadTasks();
    selectedOperationId.value = updated.operationId || updated.id;
    selectedTaskId.value = updated.id;
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('taskCenter.retryFailed');
  } finally {
    actionLoading.value = '';
  }
}

async function runNowTask(task: TaskDto) {
  actionLoading.value = `run:${task.id}`;
  try {
    const updated = await tasksApi.runNow(task.id);
    await loadTasks();
    selectedOperationId.value = updated.operationId || task.operationId || task.id;
    selectedTaskId.value = updated.id;
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('taskCenter.runNowFailed');
  } finally {
    actionLoading.value = '';
  }
}

function canRetry(task: TaskDto) {
  return ['failed', 'failed_retryable', 'blocked'].includes(task.status);
}

function canRunNow(task: TaskDto) {
  return ['queued', 'scheduled', 'failed_retryable'].includes(task.status);
}

function clearFilters() {
  statusFilter.value = 'all';
  operationFilter.value = '';
  typeFilter.value = '';
  reloadFirstPage();
}

function openDetailsDialog() {
  if (!selectedTaskId.value) return;
  detailsDialog.value = true;
}

function startPolling() {
  if (timer) window.clearInterval(timer);
  void loadTasks();
  timer = window.setInterval(loadTasks, 5000);
}

watch([selectedTaskId, detailsDialog], loadSteps);
onMounted(startPolling);
onBeforeUnmount(() => {
  if (timer) window.clearInterval(timer);
});
</script>

<template>
  <div class="task-center page-shell">
    <div class="task-kpis">
      <v-chip color="primary" variant="tonal" label>{{ t('taskCenter.active') }} {{ taskCounts.active }}</v-chip>
      <v-chip color="info" variant="tonal" label>{{ t('taskCenter.queued') }} {{ taskCounts.queued }}</v-chip>
      <v-chip color="warning" variant="tonal" label>{{ t('taskCenter.running') }} {{ taskCounts.running }}</v-chip>
      <v-chip color="error" variant="tonal" label>{{ t('taskCenter.failed') }} {{ taskCounts.failed }}</v-chip>
      <v-chip color="success" variant="tonal" label>{{ t('taskCenter.done') }} {{ taskCounts.completed }}</v-chip>
      </div>

    <v-card variant="outlined" class="filter-bar">
      <v-text-field v-model="operationFilter" :label="t('taskCenter.operationId')" variant="outlined" density="compact" hide-details clearable @keydown.enter="reloadFirstPage" />
      <v-select v-model="typeFilter" :items="taskTypeOptions" :label="t('taskCenter.type')" variant="outlined" density="compact" hide-details clearable @update:model-value="reloadFirstPage" />
      <v-select
        v-model="statusFilter"
        :items="statusFilterItems"
        item-title="title"
        item-value="value"
        :label="t('taskCenter.status')"
        variant="outlined"
        density="compact"
        hide-details
        @update:model-value="reloadFirstPage"
      />
      <v-btn variant="outlined" prepend-icon="mdi-filter-remove" class="text-none" @click="clearFilters">{{ t('taskCenter.clear') }}</v-btn>
    </v-card>

    <v-alert v-if="error" type="error" variant="tonal">{{ error }}</v-alert>

    <div class="task-workspace">
      <v-card variant="outlined" :loading="loading" class="operation-panel">
        <div class="panel-title">
          <div class="text-subtitle-1 font-weight-bold">{{ t('taskCenter.operations') }}</div>
        </div>
        <v-divider />
        <v-list lines="three" density="compact" class="operation-list">
          <v-list-item
            v-for="group in operationGroups"
            :key="group.operationId"
            :active="group.operationId === selectedOperationId"
            rounded="0"
            @click="selectOperation(group.operationId)"
          >
            <template #prepend>
              <v-icon :color="taskStatusColor(group.status)" :icon="group.status === 'completed' ? 'mdi-check-circle' : group.failedCount ? 'mdi-alert-circle' : 'mdi-progress-clock'" />
            </template>
            <v-list-item-title class="operation-title">
              <span>{{ group.summary || formatTaskType(group.tasks[0]?.type) }}</span>
              <v-chip :color="taskStatusColor(group.status)" size="x-small" label>{{ statusLabel(group.status) }}</v-chip>
            </v-list-item-title>
            <v-list-item-subtitle>
              <div class="operation-meta">
                <span class="mono">{{ shortId(group.operationId) }}</span>
                <span>{{ group.resourceType || group.triggerResourceType || t('taskCenter.resource') }} / {{ shortId(group.resourceId || group.triggerResourceId) }}</span>
                <span>{{ group.tasks.length }} {{ group.tasks.length === 1 ? t('taskCenter.taskSingular') : t('taskCenter.taskPlural') }}</span>
              </div>
              <v-progress-linear :model-value="group.progress" :color="taskStatusColor(group.status)" height="6" rounded class="mt-2" />
            </v-list-item-subtitle>
          </v-list-item>
          <v-list-item v-if="operationGroups.length === 0" :title="t('taskCenter.noTaskOperations')" />
        </v-list>
        <v-divider />
        <div class="pager">
          <v-select v-model="pageSize" :items="[10, 20, 50, 100]" density="compact" hide-details variant="outlined" class="page-size" @update:model-value="reloadFirstPage" />
          <v-pagination v-model="page" :length="totalPages" density="compact" total-visible="4" @update:model-value="loadTasks" />
        </div>
      </v-card>

      <section class="main-panel">
        <v-card variant="outlined" class="lifecycle-panel">
          <div class="panel-title">
            <div class="text-subtitle-1 font-weight-bold">{{ selectedTask?.summary || t('taskCenter.selectTask') }}</div>
            <v-chip v-if="selectedTask" :color="taskStatusColor(selectedTask.status)" label>{{ statusLabel(selectedTask.status) }}</v-chip>
          </div>

          <template v-if="selectedTask">
            <div class="selected-progress">
              <v-progress-linear
                :model-value="taskProgress(selectedTask)"
                :indeterminate="selectedTask.status === 'running' && selectedTask.percentage === null"
                :color="taskStatusColor(selectedTask.status)"
                height="16"
                rounded
              />
              <span class="font-tabular">{{ progressText(selectedTask) }}</span>
            </div>

            <div class="diagnostics-grid">
              <div>
                <span>{{ t('taskCenter.created') }}</span>
                <strong>{{ formatDateTime(selectedTask.createdAt) }}</strong>
              </div>
              <div>
                <span>{{ t('taskCenter.shouldStart') }}</span>
                <strong>{{ selectedTask.nextRunAt ? formatDateTime(selectedTask.nextRunAt) : t('taskCenter.now') }}</strong>
              </div>
              <div>
                <span>{{ t('taskCenter.actuallyStarted') }}</span>
                <strong>{{ formatDateTime(selectedTask.startedAt) }}</strong>
              </div>
              <div>
                <span>{{ t('taskCenter.finished') }}</span>
                <strong>{{ formatDateTime(selectedTask.finishedAt) }}</strong>
              </div>
              <div>
                <span>{{ t('taskCenter.queuedFor') }}</span>
                <strong>{{ durationBetween(selectedTask.createdAt, selectedTask.startedAt) }}</strong>
              </div>
              <div>
                <span>{{ t('taskCenter.runtime') }}</span>
                <strong>{{ durationBetween(selectedTask.startedAt, selectedTask.finishedAt) }}</strong>
              </div>
            </div>

            <v-alert :color="taskStatusColor(selectedTask.status)" variant="tonal" density="compact" class="mb-4">
              {{ queueReason(selectedTask) }}
            </v-alert>

            <div class="action-row">
              <v-btn
                size="small"
                variant="outlined"
                prepend-icon="mdi-text-box-search-outline"
                @click="openDetailsDialog"
              >
                {{ t('taskCenter.stepsAndLogs') }}
              </v-btn>
              <v-btn
                v-if="canRunNow(selectedTask)"
                size="small"
                color="primary"
                variant="outlined"
                prepend-icon="mdi-play"
                :loading="actionLoading === `run:${selectedTask.id}`"
                @click="runNowTask(selectedTask)"
              >
                {{ t('common.runNow') }}
              </v-btn>
              <v-btn
                v-if="canRetry(selectedTask)"
                size="small"
                variant="outlined"
                prepend-icon="mdi-reload"
                :loading="actionLoading === `retry:${selectedTask.id}`"
                @click="retryTask(selectedTask)"
              >
                {{ t('common.retry') }}
              </v-btn>
            </div>
          </template>
        </v-card>

        <v-card variant="outlined" class="task-table-panel">
          <div class="panel-title">
            <div class="text-subtitle-1 font-weight-bold">{{ t('taskCenter.tasksInOperation') }}</div>
          </div>
          <v-table density="compact" class="task-table">
            <thead>
              <tr>
                <th>{{ t('taskCenter.task') }}</th>
                <th>{{ t('taskCenter.node') }}</th>
                <th>{{ t('taskCenter.status') }}</th>
                <th>{{ t('taskCenter.progress') }}</th>
                <th>{{ t('taskCenter.shouldStart') }}</th>
                <th>{{ t('taskCenter.actuallyStarted') }}</th>
                <th>{{ t('taskCenter.finished') }}</th>
                <th>{{ t('taskCenter.whyQueuedOrResult') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-if="!(selectedOperation?.tasks.length)">
                <td colspan="8" class="text-center py-6 text-medium-emphasis">{{ t('taskCenter.selectOperation') }}</td>
              </tr>
              <tr
                v-for="task in selectedOperation?.tasks || []"
                :key="task.id"
                class="cursor-pointer"
                :class="{ selected: task.id === selectedTaskId }"
                @click="selectedTaskId = task.id"
              >
                <td>
                  <div class="font-weight-medium">{{ formatTaskType(task.type) }}</div>
                  <div class="text-caption text-medium-emphasis mono">{{ shortId(task.id) }}</div>
                </td>
                <td>{{ serverName(task.nodeId || task.serverId) }}</td>
                <td><v-chip :color="taskStatusColor(task.status)" size="x-small" label>{{ statusLabel(task.status) }}</v-chip></td>
                <td class="progress-cell">
                  <v-progress-linear :model-value="taskProgress(task)" :color="taskStatusColor(task.status)" height="8" rounded />
                  <span class="font-tabular">{{ progressText(task) }}</span>
                </td>
                <td>{{ task.nextRunAt ? formatClock(task.nextRunAt) : t('taskCenter.now') }}</td>
                <td>{{ formatClock(task.startedAt) }}</td>
                <td>{{ formatClock(task.finishedAt) }}</td>
                <td class="queue-reason">{{ queueReason(task) }}</td>
              </tr>
            </tbody>
          </v-table>
        </v-card>
      </section>

    </div>

    <v-dialog v-model="detailsDialog" width="980">
      <v-card class="app-dialog-card">
        <v-card-title class="app-dialog-title">
          <span class="app-dialog-title-text">{{ t('taskCenter.stepsAndLogs') }}</span>
          <v-btn icon="mdi-close" variant="text" @click="detailsDialog = false" />
        </v-card-title>
        <v-divider />
        <v-card-text class="app-dialog-body task-details-dialog">
          <v-timeline v-if="steps.length" side="end" density="compact" class="mb-4">
            <v-timeline-item v-for="step in steps" :key="step.id" :dot-color="taskStatusColor(step.status)" size="small">
              <div class="step-row">
                <strong>{{ step.step }}</strong>
                <span class="font-tabular">{{ Math.round(step.percentage ?? 0) }}%</span>
              </div>
              <div class="text-caption text-medium-emphasis">
                {{ statusLabel(step.status) }} · {{ formatClock(step.startedAt) }} - {{ formatClock(step.finishedAt) }}
              </div>
              <div v-if="step.error" class="text-caption text-error">{{ step.error }}</div>
            </v-timeline-item>
          </v-timeline>
          <div v-else class="text-medium-emphasis mb-4">{{ t('taskCenter.noTaskSteps') }}</div>

          <TaskLogPanel
            v-if="detailsDialog && selectedTaskId"
            :key="selectedTaskId"
            :task-id="selectedTaskId"
            :server-name="serverName(selectedTask?.nodeId || selectedTask?.serverId)"
            @finished="loadTasks"
          />
          <div v-else class="text-center py-8 text-medium-emphasis">{{ t('taskCenter.selectTaskForLogs') }}</div>
        </v-card-text>
      </v-card>
    </v-dialog>
  </div>
</template>

<style scoped>
.task-center { min-width: 0; }

.task-kpis {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 10px;
}

.filter-bar {
  display: grid;
  grid-template-columns: minmax(220px, 1fr) minmax(180px, 260px) minmax(170px, 220px) auto;
  gap: 12px;
  align-items: center;
  padding: 14px;
}

.task-workspace {
  display: grid;
  grid-template-columns: minmax(300px, 0.72fr) minmax(620px, 1.35fr);
  gap: 16px;
  align-items: start;
}

.main-panel {
  display: grid;
  gap: 16px;
}

.operation-list {
  max-height: 620px;
  overflow: auto;
}

.operation-title {
  display: flex;
  justify-content: space-between;
  gap: 10px;
  align-items: center;
}

.operation-title span:first-child {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.operation-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 6px 12px;
}

.pager {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 12px;
  padding: 12px;
}

.page-size {
  max-width: 90px;
}

.selected-progress {
  display: grid;
  grid-template-columns: 1fr auto;
  gap: 12px;
  align-items: center;
  padding: 0 16px 14px;
}

.diagnostics-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 10px;
  padding: 0 16px 14px;
}

.diagnostics-grid div {
  border: 1px solid var(--lp-border);
  border-radius: 8px;
  padding: 10px;
  min-width: 0;
}

.diagnostics-grid span {
  display: block;
  color: var(--lp-text-muted);
  font-size: 12px;
  margin-bottom: 4px;
}

.diagnostics-grid strong {
  display: block;
  font-size: 13px;
  overflow-wrap: anywhere;
}

.lifecycle-panel :deep(.v-alert) {
  margin: 0 16px 14px;
}

.action-row {
  display: flex;
  gap: 10px;
  padding: 0 16px 16px;
}

.task-table {
  overflow-x: auto;
}

.task-table th,
.task-table td {
  white-space: nowrap;
}

.progress-cell {
  min-width: 120px;
}

.progress-cell span {
  display: inline-block;
  margin-top: 4px;
  font-size: 12px;
}

.queue-reason {
  max-width: 260px;
  white-space: normal;
  line-height: 1.35;
}

.cursor-pointer {
  cursor: pointer;
}

tr.selected {
  background: rgba(var(--v-theme-primary), 0.07);
}

.mono {
  font-family: "Cascadia Code", "SFMono-Regular", Consolas, monospace;
}

.font-tabular {
  font-variant-numeric: tabular-nums;
}

.step-row {
  display: flex;
  justify-content: space-between;
  gap: 12px;
}

.task-details-dialog {
  display: grid;
  gap: 12px;
}

@media (max-width: 1280px) {
  .task-workspace {
    grid-template-columns: minmax(280px, 0.8fr) minmax(560px, 1.2fr);
  }
}

@media (max-width: 860px) {
  .panel-title {
    align-items: stretch;
    flex-direction: column;
  }

  .filter-bar,
  .task-workspace,
  .diagnostics-grid {
    grid-template-columns: 1fr;
  }
}
</style>
