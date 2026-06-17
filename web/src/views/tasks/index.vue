<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { useRoute } from 'vue-router';
import { useI18n } from '@/i18n';
import TaskLogPanel from '@/components/tasks/TaskLogPanel.vue';
import AppPagination from '@/components/AppPagination.vue';
import PageLoadingState from '@/components/PageLoadingState.vue';
import { tasksApi } from '@/api/tasks';
import { serversApi } from '@/api/servers';
import type { ServerDto, TaskDto, TaskStatus, TaskStepDto } from '@/types/api';
import { groupTasksByOperation } from './_shared/taskOperations';

const route = useRoute();
const TYPE_FILTER_COMMON = '__common';
const TYPE_FILTER_ALL = '__all';
const hiddenByCommonTaskTypes = new Set(['server_connectivity_test', 'metrics_collect']);
const { formatDateTime, formatTime, t, translateTaskStage, translateTaskStatus, translateTaskType } = useI18n();
const supportedTaskTypes = [
  'application_deploy',
  'application_stop',
  'application_restart',
  'application_refresh',
  'application_image_check',
  'application_image_update',
  'application_image_upgrade_selected',
  'application_image_upgrade_all',
  'server_connectivity_test',
  'server_info_collect',
  'server_restart',
  'server_agent_deploy',
  'agent_certificate_reset',
  'server_ufw_enable',
  'server_ufw_install',
  'metrics_collect',
  'package_refresh',
  'package_upgrade_selected',
  'package_upgrade_all',
  'certificate_issue',
  'certificate_renew',
  'certificate_self_signed_renew',
  'key_asset_tls_reissue',
  'key_asset_ssh_regenerate',
  'key_asset_export',
  'key_asset_import',
  'key_asset_sync',
  'container_start',
  'container_stop',
  'container_restart',
  'container_delete',
  'container_refresh',
  'image_pull',
  'image_refresh',
  'image_delete',
  'image_delete_unused',
  'volume_delete',
  'volume_delete_unused',
  'volume_refresh',
  'application_reconcile',
];

const tasks = ref<TaskDto[]>([]);
const steps = ref<TaskStepDto[]>([]);
const servers = ref<ServerDto[]>([]);
const selectedTaskId = ref('');
const selectedOperationId = ref('');
const statusFilter = ref<TaskStatus[]>([]);
const appliedStatusFilter = ref<TaskStatus[]>([]);
const operationFilter = ref('');
const appliedOperationFilter = ref('');
const typeFilter = ref<string[]>([TYPE_FILTER_COMMON]);
const appliedTypeFilter = ref<string[]>([TYPE_FILTER_COMMON]);
const loading = ref(false);
const actionLoading = ref('');
const error = ref('');
const detailsDialog = ref(false);
const page = ref(1);
const pageSize = ref(20);
const total = ref(0);
let timer: number | undefined;
const runnableTaskTypes = new Set(['server_connectivity_test', 'server_info_collect', 'package_refresh', 'certificate_issue']);

