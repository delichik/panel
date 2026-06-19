<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue';
import PageLoadingState from '@/components/PageLoadingState.vue';
import { diagnosticsApi } from '@/api/diagnostics';
import type { DebugDatabaseSnapshotDto, DebugSnapshotDto } from '@/types/api';
import { useI18n } from '@/i18n';

const refreshIntervalMs = 5000;
const { formatDateTime, t } = useI18n();
const snapshot = ref<DebugSnapshotDto | null>(null);
const activeTab = ref('runtime');
const activeDatabase = ref('');
const loading = ref(true);
const refreshing = ref(false);
const paused = ref(false);
const error = ref('');
let refreshTimer: number | undefined;

const processItems = computed(() => snapshot.value ? [
  [t('debugPage.startedAt'), formatDateTime(snapshot.value.process.startedAt)],
  [t('debugPage.uptime'), formatDuration(snapshot.value.process.uptimeSeconds)],
  [t('debugPage.pid'), snapshot.value.process.pid],
  [t('debugPage.goVersion'), snapshot.value.process.goVersion],
  [t('debugPage.platform'), `${snapshot.value.process.os}/${snapshot.value.process.architecture}`],
  [t('debugPage.cpuCount'), snapshot.value.process.cpuCount],
  [t('debugPage.cgoCalls'), formatNumber(snapshot.value.process.cgoCallCount)],
] : []);

const memoryItems = computed(() => snapshot.value ? [
  [t('debugPage.alloc'), formatBytes(snapshot.value.memory.allocBytes)],
  [t('debugPage.totalAlloc'), formatBytes(snapshot.value.memory.totalAllocBytes)],
  [t('debugPage.systemReserved'), formatBytes(snapshot.value.memory.sysBytes)],
  [t('debugPage.heapInUse'), formatBytes(snapshot.value.memory.heapInUseBytes)],
  [t('debugPage.heapIdle'), formatBytes(snapshot.value.memory.heapIdleBytes)],
  [t('debugPage.heapReleased'), formatBytes(snapshot.value.memory.heapReleasedBytes)],
  [t('debugPage.heapObjects'), formatNumber(snapshot.value.memory.heapObjects)],
  [t('debugPage.stackInUse'), formatBytes(snapshot.value.memory.stackInUseBytes)],
  [t('debugPage.metadataInUse'), formatBytes(snapshot.value.memory.mspanInUseBytes + snapshot.value.memory.mcacheInUseBytes)],
  [t('debugPage.nextGc'), formatBytes(snapshot.value.memory.nextGcBytes)],
  [t('debugPage.gcCycles'), formatNumber(snapshot.value.memory.gcCycles)],
  [t('debugPage.forcedGcCycles'), formatNumber(snapshot.value.memory.forcedGcCycles)],
  [t('debugPage.gcPauseTotal'), formatNanoseconds(snapshot.value.memory.gcPauseTotalNs)],
  [t('debugPage.lastGc'), snapshot.value.memory.lastGcAt ? formatDateTime(snapshot.value.memory.lastGcAt) : t('common.never')],
] : []);

const taskItems = computed(() => snapshot.value ? [
  [t('debugPage.registeredTaskTypes'), formatNumber(snapshot.value.tasks.registeredTypes)],
  [t('debugPage.executableTaskTypes'), formatNumber(snapshot.value.tasks.executableTypes)],
  [t('debugPage.periodicTaskTypes'), formatNumber(snapshot.value.tasks.periodicTypes)],
  [t('debugPage.runningExecutions'), formatNumber(snapshot.value.tasks.runningExecutions)],
] : []);

const selectedDatabase = computed(() =>
  snapshot.value?.databases.find((database) => database.name === activeDatabase.value)
  ?? snapshot.value?.databases[0]
  ?? null);

