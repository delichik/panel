<script setup lang="ts">
import { computed, defineAsyncComponent, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { RefreshCcw } from '@lucide/vue';
import { serversApi, type ServerMetricsRange, type ServerMetricsSeries } from '@/api/servers';
import AutoRefreshControl from '@/components/patterns/AutoRefreshControl.vue';
import Button from '@/components/ui/Button.vue';
import EmptyState from '@/components/ui/EmptyState.vue';
import LoadingOverlay from '@/components/ui/LoadingOverlay.vue';
import Select from '@/components/ui/Select.vue';
import { useErrorToast } from '@/components/ui/toast';
import { useAutoRefresh } from '@/composables/useAutoRefresh';
import { useI18n } from '@/i18n';
import type { ServerDto } from '@/types/servers';
import { formatDateTime } from '@/utils/datetime';
import { createLatestRequestGuard, isAbortError } from '@/views/_shared/requestState';

const props = defineProps<{ server: ServerDto; active: boolean }>();
const { t } = useI18n();
const notifyError = useErrorToast();
const MetricLineChart = defineAsyncComponent(() => import('@/views/overview/MetricLineChart.vue'));
const { mode: autoRefreshMode, enabled: autoRefreshEnabled, intervalMs: autoRefreshIntervalMs } = useAutoRefresh();

const metrics = ref<ServerMetricsSeries | null>(null);
const loading = ref(false);
const error = ref('');
const range = ref<ServerMetricsRange>('1h');
const rangeOptions = [
  { value: '1h', label: '1h' },
  { value: '6h', label: '6h' },
  { value: '1d', label: '1d' },
  { value: '7d', label: '7d' },
];

const metricsGuard = createLatestRequestGuard();
let metricsController: AbortController | null = null;
let inFlight = false;
let timer: number | undefined;

const latest = computed(() => {
  const series = metrics.value;
  return {
    cpu: series?.cpu.at(-1),
    memory: series?.memory.at(-1),
    disk: series?.disk.at(-1),
    network: series?.network.at(-1),
    time: series?.cpu.at(-1)?.time ?? series?.memory.at(-1)?.time ?? '',
  };
});

/** 有采样点才渲染图表区；空数组视为尚未收到指标。 */
const hasSamples = computed(() => {
  const series = metrics.value;
  return Boolean(series && (series.cpu.length || series.memory.length || series.disk.length || series.network.length));
});

const panels = computed(() => {
  const series = metrics.value;
  if (!series) return [];
  const single = (values: number[]) => [{ id: props.server.id, name: props.server.name, values }];
  const percentValues = (points: Array<{ usedBytes?: number; totalBytes?: number }>) => points.map((point) => (point.totalBytes ? ((point.usedBytes ?? 0) / point.totalBytes) * 100 : 0));
  return [
    { key: 'cpu', label: 'CPU', current: percent(latest.value.cpu?.usagePercent), valueKind: 'percent' as const, labels: series.cpu.map((point) => point.time), series: single(series.cpu.map((point) => point.usagePercent)) },
    { key: 'memory', label: t('serversPage.memory'), current: `${bytes(latest.value.memory?.usedBytes)} / ${bytes(latest.value.memory?.totalBytes)}`, valueKind: 'percent' as const, labels: series.memory.map((point) => point.time), series: single(percentValues(series.memory)) },
    { key: 'disk', label: t('serversPage.disk'), current: `${bytes(latest.value.disk?.usedBytes)} / ${bytes(latest.value.disk?.totalBytes)}`, valueKind: 'percent' as const, labels: series.disk.map((point) => point.time), series: single(percentValues(series.disk)) },
    { key: 'network', label: t('serversPage.network'), current: `${bytesPerSecond(latest.value.network?.rxBytesPerSecond)} / ${bytesPerSecond(latest.value.network?.txBytesPerSecond)}`, valueKind: 'bytes' as const, labels: series.network.map((point) => point.time), series: [
      { id: `${props.server.id}-rx`, name: t('serversPage.networkRx'), values: series.network.map((point) => point.rxBytesPerSecond ?? 0) },
      { id: `${props.server.id}-tx`, name: t('serversPage.networkTx'), values: series.network.map((point) => point.txBytesPerSecond ?? 0) },
    ] },
  ];
});

async function load(force = false) {
  // 自动轮询期间已有在途请求时跳过本轮，避免自伤 abort；切换范围或手动刷新时强制中止旧请求。
  if (!force && inFlight) return;
  if (force) metricsController?.abort();
  const requestId = metricsGuard.begin();
  const controller = new AbortController();
  metricsController = controller;
  loading.value = true;
  error.value = '';
  inFlight = true;
  try {
    const next = await serversApi.metrics(props.server.id, range.value, { signal: controller.signal });
    if (!metricsGuard.isCurrent(requestId)) return;
    metrics.value = next;
  } catch (err) {
    if (isAbortError(err)) return;
    if (!metricsGuard.isCurrent(requestId)) return;
    error.value = err instanceof Error ? err.message : t('serversPage.metricsFailed');
    notifyError(err instanceof Error ? err.message : t('serversPage.metricsFailed'), err);
  } finally {
    if (metricsGuard.isCurrent(requestId)) {
      loading.value = false;
      inFlight = false;
    }
  }
}

function startTimer() {
  window.clearInterval(timer);
  if (!autoRefreshEnabled.value) return;
  timer = window.setInterval(() => {
    if (props.active && document.visibilityState === 'visible') void load();
  }, autoRefreshIntervalMs.value);
}

watch(range, () => { void load(true); });
watch(autoRefreshMode, startTimer);

onMounted(() => {
  void load(true);
  startTimer();
});
onBeforeUnmount(() => {
  window.clearInterval(timer);
  metricsGuard.invalidate();
  metricsController?.abort();
});

function percent(value?: number) {
  return typeof value === 'number' ? `${value.toFixed(1)}%` : t('common.notAvailable');
}

function bytes(value?: number) {
  if (typeof value !== 'number') return t('common.notAvailable');
  if (value >= 1024 ** 3) return `${(value / 1024 ** 3).toFixed(1)} GiB`;
  if (value >= 1024 ** 2) return `${(value / 1024 ** 2).toFixed(1)} MiB`;
  return `${value} B`;
}

function bytesPerSecond(value?: number) {
  if (typeof value !== 'number') return t('common.notAvailable');
  if (!value) return '0 B/s';
  const units = ['B/s', 'KB/s', 'MB/s', 'GB/s', 'TB/s'];
  let unitIndex = 0;
  let scaled = value;
  while (scaled >= 1024 && unitIndex < units.length - 1) {
    scaled /= 1024;
    unitIndex += 1;
  }
  return `${scaled >= 100 ? scaled.toFixed(0) : scaled.toFixed(1)} ${units[unitIndex]}`;
}
</script>

<template>
  <div class="grid h-full min-h-0 grid-rows-[auto_minmax(0,1fr)] gap-4">
    <div class="flex flex-wrap items-center justify-between gap-2">
      <span class="text-xs text-muted-foreground">{{ t('serversPage.metricsSampledAt') }}: {{ formatDateTime(latest.time, t('common.never')) }}</span>
      <div class="flex flex-wrap items-center gap-2">
        <Select v-model="range" :options="rangeOptions" class="w-24" />
        <AutoRefreshControl
          :off-label="t('autoRefresh.off')"
          :short-label="t('autoRefresh.5s')"
          :long-label="t('autoRefresh.10s')"
          :hint-label="t('autoRefresh.hint')"
        />
        <Button size="sm" variant="ghost" :loading="loading" @click="load(true)"><RefreshCcw />{{ t('common.refresh') }}</Button>
      </div>
    </div>

    <div class="relative min-h-0 overflow-y-auto">
      <LoadingOverlay v-if="loading && !metrics" :label="t('common.loading')" />
      <EmptyState v-else-if="error && !metrics" variant="page" :title="t('common.loadFailed')" :description="error">
        <template #actions>
          <Button size="sm" :loading="loading" @click="load(true)"><RefreshCcw />{{ t('common.retry') }}</Button>
        </template>
      </EmptyState>
      <EmptyState v-else-if="metrics && !hasSamples" variant="inset" :title="t('serversPage.noMetrics')" :description="t('serversPage.noMetricsHint')" />
      <div v-else-if="metrics" class="grid gap-4 xl:grid-cols-2">
        <div v-for="panel in panels" :key="panel.key" class="grid min-h-[240px] grid-rows-[auto_minmax(0,1fr)] rounded-xl border border-border bg-muted p-3">
          <div class="flex items-center justify-between gap-3">
            <span class="text-xs font-medium text-muted-foreground">{{ panel.label }}</span>
            <span class="truncate text-sm font-semibold text-foreground">{{ panel.current }}</span>
          </div>
          <div class="mt-2 min-h-0">
            <MetricLineChart :labels="panel.labels" :series="panel.series" :value-kind="panel.valueKind" />
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
