<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue';
import { Activity, Database, Pause, Play, RefreshCcw, Trash2 } from '@/icons';
import { debugApi } from '@/api/debug';
import Badge from '@/components/ui/Badge.vue';
import Button from '@/components/ui/Button.vue';
import ConfirmDialog from '@/components/ui/ConfirmDialog.vue';
import EmptyState from '@/components/ui/EmptyState.vue';
import Skeleton from '@/components/ui/Skeleton.vue';
import Switch from '@/components/ui/Switch.vue';
import Table from '@/components/ui/Table.vue';
import Tabs from '@/components/ui/Tabs.vue';
import { useErrorDialog } from '@/components/ui/errorDialog';
import { useSuccessToast } from '@/components/ui/toast';
import ConsolePage from '@/components/templates/ConsolePage.vue';
import { useI18n } from '@/i18n';
import type { DebugDatabase, DebugDatabaseSnapshots, DebugPprofStatus, DebugRuntimeSnapshot, DebugTaskDefinition, DebugTaskSnapshot } from '@/types/debug';
import { formatDateTime } from '@/utils/datetime';
import { useRuntimeCleanup } from './useRuntimeCleanup';

const { t } = useI18n();
const notifyError = useErrorDialog();
const notifySuccess = useSuccessToast();

const runtimeSnapshot = ref<DebugRuntimeSnapshot | null>(null);
const taskSnapshot = ref<DebugTaskSnapshot | null>(null);
const databaseSnapshots = ref<DebugDatabaseSnapshots | null>(null);
const activeTab = ref('runtime');
const runtimeLoading = ref(false);
const tasksLoading = ref(false);
const databasesLoading = ref(false);
const paused = ref(false);
const runtimeError = ref('');
const tasksError = ref('');
const databasesError = ref('');
let timer: number | undefined;
let runtimeRequestId = 0;
let tasksRequestId = 0;
let databasesRequestId = 0;
const pprof = ref<DebugPprofStatus | null>(null);
const pprofError = ref('');
const pprofPending = ref(false);
const clearOpen = ref(false);
const {
  status: clearStatus, checking: clearChecking, submitting: clearSubmitting,
  running: clearRunning, canStart: canClear, queryFailed: clearQueryFailed,
  resultMissing: clearResultMissing, refresh: refreshClearStatus, start: startClear,
} = useRuntimeCleanup({
  onTerminal(result) {
    if (result.status === 'succeeded' && result.cleared) notifySuccess(t('debugPage.clearRuntimeDataSucceeded'));
    else notifyError(t(result.cleared ? 'debugPage.cleanup.resumeFailed' : 'debugPage.clearRuntimeDataFailed'));
    void loadAll();
  },
  onRequestError(error) {
    notifyError(t('debugPage.cleanup.requestUnconfirmed'), error);
  },
});
const clearStateLabel = computed(() => {
  if (clearQueryFailed.value || clearResultMissing.value) return t('debugPage.cleanup.unconfirmed');
  if (!clearStatus.value) return t('debugPage.cleanup.checking');
  return t(`debugPage.cleanup.status.${clearStatus.value.status}`);
});
const clearTone = computed(() => {
  if (clearQueryFailed.value || clearResultMissing.value || clearRunning.value) return 'warning';
  if (clearStatus.value?.status === 'failed') return 'danger';
  if (clearStatus.value?.status === 'succeeded') return 'success';
  return 'neutral';
});
const clearFailureHint = computed(() => {
  if (clearStatus.value?.cleared) return t('debugPage.cleanup.resumeFailed');
  if (clearStatus.value?.failedStage === 'stopping_workers') return t('debugPage.cleanup.pauseFailed');
  return t('debugPage.cleanup.partialFailure');
});
function clearStageLabel(stage?: string) {
  const stages = ['stopping_workers', 'clearing_coordination', 'clearing_logs', 'clearing_metrics', 'compacting', 'resuming_workers', 'completed'];
  return stage && stages.includes(stage) ? t(`debugPage.cleanup.stage.${stage}`) : t('common.notAvailable');
}