async function loadSnapshot() {
  if (refreshing.value) return;
  refreshing.value = true;
  error.value = '';
  try {
    snapshot.value = await diagnosticsApi.snapshot();
    if (!activeDatabase.value || !snapshot.value.databases.some((database) => database.name === activeDatabase.value)) {
      activeDatabase.value = snapshot.value.databases[0]?.name ?? '';
    }
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : t('debugPage.loadFailed');
  } finally {
    loading.value = false;
    refreshing.value = false;
  }
}

function scheduleRefresh() {
  if (refreshTimer) window.clearInterval(refreshTimer);
  refreshTimer = window.setInterval(() => void loadSnapshot(), refreshIntervalMs);
}

function togglePaused() {
  paused.value = !paused.value;
  if (paused.value) {
    if (refreshTimer) window.clearInterval(refreshTimer);
    refreshTimer = undefined;
  } else {
    scheduleRefresh();
    void loadSnapshot();
  }
}

function databaseTitle(database: DebugDatabaseSnapshotDto) {
  const key = `debugPage.databases.${database.name}`;
  const translated = t(key);
  return translated === key ? database.name : translated;
}

function diagnosticError(code?: string) {
  if (!code) return '';
  const key = `debugPage.errors.${code}`;
  const translated = t(key);
  return translated === key ? t('debugPage.errors.unknown') : translated;
}

function formatBytes(value: number) {
  if (!Number.isFinite(value) || value <= 0) return '0 B';
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB'];
  const index = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1);
  const amount = value / (1024 ** index);
  return `${amount.toFixed(index === 0 ? 0 : amount >= 100 ? 0 : amount >= 10 ? 1 : 2)} ${units[index]}`;
}

function formatNumber(value: number) {
  return new Intl.NumberFormat().format(value);
}

