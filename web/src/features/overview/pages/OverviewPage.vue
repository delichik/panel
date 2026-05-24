<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { useTheme } from 'vuetify';
import { useRouter } from 'vue-router';
import VChart from 'vue-echarts';
import { use } from 'echarts/core';
import { CanvasRenderer } from 'echarts/renderers';
import { LineChart } from 'echarts/charts';
import { GridComponent, LegendComponent, TooltipComponent } from 'echarts/components';
import { overviewApi } from '@/api/overview';
import { tasksApi } from '@/api/tasks';
import { applicationsApi } from '@/api/applications';
import type { ApplicationDto, MetricsRange, MetricsSeriesDto, OverviewDto, OverviewServerDto, TaskDto } from '@/types/api';

use([CanvasRenderer, LineChart, GridComponent, TooltipComponent, LegendComponent]);

const router = useRouter();
const theme = useTheme();
const isDark = computed(() => theme.global.current.value.dark);

const chartTextStyle = computed(() => ({
  color: isDark.value ? '#94a3b8' : '#64748b',
  fontSize: 11
}));

const chartLineStyle = computed(() => ({
  color: isDark.value ? 'rgba(255, 255, 255, 0.08)' : 'rgba(0, 0, 0, 0.06)'
}));

const overview = ref<OverviewDto>({ servers: [] });
const selectedServerId = ref('');
const range = ref<MetricsRange>('1h');
const metrics = ref<MetricsSeriesDto | null>(null);
const loading = ref(false);
const metricsLoading = ref(false);
const error = ref('');
const metricsError = ref('');

const applications = ref<ApplicationDto[]>([]);

// Task Ticker States
const activeTasks = ref<TaskDto[]>([]);
const taskIndex = ref(0);
let refreshTimer: number | undefined;
let taskTimer: number | undefined;
let rotateTimer: number | undefined;

const selectedServer = computed<OverviewServerDto | undefined>(() =>
  overview.value.servers.find((server) => server.id === selectedServerId.value),
);

const currentTickerTask = computed(() =>
  activeTasks.value.length > 0 ? activeTasks.value[taskIndex.value % activeTasks.value.length] : null
);

// Applications needing attention from Nomad runtime state.
const applicationAttentionCount = computed(() => {
  return applications.value.filter((app) => {
    const unhealthy = ['failed', 'pending', 'unknown'].includes(app.runtimeStatus || '');
    return unhealthy || Boolean(app.lastError);
  }).length;
});

// Extract 1-min load average from raw loadAverage string (e.g. "0.15 0.08 0.02")
function getOneMinLoad(loadAverage: string | null | undefined): string {
  if (!loadAverage) return '-';
  const parts = loadAverage.trim().split(/\s+/);
  return parts[0] || '-';
}

// Formatting Helpers
function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  const value = bytes / Math.pow(k, i);
  return `${value.toFixed(1)} ${sizes[i]}`;
}

function formatBytesPerSecond(bytesPerSecond: number): string {
  if (bytesPerSecond === 0) return '0 B/s';
  const k = 1024;
  const sizes = ['B/s', 'KB/s', 'MB/s', 'GB/s', 'TB/s'];
  const i = Math.floor(Math.log(bytesPerSecond) / Math.log(k));
  const value = bytesPerSecond / Math.pow(k, i);
  return `${value.toFixed(1)} ${sizes[i]}`;
}