const operationGroups = computed(() => groupTasksByOperation(tasks.value));
const selectedTask = computed(() => tasks.value.find((task) => task.id === selectedTaskId.value) ?? null);
const selectedOperation = computed(() => operationGroups.value.find((group) => group.operationId === selectedOperationId.value) ?? null);
const taskTypeFilterItems = computed(() => [
  { title: t('taskCenter.commonTypes'), value: TYPE_FILTER_COMMON },
  { title: t('taskCenter.allTypes'), value: TYPE_FILTER_ALL },
  ...Array.from(new Set([...supportedTaskTypes, ...tasks.value.map((task) => task.type)]))
    .sort((a, b) => translateTaskType(a).localeCompare(translateTaskType(b)))
    .map((value) => ({ title: translateTaskType(value), value })),
]);
const statusFilterItems = computed(() => [
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

function isSpecialTypeFilter(value: string) {
  return value === TYPE_FILTER_COMMON || value === TYPE_FILTER_ALL;
}

function normalizeTypeFilter(values: string[] | string | null | undefined, previous: string[] = []) {
  const selected = Array.isArray(values) ? values : values ? [values] : [];
  const selectedSet = new Set(selected);
  const previousSet = new Set(previous);
  if (selectedSet.has(TYPE_FILTER_ALL) && !previousSet.has(TYPE_FILTER_ALL)) return [TYPE_FILTER_ALL];
  if (selectedSet.has(TYPE_FILTER_COMMON) && !previousSet.has(TYPE_FILTER_COMMON)) return [TYPE_FILTER_COMMON];
  const exactTypes = selected.filter((value) => !isSpecialTypeFilter(value));
  if (exactTypes.length > 0) return Array.from(new Set(exactTypes));
  return [TYPE_FILTER_COMMON];
}

function onTypeFilterChange(values: string[] | string | null) {
  typeFilter.value = normalizeTypeFilter(values, typeFilter.value);
}

function taskTypeApiFilter(values: string[]) {
  const normalized = normalizeTypeFilter(values, values);
  if (normalized.includes(TYPE_FILTER_ALL)) return { includeInternal: true, commonOnly: false, types: [] };
  return {
    includeInternal: false,
    commonOnly: normalized.includes(TYPE_FILTER_COMMON),
    types: normalized.filter((value) => !isSpecialTypeFilter(value)),
  };
}

function typeFilterForLinkedTask(task: TaskDto) {
  return hiddenByCommonTaskTypes.has(task.type) ? [task.type] : [TYPE_FILTER_COMMON];
}

function searchTasks() {
  appliedStatusFilter.value = [...statusFilter.value];
  appliedOperationFilter.value = operationFilter.value.trim();
  appliedTypeFilter.value = normalizeTypeFilter(typeFilter.value, typeFilter.value);
  reloadFirstPage();
}

async function loadTasks() {
  loading.value = true;
  const typeApiFilter = taskTypeApiFilter(appliedTypeFilter.value);
  try {
    const [taskPage, serverRows] = await Promise.all([
      tasksApi.list({
        statuses: appliedStatusFilter.value,
        types: typeApiFilter.types,
        includeInternal: typeApiFilter.includeInternal,
        commonOnly: typeApiFilter.commonOnly,
        operationId: appliedOperationFilter.value,
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

function updateTaskPage(nextPage: number) {
  if (page.value === nextPage) return;
  page.value = nextPage;
  void loadTasks();
}

function updateTaskPageSize(nextPageSize: number) {
  if (pageSize.value === nextPageSize && page.value === 1) return;
  pageSize.value = nextPageSize;
  page.value = 1;
  void loadTasks();
}

function serverName(serverId?: string | null) {
  if (!serverId) return t('taskCenter.noNode');
  return servers.value.find((server) => server.id === serverId)?.name || serverId;
}

function formatTaskType(value?: string) {
  return translateTaskType(value);
}

function taskDisplayTitle(task?: TaskDto | null) {
  if (!task) return t('taskCenter.selectTask');
  return formatTaskType(task.type);
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

function routeTaskId() {
  return typeof route.query.task === 'string' ? route.query.task : '';
}

async function loadRouteTask() {
  const id = routeTaskId();
  if (!id) {
    await loadTasks();
    return;
  }
  try {
    const linkedTask = await tasksApi.get(id);
    operationFilter.value = linkedTask.operationId || '';
    appliedOperationFilter.value = operationFilter.value;
    statusFilter.value = [];
    appliedStatusFilter.value = [];
    typeFilter.value = typeFilterForLinkedTask(linkedTask);
    appliedTypeFilter.value = [...typeFilter.value];
    page.value = 1;
    await loadTasks();
    if (!tasks.value.some((task) => task.id === linkedTask.id)) {
      tasks.value = [linkedTask];
      total.value = 1;
    }
    selectedOperationId.value = linkedTask.operationId || linkedTask.id;
    selectedTaskId.value = linkedTask.id;
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('taskCenter.loadFailed');
    await loadTasks();
  }
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
  if (task.status === 'running') return task.stage ? t('taskCenter.runningStage', { stage: translateTaskStage(task.stage) }) : t('taskCenter.runningPlain');
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
  return runnableTaskTypes.has(task.type) && ['failed', 'failed_retryable', 'blocked'].includes(task.status);
}

function canRunNow(task: TaskDto) {
  return runnableTaskTypes.has(task.type) && ['queued', 'scheduled', 'failed_retryable'].includes(task.status);
}

function clearFilters() {
  statusFilter.value = [];
  appliedStatusFilter.value = [];
  operationFilter.value = '';
  appliedOperationFilter.value = '';
  typeFilter.value = [TYPE_FILTER_COMMON];
  appliedTypeFilter.value = [TYPE_FILTER_COMMON];
  reloadFirstPage();
}

function openDetailsDialog() {
  if (!selectedTaskId.value) return;
  detailsDialog.value = true;
}

function startPolling() {
  if (timer) window.clearInterval(timer);
  void loadRouteTask();
  timer = window.setInterval(loadTasks, 5000);
}

watch([selectedTaskId, detailsDialog], loadSteps);
watch(() => route.query.task, () => {
  void loadRouteTask();
});
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
      <v-text-field v-model="operationFilter" :label="t('taskCenter.operationId')" variant="outlined" density="compact" hide-details clearable @keydown.enter="searchTasks" />
      <v-select
        :model-value="typeFilter"
        :items="taskTypeFilterItems"
        item-title="title"
        item-value="value"
        :label="t('taskCenter.type')"
        :placeholder="t('taskCenter.typePlaceholder')"
        variant="outlined"
        density="compact"
        hide-details
        multiple
        chips
        closable-chips
        clearable
        @update:model-value="onTypeFilterChange"
      />
      <v-select
        v-model="statusFilter"
        :items="statusFilterItems"
        item-title="title"
        item-value="value"
        :label="t('taskCenter.status')"
        :placeholder="t('taskCenter.statusPlaceholder')"
        variant="outlined"
        density="compact"
        hide-details
        multiple
        chips
        closable-chips
        clearable
      />
      <v-btn color="primary" variant="flat" prepend-icon="mdi-magnify" class="text-none" @click="searchTasks">{{ t('taskCenter.search') }}</v-btn>
      <v-btn variant="outlined" prepend-icon="mdi-filter-remove" class="text-none" @click="clearFilters">{{ t('taskCenter.clear') }}</v-btn>
    </v-card>

    <v-alert v-if="error" type="error" variant="tonal">{{ error }}</v-alert>

    <div class="task-workspace">
      <v-card variant="outlined" :loading="loading" class="operation-panel">
        <div class="panel-title">
          <div class="text-subtitle-1 font-weight-bold">{{ t('taskCenter.operations') }}</div>
        </div>
        <v-divider />
        <PageLoadingState v-if="loading && tasks.length === 0" min-height="280px" />
        <v-list v-else lines="three" density="compact" class="operation-list">
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
              <span>{{ formatTaskType(group.tasks[0]?.type) }}</span>
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
        <AppPagination :page="page" :page-size="pageSize" :total="total" @update:page="updateTaskPage" @update:page-size="updateTaskPageSize" />
      </v-card>

      <section class="main-panel">
        <v-card variant="outlined" class="lifecycle-panel">
          <div class="panel-title">
            <div class="text-subtitle-1 font-weight-bold">{{ taskDisplayTitle(selectedTask) }}</div>
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
          <div class="task-table-wrap">
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
          </div>
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
                <strong>{{ translateTaskStage(step.step) }}</strong>
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
.task-center { min-width: 0; flex: 1 1 auto; min-height: 0; }

.task-kpis {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 10px;
}

.filter-bar {
  display: grid;
  grid-template-columns: minmax(220px, 1fr) minmax(220px, 300px) minmax(220px, 300px) auto auto;
  gap: 12px;
  align-items: center;
  padding: 14px;
}

.task-workspace {
  display: grid;
  grid-template-columns: minmax(300px, 0.72fr) minmax(620px, 1.35fr);
  flex: 1 1 auto;
  gap: 16px;
  min-height: 0;
  align-items: stretch;
}

.main-panel {
  display: grid;
  grid-template-rows: auto minmax(0, 1fr);
  gap: 16px;
  min-height: 0;
}

.operation-list {
  flex: 1 1 auto;
  min-height: 0;
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

.operation-panel,
.task-table-panel {
  display: flex;
  flex-direction: column;
  min-height: 0;
  overflow: hidden;
}

.action-row {
  display: flex;
  gap: 10px;
  flex-wrap: wrap;
  padding: 0 16px 16px;
}

.task-table-wrap {
  flex: 1 1 auto;
  min-height: 0;
  overflow: auto;
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

@media (max-width: 980px) {
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

@media (max-width: 760px) {
  .task-center,
  .task-workspace,
  .operation-list,
  .task-table-wrap {
    flex: none;
  }

  .task-workspace,
  .main-panel {
    min-height: auto;
  }

  .operation-panel,
  .task-table-panel {
    overflow: visible;
  }

  .operation-list {
    overflow: visible;
  }

  .task-table-wrap {
    overflow-x: auto;
    overflow-y: visible;
  }
}

@media (max-width: 560px) {
  .selected-progress {
    grid-template-columns: 1fr;
  }

  .action-row .v-btn {
    width: 100%;
  }

}
</style>