type TaskMetricRow = { key: string; label: string; value: string };
type TaskDefinitionRow = {
  type: string;
  kind: string;
  actions: string;
  concurrencyPolicy: string;
  defaultMaxRetries: number;
  staleQueuedAfter: string;
};

const taskMetricOrder = ['eventRunnerRunning', 'registeredEventTypes', 'executableEventTypes', 'periodicEventTypes', 'runningEventExecutions', 'workerRunning', 'registeredTypes', 'executableTypes', 'periodicTypes', 'runningExecutions'];
const loading = computed(() => runtimeLoading.value || tasksLoading.value || databasesLoading.value);
const stale = computed(() => Boolean(runtimeError.value || tasksError.value || databasesError.value));
const collectedAt = computed(() => {
  const values = [runtimeSnapshot.value?.collectedAt, taskSnapshot.value?.collectedAt, databaseSnapshots.value?.collectedAt]
    .filter((value): value is string => Boolean(value))
    .sort();
  return values[values.length - 1];
});
const tabs = computed(() => [
  { label: t('debugPage.runtime'), value: 'runtime' },
  { label: t('debugPage.tasks'), value: 'tasks' },
  { label: t('debugPage.database'), value: 'database' },
]);
const taskDefinitionColumns = computed<Array<{ key: keyof TaskDefinitionRow & string; label: string; align?: 'left' | 'right' }>>(() => [
  { key: 'type', label: t('common.type') },
  { key: 'kind', label: t('debugPage.kind') },
  { key: 'actions', label: t('debugPage.actionsColumn') },
  { key: 'concurrencyPolicy', label: t('debugPage.concurrencyPolicy') },
  { key: 'defaultMaxRetries', label: t('debugPage.maxRetries'), align: 'right' },
  { key: 'staleQueuedAfter', label: t('debugPage.staleQueuedAfter'), align: 'right' },
]);
const databaseTableColumns = computed(() => [
  { key: 'name', label: t('debugPage.table') },
  { key: 'rows', label: t('debugPage.rows') },
  { key: 'size', label: t('debugPage.size') },
]);
function eventRuntimeSnapshot() {
  return taskSnapshot.value?.eventRuntime ?? taskSnapshot.value?.tasks;
}

const taskMetricRows = computed<TaskMetricRow[]>(() => {
  const tasks = eventRuntimeSnapshot();
  if (!tasks) return [];
  const entries = Object.entries(tasks).filter((entry): entry is [string, string | number | boolean | null | undefined] => entry[0] !== 'definitions' && entry[0] !== 'eventDefinitions' && isScalarDiagnosticValue(entry[1]));
  return entries
    .sort(([left], [right]) => taskMetricSortIndex(left) - taskMetricSortIndex(right) || left.localeCompare(right))
    .map(([key, value]) => ({ key, label: taskMetricLabel(key), value: formatDiagnosticValue(value) }));
});
const taskDefinitionRows = computed<TaskDefinitionRow[]>(() => {
  const runtime = eventRuntimeSnapshot();
  const definitions = runtime?.eventDefinitions ?? runtime?.definitions ?? [];
  return definitions.map((definition) => ({
    type: definition.type,
    kind: formatTaskKind(definition),
    actions: formatTaskActions(definition),
    concurrencyPolicy: definition.concurrencyPolicy || t('common.notAvailable'),
    defaultMaxRetries: definition.defaultMaxRetries,
    staleQueuedAfter: formatSeconds(definition.staleQueuedAfterSeconds),
  }));
});
const pprofUrl = computed(() => (pprof.value?.enabled && pprof.value?.address) ? `http://${pprof.value.address}/debug/pprof/` : null);

const databaseTotals = computed(() => {
  const dbs = databaseSnapshots.value?.databases ?? [];
  return {
    healthy: dbs.filter((item) => item.healthy).length,
    total: dbs.length,
    used: dbs.reduce((sum, item) => sum + (item.usedBytes || 0), 0),
  };
});

function isScalarDiagnosticValue(value: unknown): value is string | number | boolean | null | undefined {
  return value == null || ['string', 'number', 'boolean'].includes(typeof value);
}

function taskMetricSortIndex(key: string) {
  const index = taskMetricOrder.indexOf(key);
  return index === -1 ? Number.MAX_SAFE_INTEGER : index;
}

