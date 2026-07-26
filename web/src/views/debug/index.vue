<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue';
import { Activity, Database, Pause, Play, RefreshCcw } from '@lucide/vue';
import { debugApi } from '@/api/debug';
import Badge from '@/components/ui/Badge.vue';
import Button from '@/components/ui/Button.vue';
import EmptyState from '@/components/ui/EmptyState.vue';
import Table from '@/components/ui/Table.vue';
import Tabs from '@/components/ui/Tabs.vue';
import ConsolePage from '@/components/templates/ConsolePage.vue';
import WorkspacePage from '@/components/templates/WorkspacePage.vue';
import { useI18n } from '@/i18n';
import type { DebugDatabase, DebugSnapshot, DebugTaskDefinition } from '@/types/debug';

const { t } = useI18n();

const snapshot = ref<DebugSnapshot | null>(null);
const lastGoodSnapshot = ref<DebugSnapshot | null>(null);
const activeTab = ref('runtime');
const loading = ref(false);
const paused = ref(false);
const error = ref('');
let timer: number | undefined;

type TaskMetricRow = { key: string; label: string; value: string };
type TaskDefinitionRow = {
  type: string;
  kind: string;
  actions: string;
  concurrencyPolicy: string;
  defaultMaxRetries: number;
  staleQueuedAfter: string;
  periodicInterval: string;
};

const taskMetricOrder = ['workerRunning', 'registeredTypes', 'executableTypes', 'periodicTypes', 'runningExecutions'];
const view = computed(() => snapshot.value ?? lastGoodSnapshot.value);
const stale = computed(() => Boolean(error.value && lastGoodSnapshot.value));
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
  { key: 'periodicInterval', label: t('debugPage.periodicInterval'), align: 'right' },
]);
const taskMetricRows = computed<TaskMetricRow[]>(() => {
  const tasks = view.value?.tasks;
  if (!tasks) return [];
  const entries = Object.entries(tasks).filter((entry): entry is [string, string | number | boolean | null | undefined] => entry[0] !== 'definitions' && isScalarDiagnosticValue(entry[1]));
  return entries
    .sort(([left], [right]) => taskMetricSortIndex(left) - taskMetricSortIndex(right) || left.localeCompare(right))
    .map(([key, value]) => ({ key, label: taskMetricLabel(key), value: formatDiagnosticValue(value) }));
});
const taskDefinitionRows = computed<TaskDefinitionRow[]>(() => (view.value?.tasks.definitions ?? []).map((definition) => ({
  type: definition.type,
  kind: formatTaskKind(definition),
  actions: formatTaskActions(definition),
  concurrencyPolicy: definition.concurrencyPolicy || t('common.notAvailable'),
  defaultMaxRetries: definition.defaultMaxRetries,
  staleQueuedAfter: formatSeconds(definition.staleQueuedAfterSeconds),
  periodicInterval: definition.periodic ? formatSeconds(definition.periodicIntervalSeconds) : t('common.notAvailable'),
})));
const databaseTotals = computed(() => {
  const dbs = view.value?.databases ?? [];
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

async function load() {
  loading.value = true;
  error.value = '';
  try {
    const next = await debugApi.snapshot();
    snapshot.value = next;
    lastGoodSnapshot.value = next;
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('debugPage.loadFailed');
    snapshot.value = null;
  } finally {
    loading.value = false;
  }
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
    if (!paused.value) void load();
  }, 8000);
}

onMounted(async () => {
  await load();
  startPolling();
});
onBeforeUnmount(() => window.clearInterval(timer));
</script>