function lineOption(name: string, data: Array<[string, number, number?, number?]>, unit = '') {
  return {
    tooltip: {
      trigger: 'axis',
      backgroundColor: isDark.value ? '#111827' : '#ffffff',
      borderColor: isDark.value ? 'rgba(255, 255, 255, 0.06)' : 'rgba(0, 0, 0, 0.06)',
      textStyle: {
        color: isDark.value ? '#f8fafc' : '#0f172a'
      },
      formatter: (params: any) => {
        const item = params[0];
        const timeStr = new Date(item.value[0]).toLocaleTimeString();
        const rawPercent = item.value[1];
        const percent = typeof rawPercent === 'number' ? rawPercent.toFixed(1) : rawPercent;
        const usedBytes = item.value[2];
        const totalBytes = item.value[3];

        let valDisplay = `${percent}${unit}`;
        if (usedBytes !== undefined && totalBytes !== undefined) {
          valDisplay = `${percent}${unit} (${formatBytes(usedBytes)} / ${formatBytes(totalBytes)})`;
        }

        return `<div class="font-weight-bold mb-1">${timeStr}</div>
                <div class="d-flex align-center justify-space-between" style="gap: 16px;">
                  <span>
                    <span style="display:inline-block;margin-right:4px;border-radius:10px;width:10px;height:10px;background-color:${item.color};"></span>
                    ${item.seriesName}
                  </span>
                  <span class="font-weight-bold">${valDisplay}</span>
                </div>`;
      }
    },
    grid: { left: 44, right: 20, top: 24, bottom: 32 },
    xAxis: {
      type: 'time',
      axisLabel: {
        color: chartTextStyle.value.color,
        fontSize: chartTextStyle.value.fontSize
      },
      axisLine: {
        lineStyle: {
          color: chartLineStyle.value.color
        }
      }
    },
    yAxis: {
      type: 'value',
      axisLabel: {
        formatter: (val: number) => `${Number(val).toFixed(1)}${unit}`,
        color: chartTextStyle.value.color,
        fontSize: chartTextStyle.value.fontSize
      },
      splitLine: {
        lineStyle: {
          color: chartLineStyle.value.color
        }
      }
    },
    series: [{
      name,
      type: 'line',
      smooth: true,
      symbol: 'none',
      data,
      lineStyle: {
        color: '#6366f1'
      }
    }],
  };
}

const cpuOption = computed(() =>
  lineOption('CPU', metrics.value?.cpu.map((p) => [p.time, p.usagePercent]) ?? [], '%'),
);

const memoryOption = computed(() =>
  lineOption(
    'Memory',
    metrics.value?.memory.map((p) => [p.time, Math.round((p.usedBytes / p.totalBytes) * 100), p.usedBytes, p.totalBytes]) ?? [],
    '%',
  ),
);

const diskOption = computed(() =>
  lineOption(
    'Disk',
    metrics.value?.disk.map((p) => [p.time, Math.round((p.usedBytes / p.totalBytes) * 100), p.usedBytes, p.totalBytes]) ?? [],
    '%',
  ),
);

const networkOption = computed(() => ({
  tooltip: {
    trigger: 'axis',
    backgroundColor: isDark.value ? '#151b2c' : '#ffffff',
    borderColor: isDark.value ? 'rgba(255, 255, 255, 0.12)' : 'rgba(0, 0, 0, 0.12)',
    textStyle: {
      color: isDark.value ? '#f8fafc' : '#0f172a'
    },
    formatter: (params: any) => {
      let res = `<div class="font-weight-bold mb-1">${new Date(params[0].value[0]).toLocaleTimeString()}</div>`;
      params.forEach((item: any) => {
        const val = formatBytesPerSecond(item.value[1]);
        res += `<div class="d-flex align-center justify-space-between" style="gap: 16px; margin-top: 4px;">
          <span>
            <span style="display:inline-block;margin-right:4px;border-radius:10px;width:10px;height:10px;background-color:${item.color};"></span>
            ${item.seriesName}
          </span>
          <span class="font-weight-bold">${val}</span>
        </div>`;
      });
      return res;
    }
  },
  legend: {
    top: 0,
    textStyle: {
      color: chartTextStyle.value.color
    }
  },
  grid: { left: 64, right: 20, top: 34, bottom: 32 },
  xAxis: {
    type: 'time',
    axisLabel: {
      color: chartTextStyle.value.color,
      fontSize: chartTextStyle.value.fontSize
    },
    axisLine: {
      lineStyle: {
        color: chartLineStyle.value.color
      }
    }
  },
  yAxis: {
    type: 'value',
    axisLabel: {
      formatter: (value: number) => formatBytesPerSecond(value),
      color: chartTextStyle.value.color,
      fontSize: chartTextStyle.value.fontSize
    },
    splitLine: {
      lineStyle: {
        color: chartLineStyle.value.color
      }
    }
  },
  series: [
    {
      name: 'RX Bandwidth',
      type: 'line',
      smooth: true,
      symbol: 'none',
      data: metrics.value?.network.map((p) => [p.time, p.rxBytesPerSecond]) ?? [],
      lineStyle: { color: '#6366f1' }
    },
    {
      name: 'TX Bandwidth',
      type: 'line',
      smooth: true,
      symbol: 'none',
      data: metrics.value?.network.map((p) => [p.time, p.txBytesPerSecond]) ?? [],
      lineStyle: { color: '#10b981' }
    },
  ],
}));