function taskMetricLabel(key: string) {
  const label = t(`debugPage.metric.${key}`);
  return label === `debugPage.metric.${key}` ? key : label;
}

function formatDiagnosticValue(value: string | number | boolean | null | undefined) {
  if (typeof value === 'boolean') return value ? t('debugPage.yes') : t('debugPage.no');
  if (value == null || value === '') return t('common.notAvailable');
  return String(value);
}

function formatTaskKind(definition: DebugTaskDefinition) {
  const flags = [
    definition.executable ? t('debugPage.executable') : '',
    definition.periodic ? t('debugPage.periodic') : '',
    definition.hidden ? t('debugPage.hidden') : '',
  ].filter(Boolean);
  return flags.length ? flags.join(' / ') : t('common.notAvailable');
}

function formatTaskActions(definition: DebugTaskDefinition) {
  const actions = [
    definition.allowRunNow ? t('common.runNow') : '',
    definition.allowRetry ? t('common.retry') : '',
  ].filter(Boolean);
  return actions.length ? actions.join(' / ') : t('common.notAvailable');
}

async function loadPprof() {
  pprofError.value = '';
  try {
    pprof.value = await debugApi.pprofStatus();
  } catch (err) {
    // 区块加载失败只做就地错误态 + 重试，不弹浮层。
    pprofError.value = err instanceof Error ? err.message : t('debugPage.pprofLoadFailed');
  }
}

async function togglePprof(enabled: boolean) {
  if (pprofPending.value) return;
  pprofPending.value = true;
  try {
    pprof.value = await debugApi.setPprof(enabled);
  } catch (err) {
    notifyError(err instanceof Error ? err.message : t('debugPage.pprofToggleFailed'), err);
  } finally {
    pprofPending.value = false;
  }
}

async function clearRuntimeData() {
  if (!canClear.value) return;
  await startClear();
  clearOpen.value = false;
}

async function loadRuntime() {
  if (runtimeLoading.value) return;
  const requestId = ++runtimeRequestId;
  runtimeLoading.value = true;
  runtimeError.value = '';
  try {
    const next = await debugApi.runtime();
    if (requestId !== runtimeRequestId) return;
    runtimeSnapshot.value = next;
  } catch (err) {
    if (requestId !== runtimeRequestId) return;
    runtimeError.value = err instanceof Error ? err.message : t('debugPage.loadFailed');
  } finally {
    if (requestId === runtimeRequestId) runtimeLoading.value = false;
  }
}

async function loadTasks() {
  if (tasksLoading.value) return;
  const requestId = ++tasksRequestId;
  tasksLoading.value = true;
  tasksError.value = '';
  try {
    const next = await debugApi.tasks();
    if (requestId !== tasksRequestId) return;
    taskSnapshot.value = next;
  } catch (err) {
    if (requestId !== tasksRequestId) return;
    tasksError.value = err instanceof Error ? err.message : t('debugPage.loadFailed');
  } finally {
    if (requestId === tasksRequestId) tasksLoading.value = false;
  }
}

async function loadDatabases() {
  if (databasesLoading.value) return;
  const requestId = ++databasesRequestId;
  databasesLoading.value = true;
  databasesError.value = '';
  try {
    const next = await debugApi.databases();
    if (requestId !== databasesRequestId) return;
    databaseSnapshots.value = next;
  } catch (err) {
    if (requestId !== databasesRequestId) return;
    databasesError.value = err instanceof Error ? err.message : t('debugPage.loadFailed');
  } finally {
    if (requestId === databasesRequestId) databasesLoading.value = false;
  }
}

async function loadAll() {
  await Promise.allSettled([loadRuntime(), loadTasks(), loadDatabases()]);
}

function formatBytes(value?: number) {
  if (!value) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB'];
  let size = value;
  let index = 0;
  while (size >= 1024 && index < units.length - 1) {
    size /= 1024;
    index += 1;
  }
  return `${size.toFixed(index ? 1 : 0)} ${units[index]}`;
}