<template>
  <ConsolePage :title="t('routes.debug.title')" :description="t('routes.debug.description')">
    <template #actions>
      <Button size="sm" :loading="loading" @click="load"><RefreshCcw />{{ t('common.refresh') }}</Button>
      <Button size="sm" :variant="paused ? 'primary' : 'secondary'" @click="paused = !paused">
        <Play v-if="paused" />
        <Pause v-else />
        {{ paused ? t('debugPage.resume') : t('debugPage.pause') }}
      </Button>
    </template>

    <WorkspacePage>
      <template #toolbar>
        <div class="flex flex-wrap items-center justify-between gap-3">
          <div class="flex flex-wrap gap-2">
            <Badge :tone="stale ? 'warning' : 'success'">{{ stale ? t('debugPage.staleSnapshot') : t('debugPage.liveSnapshot') }}</Badge>
            <Badge tone="info">{{ view?.collectedAt || t('common.never') }}</Badge>
          </div>
          <div v-if="error" class="rounded-xl border border-warning-border bg-warning-bg px-3 py-2 text-sm text-warning">{{ error }}</div>
        </div>
      </template>

      <EmptyState v-if="!view" :title="t('debugPage.empty')" :description="t('debugPage.emptyHint')" />
      <Tabs v-else v-model="activeTab" class="h-full min-h-[600px]" :tabs="tabs">
        <section v-if="activeTab === 'runtime'" class="grid gap-4 xl:grid-cols-[minmax(0,1fr)_320px]">
          <div class="grid gap-4 md:grid-cols-3">
            <div class="rounded-2xl border border-border bg-card p-4"><span>{{ t('debugPage.uptime') }}</span><strong>{{ view.process.uptimeSeconds }}s</strong></div>
            <div class="rounded-2xl border border-border bg-card p-4"><span>{{ t('debugPage.goroutines') }}</span><strong>{{ view.process.goroutineCount }}</strong></div>
            <div class="rounded-2xl border border-border bg-card p-4"><span>{{ t('debugPage.heap') }}</span><strong>{{ formatBytes(Number(view.memory.heapAllocBytes || view.memory.allocBytes || 0)) }}</strong></div>
          </div>
          <section class="rounded-2xl border border-border bg-card p-5">
            <h3><Activity class="size-4" />{{ t('debugPage.process') }}</h3>
            <dl class="mt-4 grid grid-cols-2 gap-3 text-sm max-md:grid-cols-1">
              <div><dt>PID</dt><dd>{{ view.process.pid }}</dd></div>
              <div><dt>{{ t('debugPage.goVersion') }}</dt><dd>{{ view.process.goVersion }}</dd></div>
              <div><dt>{{ t('debugPage.platform') }}</dt><dd>{{ view.process.os }} / {{ view.process.architecture }}</dd></div>
              <div><dt>{{ t('debugPage.cpu') }}</dt><dd>{{ view.process.cpuCount }}</dd></div>
            </dl>
          </section>
        </section>

        <section v-else-if="activeTab === 'tasks'" class="grid gap-4 xl:grid-cols-[minmax(0,1fr)_320px]">
          <div class="min-w-0 rounded-2xl border border-border bg-card p-5">
            <h3>{{ t('debugPage.taskRuntime') }}</h3>
            <div class="mt-4 grid grid-cols-3 gap-3 max-md:grid-cols-1">
              <div v-for="metric in taskMetricRows" :key="metric.key" class="rounded-xl border border-border bg-background p-3"><span>{{ metric.label }}</span><strong>{{ metric.value }}</strong></div>
            </div>
            <div class="mt-6 min-w-0">
              <h3>{{ t('debugPage.taskDefinitions') }}</h3>
              <Table v-if="taskDefinitionRows.length" class="mt-4 max-h-96" :columns="taskDefinitionColumns" :rows="taskDefinitionRows" row-key="type" />
              <p v-else class="mt-3 text-sm text-muted-foreground">{{ t('debugPage.noTaskDefinitions') }}</p>
            </div>
          </div>
          <aside class="rounded-2xl border border-border bg-card p-5">
            <h3>{{ t('debugPage.polling') }}</h3>
            <p class="text-sm text-muted-foreground">{{ paused ? t('debugPage.pausedHint') : t('debugPage.runningHint') }}</p>
          </aside>
        </section>

        <section v-else class="grid gap-4 xl:grid-cols-[minmax(0,1fr)_320px]">
          <div class="grid min-h-0 gap-3">
            <article v-for="db in view.databases" :key="db.name" class="grid gap-3 rounded-2xl border border-border bg-card p-4">
              <div class="flex items-center justify-between gap-3">
                <h3><Database class="size-4" />{{ db.name }}</h3>
                <Badge :tone="dbTone(db)">{{ db.healthy ? t('state.healthy') : db.errorCode || t('state.critical') }}</Badge>
              </div>
              <div class="grid grid-cols-3 gap-3 text-sm max-md:grid-cols-1">
                <div><span>{{ t('debugPage.fileSize') }}</span><strong>{{ formatBytes(db.fileSizeBytes) }}</strong></div>
                <div><span>{{ t('debugPage.used') }}</span><strong>{{ formatBytes(db.usedBytes) }}</strong></div>
                <div><span>{{ t('debugPage.free') }}</span><strong>{{ formatBytes(db.freeBytes) }}</strong></div>
              </div>
              <div class="max-h-56 overflow-auto rounded-xl border border-border">
                <table class="w-full text-left text-sm">
                  <thead class="sticky top-0 bg-muted text-xs text-muted-foreground"><tr><th class="p-2">{{ t('debugPage.table') }}</th><th class="p-2">{{ t('debugPage.rows') }}</th><th class="p-2">{{ t('debugPage.size') }}</th></tr></thead>
                  <tbody><tr v-for="table in db.tables" :key="table.name" class="border-t border-border"><td class="p-2">{{ table.name }}</td><td class="p-2">{{ table.rowCount }}</td><td class="p-2">{{ formatBytes(table.totalSizeBytes) }}</td></tr></tbody>
                </table>
              </div>
            </article>
          </div>
          <aside class="rounded-2xl border border-border bg-card p-5">
            <h3>{{ t('debugPage.databaseSummary') }}</h3>
            <div class="mt-4 grid gap-3 text-sm">
              <div><span>{{ t('debugPage.healthyDatabases') }}</span><strong>{{ databaseTotals.healthy }} / {{ databaseTotals.total }}</strong></div>
              <div><span>{{ t('debugPage.used') }}</span><strong>{{ formatBytes(databaseTotals.used) }}</strong></div>
            </div>
          </aside>
        </section>
      </Tabs>
    </WorkspacePage>
  </ConsolePage>
</template>

<style scoped>
h3 {
  display: flex;
  align-items: center;
  gap: 8px;
  margin: 0;
  color: hsl(var(--foreground));
  font-size: 14px;
  font-weight: 650;
}

span,
dt {
  color: hsl(var(--muted-foreground));
  font-size: 12px;
}

strong,
dd {
  margin: 0;
  color: hsl(var(--foreground));
  overflow-wrap: anywhere;
}
</style>