function formatDuration(seconds: number) {
  const days = Math.floor(seconds / 86400);
  const hours = Math.floor((seconds % 86400) / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  const rest = Math.floor(seconds % 60);
  return [days ? `${days}d` : '', hours ? `${hours}h` : '', minutes ? `${minutes}m` : '', `${rest}s`].filter(Boolean).join(' ');
}

function formatNanoseconds(value: number) {
  if (value < 1_000_000) return `${(value / 1000).toFixed(1)} µs`;
  if (value < 1_000_000_000) return `${(value / 1_000_000).toFixed(1)} ms`;
  return `${(value / 1_000_000_000).toFixed(2)} s`;
}

function formatPercent(value: number) {
  if (!Number.isFinite(value) || value <= 0) return '0%';
  return `${value >= 10 ? value.toFixed(1) : value.toFixed(2)}%`;
}

function formatSeconds(value: number) {
  return value > 0 ? formatDuration(value) : t('common.notAvailable');
}

function yesNo(value: boolean) {
  return value ? t('common.yes') : t('common.no');
}

function concurrencyLabel(value: string) {
  const key = `debugPage.concurrency.${value}`;
  const translated = t(key);
  return translated === key ? value : translated;
}

onMounted(() => {
  void loadSnapshot();
  scheduleRefresh();
});

onBeforeUnmount(() => {
  if (refreshTimer) window.clearInterval(refreshTimer);
});
</script>

<template>
  <div class="page-shell debug-page">
    <div class="page-toolbar">
      <div class="debug-refresh-state">
        <v-chip :color="paused ? 'warning' : 'success'" size="small" variant="tonal">
          {{ paused ? t('debugPage.paused') : t('debugPage.autoRefresh') }}
        </v-chip>
        <span v-if="snapshot" class="text-caption text-medium-emphasis">
          {{ t('debugPage.collectedAt', { value: formatDateTime(snapshot.collectedAt) }) }}
        </span>
      </div>
      <div class="page-actions">
        <v-btn size="small" variant="outlined" :prepend-icon="paused ? 'mdi-play' : 'mdi-pause'" @click="togglePaused">
          {{ paused ? t('debugPage.resume') : t('debugPage.pause') }}
        </v-btn>
        <v-btn size="small" color="primary" variant="flat" prepend-icon="mdi-refresh" :loading="refreshing" @click="loadSnapshot">
          {{ t('common.refresh') }}
        </v-btn>
      </div>
    </div>

    <v-alert v-if="error" type="error" variant="tonal" density="compact" closable @click:close="error = ''">
      {{ error }}
    </v-alert>

    <PageLoadingState v-if="loading && !snapshot" />

    <v-card v-else-if="snapshot" variant="outlined" class="debug-workspace">
      <v-tabs v-model="activeTab" density="comfortable" class="debug-main-tabs">
        <v-tab value="runtime">{{ t('debugPage.tabs.runtime') }}</v-tab>
        <v-tab value="tasks">{{ t('debugPage.tabs.tasks') }}</v-tab>
        <v-tab value="databases">{{ t('debugPage.tabs.databases') }}</v-tab>
      </v-tabs>

      <v-window v-model="activeTab" class="debug-window">
        <v-window-item value="runtime" class="debug-pane">
          <div class="page-summary-grid">
        <div class="page-summary-card">
          <v-icon color="primary">mdi-memory</v-icon>
          <div><div class="text-caption text-medium-emphasis">{{ t('debugPage.currentAlloc') }}</div><strong class="font-tabular">{{ formatBytes(snapshot.memory.allocBytes) }}</strong></div>
        </div>
        <div class="page-summary-card">
          <v-icon color="info">mdi-layers-triple</v-icon>
          <div><div class="text-caption text-medium-emphasis">{{ t('debugPage.goroutines') }}</div><strong class="font-tabular">{{ formatNumber(snapshot.process.goroutineCount) }}</strong></div>
        </div>
        <div class="page-summary-card">
          <v-icon color="success">mdi-delete-sweep-outline</v-icon>
          <div><div class="text-caption text-medium-emphasis">{{ t('debugPage.gcCycles') }}</div><strong class="font-tabular">{{ formatNumber(snapshot.memory.gcCycles) }}</strong></div>
        </div>
        <div class="page-summary-card">
          <v-icon color="secondary">mdi-database-outline</v-icon>
          <div><div class="text-caption text-medium-emphasis">{{ t('debugPage.databaseFiles') }}</div><strong class="font-tabular">{{ snapshot.databases.length }}</strong></div>
        </div>
      </div>

      <div class="debug-grid">
        <v-card variant="outlined" class="debug-card">
          <v-card-title>{{ t('debugPage.processRuntime') }}</v-card-title>
          <v-card-text><div class="info-grid"><div v-for="[label, value] in processItems" :key="String(label)"><span>{{ label }}</span><strong>{{ value }}</strong></div></div></v-card-text>
        </v-card>
        <v-card variant="outlined" class="debug-card">
          <v-card-title>{{ t('debugPage.memoryGc') }}</v-card-title>
          <v-card-text><div class="info-grid"><div v-for="[label, value] in memoryItems" :key="String(label)"><span>{{ label }}</span><strong>{{ value }}</strong></div></div></v-card-text>
        </v-card>
      </div>
        </v-window-item>

        <v-window-item value="tasks" class="debug-pane task-pane">
          <div class="page-summary-grid">
            <div class="page-summary-card">
              <v-icon :color="snapshot.tasks.workerRunning ? 'success' : 'error'">mdi-clipboard-check-outline</v-icon>
              <div><div class="text-caption text-medium-emphasis">{{ t('debugPage.taskWorker') }}</div><strong>{{ snapshot.tasks.workerRunning ? t('debugPage.running') : t('debugPage.stopped') }}</strong></div>
            </div>
            <div v-for="[label, value] in taskItems" :key="String(label)" class="page-summary-card">
              <v-icon color="primary">mdi-format-list-numbered</v-icon>
              <div><div class="text-caption text-medium-emphasis">{{ label }}</div><strong class="font-tabular">{{ value }}</strong></div>
            </div>
          </div>
          <div class="debug-table-scroll">
            <v-table density="compact" class="debug-table task-definition-table">
              <thead><tr>
                <th>{{ t('debugPage.taskType') }}</th>
                <th>{{ t('debugPage.hidden') }}</th>
                <th>{{ t('debugPage.executable') }}</th>
                <th>{{ t('debugPage.periodic') }}</th>
                <th>{{ t('debugPage.runNowRetry') }}</th>
                <th>{{ t('debugPage.defaultRetries') }}</th>
                <th>{{ t('debugPage.concurrencyPolicy') }}</th>
                <th>{{ t('debugPage.staleQueuedAfter') }}</th>
                <th>{{ t('debugPage.periodicInterval') }}</th>
              </tr></thead>
              <tbody>
                <tr v-for="definition in snapshot.tasks.definitions" :key="definition.type">
                  <td class="mono">{{ definition.type }}</td>
                  <td>{{ yesNo(definition.hidden) }}</td>
                  <td>{{ yesNo(definition.executable) }}</td>
                  <td>{{ yesNo(definition.periodic) }}</td>
                  <td>{{ yesNo(definition.allowRunNow) }} / {{ yesNo(definition.allowRetry) }}</td>
                  <td class="font-tabular">{{ definition.defaultMaxRetries }}</td>
                  <td>{{ concurrencyLabel(definition.concurrencyPolicy) }}</td>
                  <td class="font-tabular">{{ formatSeconds(definition.staleQueuedAfterSeconds) }}</td>
                  <td class="font-tabular">{{ formatSeconds(definition.periodicIntervalSeconds) }}</td>
                </tr>
              </tbody>
            </v-table>
          </div>
        </v-window-item>

        <v-window-item value="databases" class="debug-pane database-pane">
          <v-tabs v-model="activeDatabase" density="compact" class="database-tabs">
            <v-tab v-for="database in snapshot.databases" :key="database.name" :value="database.name">
              {{ databaseTitle(database) }}
              <v-icon :color="database.healthy ? 'success' : 'error'" size="x-small" class="ml-2">mdi-circle</v-icon>
            </v-tab>
          </v-tabs>
          <template v-if="selectedDatabase">
            <div class="database-content">
              <v-alert v-if="selectedDatabase.errorCode" type="warning" variant="tonal" density="compact">{{ diagnosticError(selectedDatabase.errorCode) }}</v-alert>
              <v-alert v-if="selectedDatabase.tableSizeErrorCode" type="warning" variant="tonal" density="compact">{{ diagnosticError(selectedDatabase.tableSizeErrorCode) }}</v-alert>
          <div class="info-grid database-info">
                <div><span>{{ t('debugPage.fileSize') }}</span><strong>{{ formatBytes(selectedDatabase.fileSizeBytes) }}</strong></div>
                <div><span>{{ t('debugPage.usedSpace') }}</span><strong>{{ formatBytes(selectedDatabase.usedBytes) }}</strong></div>
                <div><span>{{ t('debugPage.freeSpace') }}</span><strong>{{ formatBytes(selectedDatabase.freeBytes) }}</strong></div>
                <div><span>{{ t('debugPage.pages') }}</span><strong>{{ formatNumber(selectedDatabase.pageCount) }} × {{ formatBytes(selectedDatabase.pageSizeBytes) }}</strong></div>
                <div><span>{{ t('debugPage.openConnections') }}</span><strong>{{ selectedDatabase.connections.openConnections }} / {{ selectedDatabase.connections.maxOpenConnections }}</strong></div>
                <div><span>{{ t('debugPage.inUseIdle') }}</span><strong>{{ selectedDatabase.connections.inUse }} / {{ selectedDatabase.connections.idle }}</strong></div>
                <div><span>{{ t('debugPage.waitCount') }}</span><strong>{{ formatNumber(selectedDatabase.connections.waitCount) }}</strong></div>
                <div><span>{{ t('debugPage.waitDuration') }}</span><strong>{{ formatNanoseconds(selectedDatabase.connections.waitDurationNs) }}</strong></div>
              </div>
              <div class="debug-table-scroll">
                <v-table density="compact" class="debug-table">
                  <thead><tr>
                    <th>{{ t('debugPage.tableName') }}</th>
                    <th class="text-right">{{ t('debugPage.rowCount') }}</th>
                    <th class="text-right">{{ t('debugPage.tableDataSize') }}</th>
                    <th class="text-right">{{ t('debugPage.indexSize') }}</th>
                    <th class="text-right">{{ t('debugPage.totalSize') }}</th>
                    <th class="text-right">{{ t('debugPage.databaseShare') }}</th>
                  </tr></thead>
                  <tbody>
                    <tr v-if="selectedDatabase.tables.length === 0"><td colspan="6" class="text-center text-medium-emphasis">{{ t('debugPage.noTables') }}</td></tr>
                    <tr v-for="table in selectedDatabase.tables" :key="table.name">
                      <td class="mono">{{ table.name }}</td>
                      <td class="text-right font-tabular">{{ table.errorCode ? diagnosticError(table.errorCode) : formatNumber(table.rowCount) }}</td>
                      <td class="text-right font-tabular">{{ formatBytes(table.dataSizeBytes) }}</td>
                      <td class="text-right font-tabular">{{ formatBytes(table.indexSizeBytes) }}</td>
                      <td class="text-right font-tabular font-weight-bold">{{ formatBytes(table.totalSizeBytes) }}</td>
                      <td class="text-right font-tabular">{{ formatPercent(table.databasePercent) }}</td>
                    </tr>
                  </tbody>
                </v-table>
              </div>
            </div>
          </template>
        </v-window-item>
      </v-window>
    </v-card>
  </div>
</template>

<style scoped>
.debug-page { overflow: hidden; }
.debug-refresh-state, .database-title { display: flex; align-items: center; gap: 10px; }
.debug-workspace { flex: 1 1 auto; min-height: 0; display: flex; flex-direction: column; overflow: hidden; }
.debug-workspace:hover { border-color: var(--lp-border) !important; box-shadow: var(--lp-shadow-sm) !important; }
.debug-main-tabs, .database-tabs { flex: 0 0 auto; border-bottom: 1px solid var(--lp-border); }
.debug-window { flex: 1 1 auto; min-height: 0; }
.debug-window :deep(.v-window__container), .debug-window :deep(.v-window-item) { height: 100%; min-height: 0; }
.debug-pane { overflow: auto; padding: 16px; }
.task-pane, .database-pane { display: flex; flex-direction: column; gap: 16px; overflow: hidden; }
.debug-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 16px; }
.debug-card:hover { border-color: var(--lp-border) !important; box-shadow: var(--lp-shadow-sm) !important; }
.debug-card .v-card-title { padding: 16px 18px 12px; font-size: 1rem; font-weight: 700; }
.info-grid strong { display: block; margin-top: 4px; font-size: 13px; overflow-wrap: anywhere; }
.database-title { justify-content: space-between; }
.database-content { flex: 1 1 auto; min-height: 0; display: flex; flex-direction: column; gap: 16px; }
.database-info { flex: 0 0 auto; }
.debug-table-scroll { flex: 1 1 auto; min-height: 0; overflow: auto; border: 1px solid var(--lp-border); border-radius: 8px; }
.debug-table { border: 1px solid var(--lp-border); border-radius: 8px; }
.debug-table-scroll .debug-table { border: 0; border-radius: 0; }
.task-definition-table { min-width: 1120px; }
.mono { font-family: ui-monospace, SFMono-Regular, Consolas, monospace; overflow-wrap: anywhere; }
@media (max-width: 900px) { .debug-grid { grid-template-columns: 1fr; } }
@media (max-width: 760px) {
  .debug-page { overflow: visible; }
  .debug-workspace, .debug-window, .debug-window :deep(.v-window__container), .debug-window :deep(.v-window-item) { height: auto; overflow: visible; }
  .debug-pane, .task-pane, .database-pane, .database-content, .debug-table-scroll { overflow: visible; }
  .debug-table-scroll { overflow-x: auto; }
  .page-toolbar { align-items: flex-start; }
  .debug-refresh-state { align-items: flex-start; flex-direction: column; }
}
</style>