function formatSeconds(value?: number) {
  if (!value || value < 0) return t('common.notAvailable');
  if (value < 60) return `${value}s`;
  if (value < 3600) return `${Math.round(value / 60)}m`;
  if (value < 86400) return `${Math.round(value / 3600)}h`;
  return `${Math.round(value / 86400)}d`;
}

function dbTone(db: DebugDatabase) {
  return db.healthy ? 'success' : 'danger';
}

function startPolling() {
  timer = window.setInterval(() => {
    if (!paused.value && !clearRunning.value && document.visibilityState === 'visible') void loadAll();
    if (!clearRunning.value && document.visibilityState === 'visible') void refreshClearStatus();
  }, 8000);
}

onMounted(() => {
  void loadAll();
  void loadPprof();
  startPolling();
});
onBeforeUnmount(() => window.clearInterval(timer));
</script>

<template>
  <ConsolePage :title="t('routes.debug.title')" :description="t('routes.debug.description')">
    <template #actions>
      <Button class="max-sm:w-full" size="sm" :loading="loading" :disabled="clearRunning" @click="loadAll"><RefreshCcw />{{ t('common.refresh') }}</Button>
      <Button class="max-sm:w-full" size="sm" :variant="paused ? 'primary' : 'secondary'" @click="paused = !paused">
        <Play v-if="paused" />
        <Pause v-else />
        {{ paused ? t('debugPage.resume') : t('debugPage.pause') }}
      </Button>
    </template>

    <div class="grid min-w-0 gap-4">
      <div class="flex flex-wrap items-center justify-between gap-3">
        <div class="flex flex-wrap items-center gap-2">
          <Badge :tone="stale ? 'warning' : 'success'">{{ stale ? t('debugPage.staleSnapshot') : t('debugPage.liveSnapshot') }}</Badge>
          <Badge tone="info">{{ formatDateTime(collectedAt) || t('common.never') }}</Badge>
        </div>
      </div>

      <!-- 短分组在 wide(≥1239px) 起两列并排；长定义表/数据库表各自全宽。 -->
      <div class="grid gap-4 wide:grid-cols-2">
        <section class="surface-card p-4">
          <div class="flex flex-wrap items-center justify-between gap-3">
            <div class="grid gap-1">
              <h3 class="m-0 flex items-center gap-2 text-body-strong font-semibold text-foreground"><Activity class="size-4" />{{ t('debugPage.pprof') }}</h3>
              <p class="text-sm text-muted-foreground">{{ pprof?.enabled ? t('debugPage.pprofEnabledHint') : t('debugPage.pprofDisabledHint') }}</p>
            </div>
            <div class="flex min-w-0 flex-wrap items-center gap-3">
              <a v-if="pprofUrl" class="min-w-0 break-all text-xs text-muted-foreground transition-colors hover:text-foreground" :href="pprofUrl" target="_blank" rel="noreferrer">{{ pprofUrl }}</a>
              <Switch :model-value="pprof?.enabled ?? false" :disabled="pprofPending" :label="t('debugPage.pprof')" @update:model-value="togglePprof" />
            </div>
          </div>
          <div v-if="pprofError" class="mt-3 flex min-w-0 flex-wrap items-center gap-2 rounded-control border border-danger-border bg-danger-bg p-3 text-sm text-danger" role="alert">
            <span class="min-w-0 break-words">{{ pprofError }}</span>
            <Button size="sm" variant="secondary" @click="loadPprof"><RefreshCcw />{{ t('common.retry') }}</Button>
          </div>
        </section>

        <section class="rounded-overlay border border-danger-border bg-danger-bg p-4">
          <div class="flex flex-wrap items-center justify-between gap-3">
            <div class="grid gap-1">
              <h3 class="m-0 flex items-center gap-2 text-body-strong font-semibold text-danger"><Trash2 class="size-4" />{{ t('debugPage.clearRuntimeData') }}</h3>
              <p class="m-0 text-sm text-danger">{{ t('debugPage.clearRuntimeDataHint') }}</p>
            </div>
            <Button v-permission="'debug.manage'" data-perm-anchor="debug.manage.clear" class="max-sm:w-full" variant="danger" :loading="clearSubmitting" :disabled="!canClear" @click="clearOpen = true"><Trash2 />{{ t('debugPage.clearRuntimeData') }}</Button>
          </div>
          <div class="mt-4 grid gap-3 rounded-control border border-divider bg-control p-4 text-sm" aria-live="polite" aria-atomic="true">
            <div class="flex flex-wrap items-center justify-between gap-2">
              <div class="flex flex-wrap items-center gap-2"><strong class="text-body-strong text-foreground">{{ t('debugPage.cleanup.latest') }}</strong><Badge :tone="clearTone">{{ clearStateLabel }}</Badge></div>
              <Button class="max-sm:w-full" size="sm" variant="ghost" :loading="clearChecking" :disabled="clearSubmitting" @click="refreshClearStatus"><RefreshCcw />{{ t('debugPage.cleanup.checkStatus') }}</Button>
            </div>
            <dl v-if="clearStatus?.runId" class="grid gap-3 sm:grid-cols-3">
              <div><dt class="text-caption text-muted-foreground">{{ t('debugPage.cleanup.phase') }}</dt><dd class="m-0 break-words text-body text-foreground">{{ clearStageLabel(clearStatus.failedStage || clearStatus.stage) }}</dd></div>
              <div><dt class="text-caption text-muted-foreground">{{ t('debugPage.cleanup.startedAt') }}</dt><dd class="m-0 break-words text-body text-foreground">{{ formatDateTime(clearStatus.startedAt) || t('common.notAvailable') }}</dd></div>
              <div><dt class="text-caption text-muted-foreground">{{ t('debugPage.cleanup.finishedAt') }}</dt><dd class="m-0 break-words text-body text-foreground">{{ formatDateTime(clearStatus.finishedAt) || t('common.notAvailable') }}</dd></div>
            </dl>
            <p v-if="clearQueryFailed" role="alert" class="m-0 text-warning">{{ t('debugPage.cleanup.queryFailed') }}</p>
            <p v-else-if="clearResultMissing" role="alert" class="m-0 text-warning">{{ t('debugPage.cleanup.resultMissing') }}</p>
            <p v-else-if="clearRunning" class="m-0 text-muted-foreground">{{ t('debugPage.cleanup.runningHint') }}</p>
            <p v-else-if="clearStatus?.status === 'failed'" role="alert" class="m-0 text-danger">{{ clearFailureHint }}</p>
            <p v-else-if="clearStatus?.status === 'succeeded'" class="m-0 text-muted-foreground">{{ t('debugPage.cleanup.completedHint') }}</p>
          </div>
        </section>
      </div>

      <Tabs v-model="activeTab" class="min-h-0" :tabs="tabs">
        <section v-if="activeTab === 'runtime'" class="grid gap-4 wide:grid-cols-2">
          <template v-if="runtimeLoading && !runtimeSnapshot">
            <div class="grid grid-cols-1 gap-4 wide:grid-cols-3" aria-busy="true">
              <div class="surface-card p-4">
                <span class="block text-caption text-muted-foreground">{{ t('debugPage.uptime') }}</span>
                <Skeleton class="mt-1 h-5 w-16" />
              </div>
              <div class="surface-card p-4">
                <span class="block text-caption text-muted-foreground">{{ t('debugPage.goroutines') }}</span>
                <Skeleton class="mt-1 h-5 w-12" />
              </div>
              <div class="surface-card p-4">
                <span class="block text-caption text-muted-foreground">{{ t('debugPage.heap') }}</span>
                <Skeleton class="mt-1 h-5 w-20" />
              </div>
            </div>
            <section class="surface-card p-4" aria-busy="true">
              <h3 class="m-0 flex items-center gap-2 text-body-strong font-semibold text-foreground"><Activity class="size-4" />{{ t('debugPage.process') }}</h3>
              <dl class="mt-4 grid grid-cols-1 gap-3 wide:grid-cols-2">
                <div><dt class="text-caption text-muted-foreground">PID</dt><dd class="m-0 mt-1"><Skeleton class="h-4 w-16" /></dd></div>
                <div><dt class="text-caption text-muted-foreground">{{ t('debugPage.goVersion') }}</dt><dd class="m-0 mt-1"><Skeleton class="h-4 w-24" /></dd></div>
                <div><dt class="text-caption text-muted-foreground">{{ t('debugPage.platform') }}</dt><dd class="m-0 mt-1"><Skeleton class="h-4 w-28" /></dd></div>
                <div><dt class="text-caption text-muted-foreground">{{ t('debugPage.cpu') }}</dt><dd class="m-0 mt-1"><Skeleton class="h-4 w-10" /></dd></div>
              </dl>
            </section>
          </template>
          <EmptyState v-else-if="runtimeError && !runtimeSnapshot" class="wide:col-span-2" :title="t('common.loadFailed')" :description="runtimeError">
            <template #actions><Button size="sm" :loading="runtimeLoading" @click="loadRuntime"><RefreshCcw />{{ t('common.retry') }}</Button></template>
          </EmptyState>
          <template v-else-if="runtimeSnapshot">
            <div class="grid grid-cols-1 gap-4 wide:grid-cols-3">
              <div class="surface-card p-4"><span class="block text-caption text-muted-foreground">{{ t('debugPage.uptime') }}</span><strong class="mt-1 block text-body-strong text-foreground">{{ runtimeSnapshot.process.uptimeSeconds }}s</strong></div>
              <div class="surface-card p-4"><span class="block text-caption text-muted-foreground">{{ t('debugPage.goroutines') }}</span><strong class="mt-1 block text-body-strong text-foreground">{{ runtimeSnapshot.process.goroutineCount }}</strong></div>
              <div class="surface-card p-4"><span class="block text-caption text-muted-foreground">{{ t('debugPage.heap') }}</span><strong class="mt-1 block break-words text-body-strong text-foreground">{{ formatBytes(Number(runtimeSnapshot.memory.heapAllocBytes || runtimeSnapshot.memory.allocBytes || 0)) }}</strong></div>
            </div>
            <section class="surface-card p-4">
              <h3 class="m-0 flex items-center gap-2 text-body-strong font-semibold text-foreground"><Activity class="size-4" />{{ t('debugPage.process') }}</h3>
              <dl class="mt-4 grid grid-cols-1 gap-3 wide:grid-cols-2">
                <div><dt class="text-caption text-muted-foreground">PID</dt><dd class="m-0 break-words text-body text-foreground">{{ runtimeSnapshot.process.pid }}</dd></div>
                <div><dt class="text-caption text-muted-foreground">{{ t('debugPage.goVersion') }}</dt><dd class="m-0 break-words text-body text-foreground">{{ runtimeSnapshot.process.goVersion }}</dd></div>
                <div><dt class="text-caption text-muted-foreground">{{ t('debugPage.platform') }}</dt><dd class="m-0 break-words text-body text-foreground">{{ runtimeSnapshot.process.os }} / {{ runtimeSnapshot.process.architecture }}</dd></div>
                <div><dt class="text-caption text-muted-foreground">{{ t('debugPage.cpu') }}</dt><dd class="m-0 break-words text-body text-foreground">{{ runtimeSnapshot.process.cpuCount }}</dd></div>
              </dl>
            </section>
          </template>
          <EmptyState v-else class="wide:col-span-2" :title="t('debugPage.empty')" :description="t('debugPage.emptyHint')" />
        </section>

        <section v-else-if="activeTab === 'tasks'" class="grid gap-4">
          <template v-if="tasksLoading && !taskSnapshot">
            <div class="grid gap-4 wide:grid-cols-2" aria-busy="true">
              <div class="min-w-0 surface-card p-4">
                <h3 class="m-0 flex items-center gap-2 text-body-strong font-semibold text-foreground">{{ t('debugPage.taskRuntime') }}</h3>
                <div class="mt-4 grid grid-cols-1 gap-3 wide:grid-cols-2">
                  <div v-for="item in 4" :key="item" class="rounded-control bg-muted p-3">
                    <Skeleton class="h-3.5 w-24 bg-pressed" />
                    <Skeleton class="mt-2 h-4 w-14 bg-pressed" />
                  </div>
                </div>
              </div>
              <aside class="surface-card p-4">
                <h3 class="m-0 flex items-center gap-2 text-body-strong font-semibold text-foreground">{{ t('debugPage.polling') }}</h3>
                <p class="text-sm text-muted-foreground">{{ paused ? t('debugPage.pausedHint') : t('debugPage.runningHint') }}</p>
              </aside>
            </div>
            <section class="min-w-0 surface-card p-4" aria-busy="true">
              <h3 class="m-0 flex items-center gap-2 text-body-strong font-semibold text-foreground">{{ t('debugPage.taskDefinitions') }}</h3>
              <Table class="mt-4 max-h-96" row-height="auto" :framed="false" :columns="taskDefinitionColumns" :rows="[]" :loading="true" :loading-label="t('common.loading')" />
            </section>
          </template>
          <EmptyState v-else-if="tasksError && !taskSnapshot" :title="t('common.loadFailed')" :description="tasksError">
            <template #actions><Button size="sm" :loading="tasksLoading" @click="loadTasks"><RefreshCcw />{{ t('common.retry') }}</Button></template>
          </EmptyState>
          <template v-else-if="taskSnapshot">
            <div class="grid gap-4 wide:grid-cols-2">
              <div class="min-w-0 surface-card p-4">
                <h3 class="m-0 flex items-center gap-2 text-body-strong font-semibold text-foreground">{{ t('debugPage.taskRuntime') }}</h3>
                <div class="mt-4 grid grid-cols-1 gap-3 wide:grid-cols-2">
                  <div v-for="metric in taskMetricRows" :key="metric.key" class="rounded-control bg-muted p-3"><span class="block text-caption text-muted-foreground">{{ metric.label }}</span><strong class="mt-1 block break-words text-body-strong text-foreground">{{ metric.value }}</strong></div>
                </div>
              </div>
              <aside class="surface-card p-4">
                <h3 class="m-0 flex items-center gap-2 text-body-strong font-semibold text-foreground">{{ t('debugPage.polling') }}</h3>
                <p class="text-sm text-muted-foreground">{{ paused ? t('debugPage.pausedHint') : t('debugPage.runningHint') }}</p>
              </aside>
            </div>
            <section class="min-w-0 surface-card p-4">
              <h3 class="m-0 flex items-center gap-2 text-body-strong font-semibold text-foreground">{{ t('debugPage.taskDefinitions') }}</h3>
              <Table v-if="taskDefinitionRows.length" class="mt-4 max-h-96" row-height="auto" :framed="false" :columns="taskDefinitionColumns" :rows="taskDefinitionRows" row-key="type">
                <template #type="{ row }"><span class="break-all">{{ row.type }}</span></template>
              </Table>
              <p v-else class="mt-3 text-sm text-muted-foreground">{{ t('debugPage.noTaskDefinitions') }}</p>
            </section>
          </template>
          <EmptyState v-else :title="t('debugPage.empty')" :description="t('debugPage.emptyHint')" />
        </section>

        <section v-else class="grid gap-4">
          <template v-if="databasesLoading && !databaseSnapshots">
            <aside class="surface-card p-4" aria-busy="true">
              <h3 class="m-0 flex items-center gap-2 text-body-strong font-semibold text-foreground">{{ t('debugPage.databaseSummary') }}</h3>
              <div class="mt-4 grid grid-cols-1 gap-3 wide:grid-cols-2">
                <div><span class="block text-caption text-muted-foreground">{{ t('debugPage.healthyDatabases') }}</span><Skeleton class="mt-1 h-4 w-14" /></div>
                <div><span class="block text-caption text-muted-foreground">{{ t('debugPage.used') }}</span><Skeleton class="mt-1 h-4 w-20" /></div>
              </div>
            </aside>
            <article class="grid gap-3 surface-card p-4" aria-busy="true">
              <div class="flex flex-wrap items-center justify-between gap-3">
                <Skeleton class="h-5 w-32" />
                <Skeleton class="h-5 w-16 rounded-pill" />
              </div>
              <div class="grid grid-cols-1 gap-3 wide:grid-cols-3">
                <div><span class="block text-caption text-muted-foreground">{{ t('debugPage.fileSize') }}</span><Skeleton class="mt-1 h-4 w-20" /></div>
                <div><span class="block text-caption text-muted-foreground">{{ t('debugPage.used') }}</span><Skeleton class="mt-1 h-4 w-20" /></div>
                <div><span class="block text-caption text-muted-foreground">{{ t('debugPage.free') }}</span><Skeleton class="mt-1 h-4 w-20" /></div>
              </div>
              <Table class="max-h-56" row-height="auto" :framed="false" :columns="databaseTableColumns" :rows="[]" :loading="true" :loading-label="t('common.loading')" />
            </article>
          </template>
          <EmptyState v-else-if="databasesError && !databaseSnapshots" :title="t('common.loadFailed')" :description="databasesError">
            <template #actions><Button size="sm" :loading="databasesLoading" @click="loadDatabases"><RefreshCcw />{{ t('common.retry') }}</Button></template>
          </EmptyState>
          <template v-else-if="databaseSnapshots">
            <aside class="surface-card p-4">
              <h3 class="m-0 flex items-center gap-2 text-body-strong font-semibold text-foreground">{{ t('debugPage.databaseSummary') }}</h3>
              <div class="mt-4 grid grid-cols-1 gap-3 wide:grid-cols-2">
                <div><span class="block text-caption text-muted-foreground">{{ t('debugPage.healthyDatabases') }}</span><strong class="mt-1 block break-words text-body text-foreground">{{ databaseTotals.healthy }} / {{ databaseTotals.total }}</strong></div>
                <div><span class="block text-caption text-muted-foreground">{{ t('debugPage.used') }}</span><strong class="mt-1 block break-words text-body text-foreground">{{ formatBytes(databaseTotals.used) }}</strong></div>
              </div>
            </aside>
            <div class="grid gap-3">
              <article v-for="db in databaseSnapshots.databases" :key="db.name" class="grid gap-3 surface-card p-4">
                <div class="flex flex-wrap items-center justify-between gap-3">
                  <h3 class="m-0 flex min-w-0 items-center gap-2 break-all text-body-strong font-semibold text-foreground"><Database class="size-4 shrink-0" />{{ db.name }}</h3>
                  <Badge :tone="dbTone(db)">{{ db.healthy ? t('state.healthy') : db.errorCode || t('state.critical') }}</Badge>
                </div>
                <div class="grid grid-cols-1 gap-3 wide:grid-cols-3">
                  <div><span class="block text-caption text-muted-foreground">{{ t('debugPage.fileSize') }}</span><strong class="mt-1 block break-words text-body text-foreground">{{ formatBytes(db.fileSizeBytes) }}</strong></div>
                  <div><span class="block text-caption text-muted-foreground">{{ t('debugPage.used') }}</span><strong class="mt-1 block break-words text-body text-foreground">{{ formatBytes(db.usedBytes) }}</strong></div>
                  <div><span class="block text-caption text-muted-foreground">{{ t('debugPage.free') }}</span><strong class="mt-1 block break-words text-body text-foreground">{{ formatBytes(db.freeBytes) }}</strong></div>
                </div>
                <Table class="max-h-56" row-height="auto" :framed="false" :columns="databaseTableColumns" :rows="db.tables" row-key="name">
                  <template #name="{ row }"><span class="break-all">{{ row.name }}</span></template>
                  <template #rows="{ row }">{{ row.rowCount }}</template>
                  <template #size="{ row }">{{ formatBytes(row.totalSizeBytes) }}</template>
                </Table>
              </article>
            </div>
          </template>
          <EmptyState v-else :title="t('debugPage.empty')" :description="t('debugPage.emptyHint')" />
        </section>
      </Tabs>
    </div>
    <ConfirmDialog
      v-model:open="clearOpen"
      :title="t('debugPage.clearRuntimeDataTitle')"
      :description="t('debugPage.clearRuntimeDataDescription')"
      :impact="t('debugPage.clearRuntimeDataImpact')"
      tone="danger"
      :confirm-label="t('debugPage.clearRuntimeDataConfirm')"
      :cancel-label="t('common.cancel')"
      :checkbox-label="t('debugPage.clearRuntimeDataCheckbox')"
      :require-checkbox="true"
      :loading="clearSubmitting"
      @confirm="clearRuntimeData"
    />
  </ConsolePage>
</template>
