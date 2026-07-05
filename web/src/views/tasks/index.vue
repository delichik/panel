<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue';
import { useRoute } from 'vue-router';
import { useI18n } from '@/i18n';
import TaskLogPanel from '@/components/tasks/TaskLogPanel.vue';
import AppActionButton from '@/components/AppActionButton.vue';
import AppActionGroup from '@/components/AppActionGroup.vue';
import AppMasterDetailWorkspace from '@/components/AppMasterDetailWorkspace.vue';
import AppSelectorItem from '@/components/AppSelectorItem.vue';
import AppSelectorPanel from '@/components/AppSelectorPanel.vue';
import PageLoadingState from '@/components/PageLoadingState.vue';
import { tasksApi } from '@/api/tasks';
import { serversApi } from '@/api/servers';
import type { ServerDto, TaskDeploymentOperationProjectionDto, TaskDeploymentTargetProjectionDto, TaskDto, TaskStatus, TaskStepDto } from '@/types/api';
import { groupTasksByOperation, type TaskOperationGroup } from './_shared/taskOperations';

const route = useRoute();
const TYPE_FILTER_COMMON = '__common';
const TYPE_FILTER_ALL = '__all';
const hiddenByCommonTaskTypes = new Set(['metrics_collect']);
const { formatDateTime, formatTime, t, translateLifecycleStage, translateTaskStage, translateTaskStatus, translateTaskType } = useI18n();
const supportedTaskTypes = [
  'application_target_batch',
  'application_target_apply',
  'application_target_stop',
  'application_target_purge',
  'application_stop',
  'application_restart',
  'application_refresh',
  'application_image_check',
  'application_image_update',
  'application_image_upgrade_selected',
  'application_image_upgrade_all',
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
const statusFilter = ref<TaskStatus[] | null>([]);
const appliedStatusFilter = ref<TaskStatus[]>([]);
const operationFilter = ref<string | null>('');
const appliedOperationFilter = ref('');
const typeFilter = ref<string[]>([TYPE_FILTER_COMMON]);
const appliedTypeFilter = ref<string[]>([TYPE_FILTER_COMMON]);
const loading = ref(false);
const stepsLoading = ref(false);
const actionLoading = ref('');
const error = ref('');
const detailsDialog = ref(false);
const page = ref(1);
const pageSize = ref(20);
const total = ref(0);
let stepsRequestId = 0;

const operationGroups = computed(() => groupTasksByOperation(tasks.value));
const selectedTask = computed(() => tasks.value.find((task) => task.id === selectedTaskId.value) ?? null);
const selectedOperation = computed(() => operationGroups.value.find((group) => group.operationId === selectedOperationId.value) ?? null);
const selectedDeploymentOperation = computed(() => deploymentOperation(selectedOperation.value));
const selectedDeploymentTarget = computed(() => selectedTask.value?.deployment?.target ?? null);
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

function isSpecialTypeFilter(value: string) {
  return value === TYPE_FILTER_COMMON || value === TYPE_FILTER_ALL;
}

function normalizeTypeFilter(values: string[] | string | null | undefined, previous: string[] = []) {
  const selected = Array.isArray(values) ? values : values ? [values] : [];
  const selectedSet = new Set(selected);
  const previousSet = new Set(previous);
  if (selectedSet.has(TYPE_FILTER_ALL) && selectedSet.has(TYPE_FILTER_COMMON)) {
    if (!previousSet.has(TYPE_FILTER_ALL)) return [TYPE_FILTER_ALL];
    if (!previousSet.has(TYPE_FILTER_COMMON)) return [TYPE_FILTER_COMMON];
    return previousSet.has(TYPE_FILTER_ALL) ? [TYPE_FILTER_ALL] : [TYPE_FILTER_COMMON];
  }
  if (selectedSet.has(TYPE_FILTER_ALL)) return [TYPE_FILTER_ALL];
  if (selectedSet.has(TYPE_FILTER_COMMON)) return [TYPE_FILTER_COMMON];
  const exactTypes = selected.filter((value) => !isSpecialTypeFilter(value));
  if (exactTypes.length > 0) return Array.from(new Set(exactTypes));
  return [TYPE_FILTER_COMMON];
}

function onTypeFilterChange(values: string[] | string | null) {
  typeFilter.value = normalizeTypeFilter(values, typeFilter.value);
}

function normalizeStatusFilter(values: TaskStatus[] | TaskStatus | null | undefined) {
  const selected = Array.isArray(values) ? values : values ? [values] : [];
  return Array.from(new Set(selected.filter(Boolean)));
}

function normalizeOperationFilter(value: string | null | undefined) {
  return value?.trim() ?? '';
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
  const normalizedStatus = normalizeStatusFilter(statusFilter.value);
  const normalizedOperation = normalizeOperationFilter(operationFilter.value);
  statusFilter.value = normalizedStatus;
  operationFilter.value = normalizedOperation;
  appliedStatusFilter.value = [...normalizedStatus];
  appliedOperationFilter.value = normalizedOperation;
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
        operationPage: true,
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
  const taskId = selectedTaskId.value;
  const requestId = ++stepsRequestId;
  steps.value = [];
  if (!detailsDialog.value || !selectedTaskId.value) {
    stepsLoading.value = false;
    return;
  }
  stepsLoading.value = true;
  try {
    const result = await tasksApi.steps(taskId);
    if (requestId !== stepsRequestId || selectedTaskId.value !== taskId || !detailsDialog.value) return;
    steps.value = result;
  } catch {
    if (requestId !== stepsRequestId || selectedTaskId.value !== taskId || !detailsDialog.value) return;
    steps.value = [];
  } finally {
    if (requestId === stepsRequestId && selectedTaskId.value === taskId) {
      stepsLoading.value = false;
    }
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

function parseTaskMetadata(task?: TaskDto | null): Record<string, unknown> {
  if (!task?.metadataJson) return {};
  try {
    const parsed = JSON.parse(task.metadataJson);
    return parsed && typeof parsed === 'object' && !Array.isArray(parsed) ? parsed : {};
  } catch {
    return {};
  }
}

function taskMetadataString(task: TaskDto | null | undefined, key: string) {
  const value = parseTaskMetadata(task)[key];
  return typeof value === 'string' ? value : '';
}

function taskMetadataNumber(task: TaskDto | null | undefined, key: string) {
  const value = parseTaskMetadata(task)[key];
  return typeof value === 'number' ? value : null;
}

function deploymentActionLabel(action?: string) {
  if (!action) return t('common.notAvailable');
  return t(`taskCenter.deploymentActions.${action}`);
}

function deploymentContext(task: TaskDto) {
  const operation = task.deployment?.operation;
  const target = task.deployment?.target;
  const appName = target?.applicationName || operation?.applicationName || taskMetadataString(task, 'applicationName');
  const action = target?.action || taskMetadataString(task, 'action');
  const generation = target?.desiredGeneration ?? operation?.generation ?? taskMetadataNumber(task, 'generation');
  const specHash = target?.desiredSpecHash || operation?.specHash || taskMetadataString(task, 'specHash');
  return {
    appName,
    action,
    generation,
    specHash,
    operationId: target?.operationId || operation?.id || taskMetadataString(task, 'lifecycleOperationId'),
    targetId: target?.id || taskMetadataString(task, 'lifecycleTargetId'),
    state: target?.state || '',
    stage: target?.stage || '',
    attempt: target?.attempt ?? 0,
    nextRunAt: target?.nextRunAt || null,
    claimedTaskId: target?.claimedTaskId || '',
    claimedTaskStatus: target?.claimedTaskStatus || '',
    errorCode: target?.errorCode || '',
    errorMessage: target?.errorMessage || '',
    errorDetail: target?.errorDetail || '',
  };
}

function deploymentOperation(group?: TaskOperationGroup | null): TaskDeploymentOperationProjectionDto | null {
  return group?.tasks.find((task) => task.deployment?.operation)?.deployment?.operation ?? null;
}

function deploymentTargetStatusColor(state?: string | null, status?: string | null) {
  const value = state || status || '';
  if (['failed', 'cancelled'].includes(value)) return 'error';
  if (value === 'failed_retryable') return 'warning';
  if (['succeeded', 'running', 'deployed'].includes(value)) return 'success';
  if (['preparing', 'applying', 'stopping', 'purging', 'verifying', 'claimed'].includes(value)) return 'primary';
  if (['ready', 'planned', 'pending'].includes(value)) return 'info';
  if (value === 'superseded') return 'default';
  return 'default';
}

function deploymentStateLabel(value?: string | null) {
  if (!value) return t('common.notAvailable');
  return t(`taskCenter.deploymentTargetStates.${value}`);
}

function deploymentTargetError(target?: TaskDeploymentTargetProjectionDto | null) {
  return target?.errorDetail || target?.errorMessage || target?.errorCode || '';
}

function deploymentTargetReason(target?: TaskDeploymentTargetProjectionDto | null) {
  if (!target) return '';
  const state = target.state || target.status;
  if (target.errorMessage || target.errorCode) return target.errorMessage || target.errorCode || '';
  if (state === 'failed_retryable' && target.nextRunAt) return t('taskCenter.targetRetryAfterBackoff', { time: formatDateTime(target.nextRunAt) });
  if (state === 'ready') return t('taskCenter.waitingServerQueue');
  if (state === 'claimed' && target.claimedTaskId) return t('taskCenter.claimedByTask', { id: shortId(target.claimedTaskId) });
  if (state === 'superseded') return target.errorDetail || t('taskCenter.supersededByNewerOperation');
  return '';
}

function deploymentStageLabel(value?: string | null) {
  return value ? translateLifecycleStage(value) : t('common.notAvailable');
}

function operationObjectLabel(group: TaskOperationGroup) {
  const task = group.tasks.find((item) => item.parentTaskId && taskMetadataString(item, 'applicationName')) ?? group.tasks.find((item) => item.parentTaskId) ?? group.tasks[0];
  const appName = task?.deployment?.operation?.applicationName || task?.deployment?.target?.applicationName || taskMetadataString(task, 'applicationName');
  if (appName) return appName;
  const serverId = task?.nodeId || task?.serverId;
  const server = serverId ? servers.value.find((item) => item.id === serverId) : null;
  if (server) return server.name;

  const resourceType = group.resourceType || group.triggerResourceType;
  const resourceKey = {
    server: 'server',
    application: 'application',
    applications: 'application',
    certificate: 'certificate',
    key_asset: 'keyAsset',
    task_batch: 'taskBatch',
  }[resourceType];
  return t(`taskCenter.resourceTypes.${resourceKey || 'systemTask'}`);
}

function formatTaskType(value?: string) {
  return translateTaskType(value);
}

function taskDisplayTitle(task?: TaskDto | null) {
  if (!task) return t('taskCenter.selectTask');
  return formatTaskType(task.type);
}

function executionModeLabel(value?: string | null) {
  if (!value) return t('common.notAvailable');
  return t(`taskCenter.executionModes.${value}`);
}

function isBatchParentTask(task: TaskDto) {
  return !task.parentTaskId && !!task.childCount && task.childCount > 0;
}

function operationTaskCount(group?: TaskOperationGroup | null) {
  if (!group) return 1;
  const childCount = group.tasks.filter((task) => task.parentTaskId).length;
  return childCount || group.tasks.length;
}

function taskOrdinalLabel(task: TaskDto) {
  if (isBatchParentTask(task)) return t('taskCenter.operationSummary');
  if (!task.parentTaskId || !task.childCount || task.childCount <= 1) return formatTaskType(task.type);
  const context = deploymentContext(task);
  if (context.appName && context.action) {
    return t('taskCenter.deploymentTargetTitle', {
      action: deploymentActionLabel(context.action),
      app: context.appName,
    });
  }
  return t('taskCenter.taskOrdinal', { index: task.childIndex || 1, count: task.childCount });
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
  const targetReason = deploymentTargetReason(task.deployment?.target);
  if (targetReason) return targetReason;
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
  return task.allowRetry && ['failed', 'failed_retryable', 'blocked'].includes(task.status);
}

function canRunNow(task: TaskDto) {
  return task.allowRunNow && ['queued', 'scheduled', 'failed_retryable'].includes(task.status);
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

function loadTaskCenter() {
  void loadRouteTask();
}

watch([selectedTaskId, detailsDialog], loadSteps);
watch(() => route.query.task, () => {
  void loadRouteTask();
});
onMounted(loadTaskCenter);
</script>

<template>
  <div class="task-center page-shell">
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
      <AppActionGroup context="filter" mobile-stack>
        <AppActionButton kind="primary" icon="mdi-magnify" :label="t('taskCenter.search')" @click="searchTasks" />
        <AppActionButton icon="mdi-filter-remove" :label="t('taskCenter.clear')" @click="clearFilters" />
      </AppActionGroup>
    </v-card>

    <v-alert v-if="error" type="error" variant="tonal">{{ error }}</v-alert>

    <AppMasterDetailWorkspace>
      <template #aside>
      <AppSelectorPanel
        class="operation-panel"
        :title="t('taskCenter.operations')"
        :loading="loading"
        :empty="operationGroups.length === 0"
        empty-icon="mdi-progress-clock"
        :empty-text="t('taskCenter.noTaskOperations')"
        :page="page"
        :page-size="pageSize"
        :total="total"
        @update:page="updateTaskPage"
        @update:page-size="updateTaskPageSize"
      >
          <AppSelectorItem
            v-for="group in operationGroups"
            :key="group.operationId"
            :selected="group.operationId === selectedOperationId"
            @select="selectOperation(group.operationId)"
          >
            <span class="operation-item">
              <v-icon :color="taskStatusColor(group.status)" :icon="group.status === 'completed' ? 'mdi-check-circle' : group.failedCount ? 'mdi-alert-circle' : 'mdi-progress-clock'" />
              <span class="operation-item__content">
                <span class="operation-name">{{ formatTaskType(group.tasks[0]?.type) }}</span>
                <span class="operation-context">{{ formatTime(group.createdAt) }} · {{ operationObjectLabel(group) }}</span>
              </span>
              <v-chip :color="taskStatusColor(group.status)" size="x-small" label>{{ statusLabel(group.status) }}</v-chip>
          </span>
        </AppSelectorItem>
      </AppSelectorPanel>
      </template>

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
                <span>{{ t('taskCenter.executionMode') }}</span>
                <strong>{{ executionModeLabel(selectedOperation?.executionMode || selectedTask.executionMode) }}</strong>
              </div>
              <div>
                <span>{{ t('taskCenter.operationTaskCount') }}</span>
                <strong>{{ operationTaskCount(selectedOperation) }}</strong>
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

            <div v-if="selectedDeploymentTarget" class="deployment-diagnostics">
              <div class="deployment-diagnostics__title">
                <span class="text-subtitle-2 font-weight-bold">{{ t('taskCenter.deploymentTargetDiagnostics') }}</span>
                <v-chip :color="deploymentTargetStatusColor(selectedDeploymentTarget.state, selectedDeploymentTarget.status)" size="x-small" label>
                  {{ deploymentStateLabel(selectedDeploymentTarget.state || selectedDeploymentTarget.status) }}
                </v-chip>
              </div>
              <div class="diagnostics-grid">
                <div>
                  <span>{{ t('taskCenter.targetStage') }}</span>
                  <strong>{{ deploymentStageLabel(selectedDeploymentTarget.stage) }}</strong>
                </div>
                <div>
                  <span>{{ t('taskCenter.attempt') }}</span>
                  <strong>{{ selectedDeploymentTarget.attempt || 0 }}</strong>
                </div>
                <div>
                  <span>{{ t('taskCenter.nextRetry') }}</span>
                  <strong>{{ selectedDeploymentTarget.nextRunAt ? formatDateTime(selectedDeploymentTarget.nextRunAt) : t('common.notAvailable') }}</strong>
                </div>
                <div>
                  <span>{{ t('taskCenter.claimedTask') }}</span>
                  <strong>{{ selectedDeploymentTarget.claimedTaskId ? shortId(selectedDeploymentTarget.claimedTaskId) : t('common.notAvailable') }}</strong>
                </div>
                <div>
                  <span>{{ t('taskCenter.errorCode') }}</span>
                  <strong>{{ selectedDeploymentTarget.errorCode || t('common.notAvailable') }}</strong>
                </div>
                <div>
                  <span>{{ t('taskCenter.targetError') }}</span>
                  <strong>{{ deploymentTargetError(selectedDeploymentTarget) || t('common.notAvailable') }}</strong>
                </div>
              </div>
            </div>

            <AppActionGroup context="section" align="start" class="action-row">
              <AppActionButton
                icon="mdi-text-box-search-outline"
                :label="t('taskCenter.stepsAndLogs')"
                @click="openDetailsDialog"
              />
              <AppActionButton
                v-if="canRunNow(selectedTask)"
                icon="mdi-play"
                :label="t('common.runNow')"
                :loading="actionLoading === `run:${selectedTask.id}`"
                @click="runNowTask(selectedTask)"
              />
              <AppActionButton
                v-if="canRetry(selectedTask)"
                icon="mdi-reload"
                :label="t('common.retry')"
                :loading="actionLoading === `retry:${selectedTask.id}`"
                @click="retryTask(selectedTask)"
              />
            </AppActionGroup>
          </template>
        </v-card>

        <v-card v-if="selectedDeploymentOperation" variant="outlined" class="deployment-targets-panel">
          <div class="panel-title">
            <div class="text-subtitle-1 font-weight-bold">{{ t('taskCenter.deploymentTargets') }}</div>
            <v-chip :color="deploymentTargetStatusColor(undefined, selectedDeploymentOperation.status)" size="x-small" label>
              {{ selectedDeploymentOperation.status }}
            </v-chip>
          </div>
          <div class="task-table-wrap">
            <v-table density="compact" class="task-table deployment-targets-table">
              <thead>
                <tr>
                  <th>{{ t('taskCenter.node') }}</th>
                  <th>{{ t('taskCenter.deployment') }}</th>
                  <th>{{ t('taskCenter.targetState') }}</th>
                  <th>{{ t('taskCenter.targetStage') }}</th>
                  <th>{{ t('taskCenter.attempt') }}</th>
                  <th>{{ t('taskCenter.nextRetry') }}</th>
                  <th>{{ t('taskCenter.claimedTask') }}</th>
                  <th>{{ t('taskCenter.targetError') }}</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="target in selectedDeploymentOperation.targets || []" :key="target.id">
                  <td>{{ target.serverName || serverName(target.serverId) }}</td>
                  <td>
                    <div class="font-weight-medium">{{ deploymentActionLabel(target.action) }}</div>
                    <div v-if="target.desiredGeneration" class="text-caption text-medium-emphasis">
                      {{ t('taskCenter.generationShort', { generation: target.desiredGeneration }) }}
                    </div>
                  </td>
                  <td>
                    <v-chip :color="deploymentTargetStatusColor(target.state, target.status)" size="x-small" label>
                      {{ deploymentStateLabel(target.state || target.status) }}
                    </v-chip>
                  </td>
                  <td>{{ deploymentStageLabel(target.stage) }}</td>
                  <td class="font-tabular">{{ target.attempt || 0 }}</td>
                  <td>{{ target.nextRunAt ? formatClock(target.nextRunAt) : t('common.notAvailable') }}</td>
                  <td class="mono">{{ target.claimedTaskId ? shortId(target.claimedTaskId) : t('common.notAvailable') }}</td>
                  <td class="queue-reason">{{ deploymentTargetReason(target) || deploymentTargetError(target) || t('common.notAvailable') }}</td>
                </tr>
                <tr v-if="!(selectedDeploymentOperation.targets?.length)">
                  <td colspan="8" class="text-center py-6 text-medium-emphasis">{{ t('taskCenter.noDeploymentTargets') }}</td>
                </tr>
              </tbody>
            </v-table>
          </div>
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
                  <th>{{ t('taskCenter.deployment') }}</th>
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
                  <td colspan="9" class="text-center py-6 text-medium-emphasis">{{ t('taskCenter.selectOperation') }}</td>
                </tr>
                <tr
                  v-for="task in selectedOperation?.tasks || []"
                  :key="task.id"
                  class="cursor-pointer"
                  :class="{ selected: task.id === selectedTaskId }"
                  @click="selectedTaskId = task.id"
                >
                  <td>
                    <div class="font-weight-medium">{{ taskOrdinalLabel(task) }}</div>
                    <div v-if="task.parentTaskId || isBatchParentTask(task)" class="text-caption text-medium-emphasis">{{ formatTaskType(task.type) }}</div>
                    <div class="text-caption text-medium-emphasis mono">{{ shortId(task.id) }}</div>
                  </td>
                  <td>
                    <div v-if="deploymentContext(task).appName" class="deployment-context">
                      <div class="font-weight-medium">{{ deploymentContext(task).appName }}</div>
                      <div class="text-caption text-medium-emphasis">
                        {{ deploymentActionLabel(deploymentContext(task).action) }}
                        <span v-if="deploymentContext(task).generation !== null"> / {{ t('taskCenter.generationShort', { generation: deploymentContext(task).generation }) }}</span>
                      </div>
                      <div v-if="deploymentContext(task).specHash" class="text-caption text-medium-emphasis mono">
                        {{ t('taskCenter.specHashShort', { hash: shortId(deploymentContext(task).specHash) }) }}
                      </div>
                      <div v-if="deploymentContext(task).state" class="deployment-context__state">
                        <v-chip :color="deploymentTargetStatusColor(deploymentContext(task).state)" size="x-small" label>
                          {{ deploymentStateLabel(deploymentContext(task).state) }}
                        </v-chip>
                        <span v-if="deploymentContext(task).stage" class="text-caption text-medium-emphasis">{{ deploymentStageLabel(deploymentContext(task).stage) }}</span>
                      </div>
                    </div>
                    <span v-else class="text-medium-emphasis">{{ t('common.notAvailable') }}</span>
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

    </AppMasterDetailWorkspace>

    <v-dialog v-model="detailsDialog" width="980">
      <v-card class="app-dialog-card">
        <v-card-title class="app-dialog-title">
          <span class="app-dialog-title-text">{{ t('taskCenter.stepsAndLogs') }}</span>
          <AppActionButton kind="tool" icon="mdi-close" :label="t('common.close')" @click="detailsDialog = false" />
        </v-card-title>
        <v-divider />
        <v-card-text class="app-dialog-body task-details-dialog">
          <PageLoadingState v-if="stepsLoading && steps.length === 0" compact min-height="180px" class="mb-4" />
          <v-timeline v-else-if="steps.length" side="end" density="compact" class="mb-4">
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

.filter-bar {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 240px), 1fr));
  gap: 12px;
  align-items: center;
  flex: 0 0 auto;
  padding: 14px 16px;
  background:
    linear-gradient(90deg, color-mix(in srgb, rgb(var(--v-theme-primary)), transparent 98%), transparent 62%),
    var(--lp-surface) !important;
}

.main-panel {
  display: grid;
  grid-template-rows: auto minmax(220px, 0.55fr) minmax(0, 1fr);
  gap: 14px;
  min-height: 0;
}

.operation-item { display: flex; grid-column: 1 / -1; align-items: center; gap: 10px; min-width: 0; }

.deployment-context {
  min-width: 160px;
  max-width: 260px;
  overflow-wrap: anywhere;
}

.deployment-context__state {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-top: 6px;
  min-width: 0;
}

.operation-item__content {
  display: flex;
  flex: 1 1 auto;
  flex-direction: column;
  min-width: 0;
}

.operation-name {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.operation-context {
  color: var(--lp-text-muted);
  font-size: 0.76rem;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
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
.deployment-targets-panel,
.task-table-panel {
  display: flex;
  flex-direction: column;
  min-height: 0;
  overflow: hidden;
}

.lifecycle-panel,
.deployment-targets-panel,
.task-table-panel {
  background:
    linear-gradient(180deg, color-mix(in srgb, var(--lp-surface-container), transparent 62%), transparent 74%),
    var(--lp-surface) !important;
}

.deployment-targets-panel {
  max-height: 260px;
}

.deployment-diagnostics {
  padding-bottom: 14px;
}

.deployment-diagnostics__title {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 0 16px 10px;
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

@media (max-width: 1080px) {
  .panel-title {
    align-items: stretch;
    flex-direction: column;
  }

  .filter-bar,
  .diagnostics-grid {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 760px) {
  .task-center,
  .task-table-wrap {
    flex: none;
  }

  .main-panel {
    min-height: auto;
  }

  .operation-panel,
  .deployment-targets-panel,
  .task-table-panel {
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
