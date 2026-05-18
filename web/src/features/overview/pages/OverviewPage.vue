<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import VChart from 'vue-echarts';
import { use } from 'echarts/core';
import { CanvasRenderer } from 'echarts/renderers';
import { LineChart } from 'echarts/charts';
import { GridComponent, LegendComponent, TooltipComponent } from 'echarts/components';
import { Refresh } from '@element-plus/icons-vue';
import { overviewApi } from '@/api/overview';
import type { MetricsRange, MetricsSeriesDto, OverviewDto, OverviewServerDto } from '@/types/api';

use([CanvasRenderer, LineChart, GridComponent, TooltipComponent, LegendComponent]);

const overview = ref<OverviewDto>({ servers: [] });
const selectedServerId = ref('');
const range = ref<MetricsRange>('1h');
const metrics = ref<MetricsSeriesDto | null>(null);
const loading = ref(false);
const metricsLoading = ref(false);
const error = ref('');
const metricsError = ref('');
let refreshTimer: number | undefined;

const selectedServer = computed<OverviewServerDto | undefined>(() =>
  overview.value.servers.find((server) => server.id === selectedServerId.value),
);

function lineOption(name: string, data: Array<[string, number]>, unit = '') {
  return {
    tooltip: { trigger: 'axis' },
    grid: { left: 44, right: 20, top: 24, bottom: 32 },
    xAxis: { type: 'time' },
    yAxis: { type: 'value', axisLabel: { formatter: `{value}${unit}` } },
    series: [{ name, type: 'line', smooth: true, symbol: 'none', data }],
  };
}

const cpuOption = computed(() =>
  lineOption('CPU', metrics.value?.cpu.map((p) => [p.time, p.usagePercent]) ?? [], '%'),
);
const memoryOption = computed(() =>
  lineOption(
    'Memory',
    metrics.value?.memory.map((p) => [p.time, Math.round((p.usedBytes / p.totalBytes) * 100)]) ?? [],
    '%',
  ),
);
const diskOption = computed(() =>
  lineOption(
    'Disk',
    metrics.value?.disk.map((p) => [p.time, Math.round((p.usedBytes / p.totalBytes) * 100)]) ?? [],
    '%',
  ),
);
const networkOption = computed(() => ({
  tooltip: { trigger: 'axis' },
  legend: { top: 0 },
  grid: { left: 54, right: 20, top: 34, bottom: 32 },
  xAxis: { type: 'time' },
  yAxis: { type: 'value' },
  series: [
    {
      name: 'RX B/s',
      type: 'line',
      smooth: true,
      symbol: 'none',
      data: metrics.value?.network.map((p) => [p.time, p.rxBytesPerSecond]) ?? [],
    },
    {
      name: 'TX B/s',
      type: 'line',
      smooth: true,
      symbol: 'none',
      data: metrics.value?.network.map((p) => [p.time, p.txBytesPerSecond]) ?? [],
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

watch([selectedServerId, range], loadMetrics);
onMounted(async () => {
  await loadOverview();
  await loadMetrics();
  refreshTimer = window.setInterval(async () => {
    await loadOverview();
    await loadMetrics();
  }, 15000);
});

onBeforeUnmount(() => {
  if (refreshTimer) window.clearInterval(refreshTimer);
});
</script>

<template>
  <div>
    <div class="panel-header panel">
      <div>
        <p class="page-subtitle">Server health, update pressure, and recent resource telemetry.</p>
      </div>
      <el-button :icon="Refresh" :loading="loading" @click="loadOverview">Refresh</el-button>
    </div>

    <el-alert v-if="error" class="page-alert" type="error" :title="error" show-icon />

    <el-empty v-if="!loading && overview.servers.length === 0" description="No servers registered yet" />

    <div v-else class="overview-grid">
      <section class="panel server-list" v-loading="loading">
        <div class="panel-header">
          <strong>Servers</strong>
          <el-tag>{{ overview.servers.length }}</el-tag>
        </div>
        <div class="server-cards">
          <button
            v-for="server in overview.servers"
            :key="server.id"
            class="server-card"
            :class="{ active: server.id === selectedServerId }"
            @click="selectedServerId = server.id"
          >
            <div class="server-name">{{ server.name }}</div>
            <div class="muted">{{ server.host }}</div>
            <div class="server-flags">
              <el-tag :type="server.reachable ? 'success' : 'danger'" size="small">
                {{ server.reachable ? 'reachable' : 'offline' }}
              </el-tag>
              <el-tag :type="server.metricsFresh ? 'success' : 'warning'" size="small">
                {{ server.metricsFresh ? 'fresh' : 'stale' }}
              </el-tag>
              <el-tag :type="server.packageUpdateCount > 0 ? 'warning' : 'info'" size="small">
                {{ server.packageUpdateCount }} updates
              </el-tag>
            </div>
          </button>
        </div>
      </section>

      <section class="panel charts" v-loading="metricsLoading">
        <div class="panel-header">
          <div>
            <strong>{{ selectedServer?.name || 'Select a server' }}</strong>
            <div class="muted">
              Last metrics: {{ selectedServer?.lastMetricsAt || 'never' }}
            </div>
          </div>
          <el-radio-group v-model="range" size="small">
            <el-radio-button label="1h">1h</el-radio-button>
            <el-radio-button label="6h">6h</el-radio-button>
            <el-radio-button label="24h">24h</el-radio-button>
          </el-radio-group>
        </div>
        <el-alert v-if="metricsError" class="panel-alert" type="error" :title="metricsError" show-icon />
        <el-alert
          v-else-if="selectedServer && !selectedServer.metricsFresh"
          class="panel-alert"
          type="warning"
          title="Metrics are stale or not collected yet"
          show-icon
        />
        <div v-if="metrics" class="chart-grid">
          <VChart class="chart" :option="cpuOption" autoresize />
          <VChart class="chart" :option="memoryOption" autoresize />
          <VChart class="chart" :option="diskOption" autoresize />
          <VChart class="chart" :option="networkOption" autoresize />
        </div>
      </section>
    </div>
  </div>
</template>

<style scoped>
.page-alert,
.panel-alert {
  margin-top: 16px;
}

.overview-grid {
  display: grid;
  grid-template-columns: 340px 1fr;
  gap: 20px;
  margin-top: 20px;
}

.server-cards {
  display: grid;
  gap: 10px;
  padding: 14px;
}

.server-card {
  width: 100%;
  padding: 14px;
  text-align: left;
  border: 1px solid #dfe4ea;
  border-radius: 8px;
  background: #fff;
  cursor: pointer;
}

.server-card.active {
  border-color: #409eff;
  box-shadow: inset 3px 0 0 #409eff;
}

.server-name {
  font-weight: 700;
}

.server-flags {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 12px;
}

.chart-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(260px, 1fr));
  gap: 16px;
  padding: 20px;
}

.chart {
  height: 260px;
  border: 1px solid #edf0f3;
  border-radius: 8px;
}
</style>