async function loadOverview() {
  loading.value = true;
  try {
    overview.value = await overviewApi.getOverview();
    if (!selectedServerId.value && overview.value.servers.length) {
      selectedServerId.value = overview.value.servers[0].id;
    }
    error.value = '';
  } catch (err) {
    error.value = err instanceof Error ? err.message : 'Unable to load overview';
  } finally {
    loading.value = false;
  }
}

async function loadMetrics() {
  if (!selectedServerId.value) {
    metrics.value = null;
    return;
  }
  metricsLoading.value = true;
  try {
    metrics.value = await overviewApi.getMetrics(selectedServerId.value, range.value);
    metricsError.value = '';
  } catch (err) {
    metrics.value = null;
    metricsError.value = err instanceof Error ? err.message : 'Unable to load metrics';
  } finally {
    metricsLoading.value = false;
  }
}

async function loadApplications() {
  try {
    applications.value = await applicationsApi.list();
  } catch (err) {
    console.error('Failed to load applications', err);
  }
}

async function loadActiveTasks() {
  try {
    const [running, queued] = await Promise.all([
      tasksApi.list({ status: 'running', pageSize: 10 }),
      tasksApi.list({ status: 'queued', pageSize: 10 }),
    ]);
    activeTasks.value = [...running.items, ...queued.items];
    if (taskIndex.value >= activeTasks.value.length) {
      taskIndex.value = 0;
    }
  } catch {
    activeTasks.value = [];
  }
}

watch([selectedServerId, range], loadMetrics);

onMounted(async () => {
  await loadOverview();
  await loadMetrics();
  await loadApplications();
  await loadActiveTasks();

  refreshTimer = window.setInterval(async () => {
    await loadOverview();
    await loadMetrics();
    await loadApplications();
  }, 15000);

  taskTimer = window.setInterval(loadActiveTasks, 8000);
  rotateTimer = window.setInterval(() => {
    if (activeTasks.value.length > 1) {
      taskIndex.value = (taskIndex.value + 1) % activeTasks.value.length;
    }
  }, 3500);
});

onBeforeUnmount(() => {
  if (refreshTimer) window.clearInterval(refreshTimer);
  if (taskTimer) window.clearInterval(taskTimer);
  if (rotateTimer) window.clearInterval(rotateTimer);
});
</script>

