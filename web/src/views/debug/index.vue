<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue';
import PageLoadingState from '@/components/PageLoadingState.vue';
import { diagnosticsApi } from '@/api/diagnostics';
import type { DebugDatabaseSnapshotDto, DebugSnapshotDto } from '@/types/api';
import { useI18n } from '@/i18n';

const refreshIntervalMs = 5000;
const { formatDateTime, t } = useI18n();
const snapshot = ref<DebugSnapshotDto | null>(null);
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

async function loadSnapshot() {
  if (refreshing.value) return;
  refreshing.value = true;
  error.value = '';
  try {
    snapshot.value = await diagnosticsApi.snapshot();
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

    <div v-else-if="snapshot" class="debug-scroll">
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
          <div><div class="text-caption text-medium-emphasis">{{ t('debugPage.databaseSize') }}</div><strong class="font-tabular">{{ formatBytes(snapshot.databases.reduce((sum, item) => sum + item.fileSizeBytes, 0)) }}</strong></div>
        </div>
        <div class="page-summary-card">
          <v-icon :color="snapshot.tasks.workerRunning ? 'success' : 'error'">mdi-clipboard-check-outline</v-icon>
          <div><div class="text-caption text-medium-emphasis">{{ t('debugPage.taskWorker') }}</div><strong>{{ snapshot.tasks.workerRunning ? t('debugPage.running') : t('debugPage.stopped') }}</strong></div>
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
        <v-card variant="outlined" class="debug-card">
          <v-card-title class="database-title">
            <span>{{ t('debugPage.taskRuntime') }}</span>
            <v-chip :color="snapshot.tasks.workerRunning ? 'success' : 'error'" size="small" variant="tonal">
              {{ snapshot.tasks.workerRunning ? t('debugPage.running') : t('debugPage.stopped') }}
            </v-chip>
          </v-card-title>
          <v-card-text><div class="info-grid"><div v-for="[label, value] in taskItems" :key="String(label)"><span>{{ label }}</span><strong>{{ value }}</strong></div></div></v-card-text>
        </v-card>
      </div>

      <v-card v-for="database in snapshot.databases" :key="database.name" variant="outlined" class="debug-card database-card">
        <v-card-title class="database-title">
          <span>{{ databaseTitle(database) }}</span>
          <v-chip :color="database.healthy ? 'success' : 'error'" size="small" variant="tonal">
            {{ database.healthy ? t('debugPage.healthy') : t('debugPage.unavailable') }}
          </v-chip>
        </v-card-title>
        <v-card-text>
          <v-alert v-if="database.errorCode" type="warning" variant="tonal" density="compact" class="mb-4">{{ diagnosticError(database.errorCode) }}</v-alert>
          <div class="info-grid database-info">
            <div><span>{{ t('debugPage.fileSize') }}</span><strong>{{ formatBytes(database.fileSizeBytes) }}</strong></div>
            <div><span>{{ t('debugPage.usedSpace') }}</span><strong>{{ formatBytes(database.usedBytes) }}</strong></div>
            <div><span>{{ t('debugPage.freeSpace') }}</span><strong>{{ formatBytes(database.freeBytes) }}</strong></div>
            <div><span>{{ t('debugPage.pages') }}</span><strong>{{ formatNumber(database.pageCount) }} × {{ formatBytes(database.pageSizeBytes) }}</strong></div>
            <div><span>{{ t('debugPage.openConnections') }}</span><strong>{{ database.connections.openConnections }} / {{ database.connections.maxOpenConnections }}</strong></div>
            <div><span>{{ t('debugPage.inUseIdle') }}</span><strong>{{ database.connections.inUse }} / {{ database.connections.idle }}</strong></div>
            <div><span>{{ t('debugPage.waitCount') }}</span><strong>{{ formatNumber(database.connections.waitCount) }}</strong></div>
            <div><span>{{ t('debugPage.waitDuration') }}</span><strong>{{ formatNanoseconds(database.connections.waitDurationNs) }}</strong></div>
          </div>
          <div class="table-heading">{{ t('debugPage.tables') }}</div>
          <v-table density="compact" class="debug-table">
            <thead><tr><th>{{ t('debugPage.tableName') }}</th><th class="text-right">{{ t('debugPage.rowCount') }}</th></tr></thead>
            <tbody>
              <tr v-if="database.tables.length === 0"><td colspan="2" class="text-center text-medium-emphasis">{{ t('debugPage.noTables') }}</td></tr>
              <tr v-for="table in database.tables" :key="table.name">
                <td class="mono">{{ table.name }}</td>
                <td class="text-right font-tabular">{{ table.errorCode ? diagnosticError(table.errorCode) : formatNumber(table.rowCount) }}</td>
              </tr>
            </tbody>
          </v-table>
        </v-card-text>
      </v-card>
    </div>
  </div>
</template>

<style scoped>
.debug-page { overflow: hidden; }
.debug-refresh-state, .database-title { display: flex; align-items: center; gap: 10px; }
.debug-scroll { min-height: 0; overflow: auto; display: grid; align-content: start; gap: 16px; padding-right: 2px; }
.debug-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 16px; }
.debug-card:hover { border-color: var(--lp-border) !important; box-shadow: var(--lp-shadow-sm) !important; }
.debug-card .v-card-title { padding: 16px 18px 12px; font-size: 1rem; font-weight: 700; }
.info-grid strong { display: block; margin-top: 4px; font-size: 13px; overflow-wrap: anywhere; }
.database-title { justify-content: space-between; }
.database-info { margin-bottom: 18px; }
.table-heading { margin: 4px 0 8px; font-weight: 700; }
.debug-table { border: 1px solid var(--lp-border); border-radius: 8px; }
.mono { font-family: ui-monospace, SFMono-Regular, Consolas, monospace; overflow-wrap: anywhere; }
@media (max-width: 900px) { .debug-grid { grid-template-columns: 1fr; } }
@media (max-width: 760px) {
  .debug-page { overflow: visible; }
  .debug-scroll { overflow: visible; }
  .page-toolbar { align-items: flex-start; }
  .debug-refresh-state { align-items: flex-start; flex-direction: column; }
}
</style>