<template>
  <div class="overview-page-wrapper d-flex flex-column h-100 overflow-hidden" style="height: calc(100vh - 120px);">

    <!-- Title & Active Tasks Ticker in the Header -->
    <div class="overview-header-container mb-6 flex-shrink-0">
      <div class="d-flex justify-space-between align-center py-3 px-4 rounded-lg header-ticker-card">
        <div class="ticker-content d-flex align-center flex-grow-1 overflow-hidden">
          <span class="status-indicator mr-3">
            <span class="status-pulse" :class="activeTasks.length > 0 ? 'processing' : 'operational'"></span>
          </span>

          <div class="ticker-text-wrapper flex-grow-1 overflow-hidden">
            <Transition name="slide-y" mode="out-in">
              <div :key="currentTickerTask?.id || 'idle'" class="ticker-item text-body-2 font-weight-medium">
                <template v-if="currentTickerTask">
                  <span class="text-primary font-weight-bold mr-1">[Task #{{ currentTickerTask.id.slice(0, 8) }}]</span>
                  <span>{{ currentTickerTask.summary || currentTickerTask.type }}</span>
                  <v-chip size="x-small" color="primary" class="ml-2 font-weight-bold" label>
                    {{ currentTickerTask.status }}
                  </v-chip>
                  <span class="text-medium-emphasis ml-2">{{ currentTickerTask.stage || 'queued' }}</span>
                </template>
                <template v-else>
                  <span class="text-high-emphasis font-weight-bold text-subtitle-1">Overview</span>
                  <span class="separator mx-2">•</span>
                  <span class="text-success font-weight-medium">All systems operational</span>
                  <span class="separator mx-2">•</span>
                  <span class="text-medium-emphasis">No active tasks</span>
                </template>
              </div>
            </Transition>
          </div>
        </div>

        <v-btn
          prepend-icon="mdi-refresh"
          :loading="loading"
          variant="flat"
          color="primary"
          @click="loadOverview"
          class="text-none font-weight-bold ml-4 flex-shrink-0"
          size="comfortable"
        >
          Refresh
        </v-btn>
      </div>
    </div>

    <v-alert v-if="error" type="error" variant="tonal" class="mb-4 flex-shrink-0">{{ error }}</v-alert>

    <!-- Onboarding empty state guidance -->
    <div v-if="!loading && overview.servers.length === 0" class="flex-grow-1 min-height-0 overflow-y-auto">
      <v-card class="pa-8 d-flex flex-column align-center justify-center text-center h-100" variant="outlined">
        <div class="onboarding-illustration mb-6">
          <div class="pulsing-server-icon">
            <v-icon size="64" color="primary">mdi-server-network</v-icon>
            <div class="pulse-ring"></div>
          </div>
        </div>

        <h2 class="text-h5 font-weight-bold mb-2">Connect Your First SSH Server</h2>
        <p class="text-body-1 text-medium-emphasis mb-8 max-width-600">
          Welcome to your Linux Panel control plane! To unlock detailed health metrics, automated package updates, container resource visualization, and easy application deployments, let's get your first server hooked up.
        </p>

        <div class="features-checklist text-left mb-8 px-5 py-6 rounded-lg bg-surface-variant max-width-600 w-100">
          <div class="text-subtitle-1 font-weight-bold mb-4 d-flex align-center">
            <v-icon color="primary" class="mr-2">mdi-check-decagram</v-icon>
            What you can do once connected:
          </div>

          <div class="d-flex align-start mb-3">
            <v-icon color="success" class="mr-3 mt-1" size="small">mdi-checkbox-marked-circle</v-icon>
            <div>
              <div class="font-weight-bold text-body-2 text-high-emphasis">Real-Time Resource Telemetry</div>
              <div class="text-caption text-medium-emphasis">Monitor CPU utilization, memory allocations, disk health, and real-time network throughput charts.</div>
            </div>
          </div>

          <div class="d-flex align-start mb-3">
            <v-icon color="success" class="mr-3 mt-1" size="small">mdi-checkbox-marked-circle</v-icon>
            <div>
              <div class="font-weight-bold text-body-2 text-high-emphasis">Applications Management</div>
              <div class="text-caption text-medium-emphasis">Render Nomad jobs, deploy applications, and inspect runtime state from one workspace.</div>
            </div>
          </div>

          <div class="d-flex align-start">
            <v-icon color="success" class="mr-3 mt-1" size="small">mdi-checkbox-marked-circle</v-icon>
            <div>
              <div class="font-weight-bold text-body-2 text-high-emphasis">Automated Package Upgrades</div>
              <div class="text-caption text-medium-emphasis">Audit pending system packages, check security patches, and trigger upgrades securely through background tasks.</div>
            </div>
          </div>
        </div>

        <v-btn
          color="primary"
          size="large"
          prepend-icon="mdi-plus"
          class="text-none font-weight-bold px-8 shadow-glow"
          @click="$router.push('/servers')"
        >
          Add SSH Target Server
        </v-btn>
      </v-card>
    </div>

    <!-- Live telemetry dashboard layout -->
    <div v-else class="overview-grid flex-grow-1 min-height-0">

      <!-- Left Sidebar (Servers List - pure style as requested) -->
      <v-card class="d-flex flex-column h-100 overflow-hidden" variant="outlined" :loading="loading">
        <v-card-item class="bg-surface-variant py-3 flex-shrink-0">
          <div class="d-flex justify-space-between align-center">
            <v-card-title class="text-subtitle-1 font-weight-bold my-0 py-0">Servers</v-card-title>
            <v-chip size="small" color="primary">{{ overview.servers.length }}</v-chip>
          </div>
        </v-card-item>

        <v-card-text class="flex-grow-1 overflow-y-auto pa-3">
          <div class="server-cards d-flex flex-column" style="gap: 10px;">
            <div
              v-for="server in overview.servers"
              :key="server.id"
              class="server-item"
              :class="{ 'selected': server.id === selectedServerId }"
              @click="selectedServerId = server.id"
            >
              <div class="d-flex justify-space-between align-center mb-1">
                <div class="d-flex align-center overflow-hidden">
                  <span class="status-pulse" :class="server.reachable ? 'online' : 'offline'"></span>
                  <span class="text-subtitle-2 font-weight-bold text-high-emphasis text-truncate">{{ server.name }}</span>
                </div>

                <div class="text-caption font-weight-bold text-medium-emphasis flex-shrink-0 ml-2 font-tabular">
                  Load: {{ getOneMinLoad(server.loadAverage) }}
                </div>
              </div>
            </div>
          </div>
        </v-card-text>
      </v-card>

      <!-- Right Container (Metrics / Charts / Alerts & Updates) -->
      <v-card class="d-flex flex-column h-100 overflow-hidden" variant="outlined" :loading="metricsLoading">
        <v-card-item class="bg-surface-variant py-3 flex-shrink-0">
          <div class="d-flex justify-space-between align-center">
            <div class="overflow-hidden mr-4">
              <v-card-title class="text-subtitle-1 font-weight-bold text-truncate">{{ selectedServer?.name || 'Select a server' }}</v-card-title>
              <v-card-subtitle class="text-caption text-truncate">
                Last metrics: {{ selectedServer?.lastMetricsAt ? new Date(selectedServer.lastMetricsAt).toLocaleString() : 'never' }}
              </v-card-subtitle>
            </div>
            <v-btn-toggle v-model="range" mandatory color="primary" density="compact" class="flex-shrink-0">
              <v-btn value="1h" class="text-none">1h</v-btn>
              <v-btn value="6h" class="text-none">6h</v-btn>
              <v-btn value="24h" class="text-none">24h</v-btn>
            </v-btn-toggle>
          </div>
        </v-card-item>

        <v-card-text class="flex-grow-1 overflow-y-auto pa-4">
          <v-alert v-if="metricsError" type="error" variant="tonal" class="mb-4">{{ metricsError }}</v-alert>

          <!-- Core Actionable Banners for Package and Container Updates -->
          <div v-if="selectedServer" class="update-banners-container mb-4">
            <div
              v-if="selectedServer.packageUpdateCount > 0 || applicationAttentionCount > 0"
              class="d-flex flex-column flex-md-row"
              style="gap: 16px;"
            >
              <!-- System Packages Updates Card -->
              <div
                v-if="selectedServer.packageUpdateCount > 0"
                class="update-banner-card flex-grow-1 d-flex align-center justify-space-between px-4 py-3 rounded-lg cursor-pointer"
                @click="router.push('/packages')"
              >
                <div class="d-flex align-center mr-3">
                  <v-icon color="warning" class="mr-3" size="default">mdi-package-variant-plus</v-icon>
                  <div>
                    <div class="text-subtitle-2 font-weight-bold text-high-emphasis">System Updates Available</div>
                    <div class="text-caption text-medium-emphasis">
                      There are <strong>{{ selectedServer.packageUpdateCount }}</strong> package updates pending installation.
                    </div>
                  </div>
                </div>
                <v-btn
                  variant="tonal"
                  color="warning"
                  size="small"
                  class="text-none font-weight-bold flex-shrink-0"
                >
                  Upgrade Now
                </v-btn>
              </div>

              <!-- Applications Attention Card -->
              <div
                v-if="applicationAttentionCount > 0"
                class="update-banner-card drifted flex-grow-1 d-flex align-center justify-space-between px-4 py-3 rounded-lg cursor-pointer"
                @click="router.push('/applications')"
              >
                <div class="d-flex align-center mr-3">
                  <v-icon color="primary" class="mr-3" size="default">mdi-apps</v-icon>
                  <div>
                    <div class="text-subtitle-2 font-weight-bold text-high-emphasis">Applications Attention</div>
                    <div class="text-caption text-medium-emphasis">
                      There are <strong>{{ applicationAttentionCount }}</strong> applications requiring attention.
                    </div>
                  </div>
                </div>
                <v-btn
                  variant="tonal"
                  color="primary"
                  size="small"
                  class="text-none font-weight-bold flex-shrink-0"
                >
                  Open Applications
                </v-btn>
              </div>
            </div>
          </div>

          <v-alert
            v-if="selectedServer && !selectedServer.metricsFresh"
            type="warning"
            variant="tonal"
            class="mb-4"
          >
            Metrics are stale or not collected yet
          </v-alert>

          <div v-if="metrics" class="chart-grid">
            <div class="chart-card">
              <div class="text-subtitle-2 font-weight-bold text-center mb-1">CPU Usage</div>
              <VChart class="chart" :option="cpuOption" autoresize />
            </div>
            <div class="chart-card">
              <div class="text-subtitle-2 font-weight-bold text-center mb-1">Memory Usage</div>
              <VChart class="chart" :option="memoryOption" autoresize />
            </div>
            <div class="chart-card">
              <div class="text-subtitle-2 font-weight-bold text-center mb-1">Disk Usage</div>
              <VChart class="chart" :option="diskOption" autoresize />
            </div>
            <div class="chart-card">
              <div class="text-subtitle-2 font-weight-bold text-center mb-1">Network Bandwidth</div>
              <VChart class="chart" :option="networkOption" autoresize />
            </div>
          </div>
        </v-card-text>
      </v-card>
    </div>
  </div>
</template>

<style scoped>
/* Page Layout Constraints */
.overview-grid {
  display: grid;
  grid-template-columns: 340px 1fr;
  gap: 24px;
  min-height: 0;
}

/* Update Banner Cards */
.update-banners-container {
  width: 100%;
}

.update-banner-card {
  border: 1px solid rgba(var(--v-border-color), 0.06);
  background: rgba(var(--v-theme-warning), 0.05);
  transition: all 0.2s ease;
  border-left: 4px solid rgb(var(--v-theme-warning));
  border-radius: 10px;
}

.update-banner-card:hover {
  background: rgba(var(--v-theme-warning), 0.08);
  box-shadow: 0 2px 8px rgba(var(--v-theme-warning), 0.1);
  transform: translateY(-1px);
}

.update-banner-card.drifted {
  background: rgba(var(--v-theme-primary), 0.05);
  border-left-color: rgb(var(--v-theme-primary));
}

.update-banner-card.drifted:hover {
  background: rgba(var(--v-theme-primary), 0.08);
  box-shadow: 0 2px 8px rgba(var(--v-theme-primary), 0.1);
}

/* Onboarding empty state styling */
.max-width-600 {
  max-width: 600px;
}

.pulsing-server-icon {
  position: relative;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 120px;
  height: 120px;
  border-radius: 50%;
  background: rgba(var(--v-theme-primary), 0.08);
}

.pulse-ring {
  position: absolute;
  width: 120px;
  height: 120px;
  border: 2px solid rgba(var(--v-theme-primary), 0.2);
  border-radius: 50%;
  animation: pulse-ring-anim 2.5s infinite;
}

@keyframes pulse-ring-anim {
  0% {
    transform: scale(0.85);
    opacity: 0.8;
  }
  100% {
    transform: scale(1.3);
    opacity: 0;
  }
}

.shadow-glow {
  box-shadow: 0 4px 16px rgba(var(--v-theme-primary), 0.3) !important;
  transition: all 0.25s ease;
}

.shadow-glow:hover {
  transform: translateY(-2px);
  box-shadow: 0 6px 20px rgba(var(--v-theme-primary), 0.45) !important;
}

/* Header Active Task Ticker */
.header-ticker-card {
  border: 1px solid rgba(var(--v-border-color), 0.06);
  background: rgba(var(--v-theme-surface-variant), 0.15);
  backdrop-filter: blur(8px);
  height: 64px;
  border-radius: 12px;
}

.status-indicator {
  display: flex;
  align-items: center;
  justify-content: center;
}

.status-pulse {
  width: 10px;
  height: 10px;
  border-radius: 50%;
  transition: all 0.3s ease;
}

.status-pulse.operational {
  background-color: #10b981;
  box-shadow: 0 0 10px rgba(16, 185, 129, 0.4);
}

.status-pulse.processing {
  background-color: rgb(var(--v-theme-primary));
  box-shadow: 0 0 0 0 rgba(var(--v-theme-primary), 0.4);
  animation: pulse-primary 1.5s infinite;
}

@keyframes pulse-primary {
  0% {
    transform: scale(0.95);
    box-shadow: 0 0 0 0 rgba(var(--v-theme-primary), 0.7);
  }
  70% {
    transform: scale(1);
    box-shadow: 0 0 0 8px rgba(var(--v-theme-primary), 0);
  }
  100% {
    transform: scale(0.95);
    box-shadow: 0 0 0 0 rgba(var(--v-theme-primary), 0);
  }
}

/* Custom Sliding Transitions */
.slide-y-enter-active,
.slide-y-leave-active {
  transition: transform 0.4s cubic-bezier(0.4, 0, 0.2, 1), opacity 0.3s ease;
}

.slide-y-enter-from {
  transform: translateY(20px);
  opacity: 0;
}

.slide-y-leave-to {
  transform: translateY(-20px);
  opacity: 0;
}

.ticker-text-wrapper {
  position: relative;
  height: 24px;
  display: flex;
  align-items: center;
}

.ticker-item {
  position: absolute;
  left: 0;
  width: 100%;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.separator {
  opacity: 0.4;
}

/* Sleek Server List Items */
.server-item {
  position: relative;
  border: 1px solid rgba(var(--v-border-color), 0.06);
  border-radius: 10px;
  transition: all 0.25s cubic-bezier(0.4, 0, 0.2, 1);
  padding: 12px 14px;
  cursor: pointer;
  background: rgba(var(--v-theme-surface), 0.4);
}

.server-item:hover {
  transform: translateY(-1px);
  border-color: rgba(var(--v-theme-primary), 0.25);
  box-shadow: 0 4px 12px rgba(var(--v-theme-primary), 0.05);
}

.server-item.selected {
  border-color: rgb(var(--v-theme-primary));
  background-color: rgba(var(--v-theme-primary), 0.04);
  box-shadow: 0 4px 16px rgba(var(--v-theme-primary), 0.08);
}

.server-item.selected::before {
  content: '';
  position: absolute;
  left: 0;
  top: 12px;
  bottom: 12px;
  width: 4px;
  background-color: rgb(var(--v-theme-primary));
  border-top-right-radius: 4px;
  border-bottom-right-radius: 4px;
}

.server-item .status-pulse {
  width: 8px;
  height: 8px;
  margin-right: 8px;
  display: inline-block;
  border-radius: 50%;
}

.server-item .status-pulse.online {
  background-color: #10b981;
  box-shadow: 0 0 0 0 rgba(16, 185, 129, 0.4);
  animation: pulse-green 2s infinite;
}

.server-item .status-pulse.offline {
  background-color: #ef4444;
}

@keyframes pulse-green {
  0% {
    transform: scale(0.95);
    box-shadow: 0 0 0 0 rgba(16, 185, 129, 0.7);
  }
  70% {
    transform: scale(1);
    box-shadow: 0 0 0 6px rgba(16, 185, 129, 0);
  }
  100% {
    transform: scale(0.95);
    box-shadow: 0 0 0 0 rgba(16, 185, 129, 0);
  }
}

/* Charts and Grid Layouts */
.chart-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(260px, 1fr));
  gap: 16px;
}

.chart-card {
  border: 1px solid rgba(var(--v-border-color), 0.06);
  border-radius: 14px;
  padding: 16px;
  background: rgb(var(--v-theme-surface));
  transition: all 0.25s cubic-bezier(0.4, 0, 0.2, 1);
}

.chart-card:hover {
  border-color: rgba(var(--v-theme-primary), 0.15);
  box-shadow: 0 10px 15px -3px rgba(0, 0, 0, 0.04);
}

.chart {
  height: 260px;
  width: 100%;
}
</style>
