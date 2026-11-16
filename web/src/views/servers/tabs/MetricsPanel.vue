<script setup lang="ts">
import { computed, defineAsyncComponent, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { RefreshCcw } from '@lucide/vue';
import { serversApi, type ServerMetricsRange, type ServerMetricsSeries } from '@/api/servers';
import Button from '@/components/ui/Button.vue';
import EmptyState from '@/components/ui/EmptyState.vue';
import LoadingOverlay from '@/components/ui/LoadingOverlay.vue';
import Select from '@/components/ui/Select.vue';
import { useErrorToast } from '@/components/ui/toast';
import { useI18n } from '@/i18n';
import type { ServerDto } from '@/types/servers';
import { formatDateTime } from '@/utils/datetime';
import { formatBytes, formatPercent, formatRate, type MetricSeriesData, type MetricThreshold } from '@/utils/metrics';
import { createLatestRequestGuard, isAbortError } from '@/views/_shared/requestState';
import {
  bucketSecondsForRange,
  cpuCurrent,
  cpuSeries,
  diskIoCurrent,
  diskIoSeries,
  hasSamples,
  latestSampleTime,
  memoryCurrent,
  memorySeries,
  metricsRangeOptions,
  networkCurrent,
  networkSeries,
  statsFor,
} from './metricsModel';

const props = defineProps<{ server: ServerDto }>();
const emit = defineEmits<{ 'metrics-loaded': [series: ServerMetricsSeries] }>();
const { t } = useI18n();
const notifyError = useErrorToast();
const MetricChart = defineAsyncComponent(() => import('@/components/patterns/MetricChart.vue'));

const metrics = ref<ServerMetricsSeries | null>(null);
const loading = ref(false);
const error = ref('');
const range = ref<ServerMetricsRange>('1h');
const rangeOptions = metricsRangeOptions;

const metricsGuard = createLatestRequestGuard();
let metricsController: AbortController | null = null;
let inFlight = false;

const sampledAt = computed(() => latestSampleTime(metrics.value));

interface PanelView {
  key: string;
  title: string;
  valueKind: 'percent' | 'bytes';
  series: MetricSeriesData[];
  current: string;
  currentHint?: string;
  stats: string;
  legend: Array<{ name: string; colorClass: string }>;
  thresholds: MetricThreshold[];
  hasData: boolean;
  emptyKey: string;
}

function seriesHasData(series: MetricSeriesData[]): boolean {
  return series.some((item) => item.points.some((point) => point.value !== null));
}

function pairedAverage(series: MetricSeriesData[], formatter: (value: number) => string): string {
  const parts = series.map((item) => {
    const stats = statsFor(item.points);
    return stats.average === null ? t('common.notAvailable') : formatter(stats.average);
  });
  return parts.join(' / ');
}

function pairedPeak(series: MetricSeriesData[], formatter: (value: number) => string): string {
  const parts = series.map((item) => {
    const stats = statsFor(item.points);
    return stats.peak === null ? t('common.notAvailable') : formatter(stats.peak);
  });
  return parts.join(' / ');
}

const panels = computed<PanelView[]>(() => {
  const series = metrics.value;
  if (!series) return [];
  const cpu = cpuSeries(series);
  const memory = memorySeries(series).map((item) => ({ ...item, name: t('serversPage.memory') }));
  const diskIo = diskIoSeries(series).map((item, index) => ({ ...item, name: index === 0 ? t('serversPage.diskIoRead') : t('serversPage.diskIoWrite') }));
  const network = networkSeries(series).map((item, index) => ({ ...item, name: index === 0 ? t('serversPage.networkRx') : t('serversPage.networkTx') }));
  const memoryNow = memoryCurrent(series);
  const diskNow = diskIoCurrent(series);
  const networkNow = networkCurrent(series);

  const cpuStats = statsFor(cpu[0]?.points ?? []);
  const memoryStats = statsFor(memory[0]?.points ?? []);

  return [
    {
      key: 'cpu',
      title: t('serversPage.metrics.cpuUsage'),
      valueKind: 'percent',
      series: cpu,
      current: cpuCurrent(series) === null ? t('common.notAvailable') : formatPercent(cpuCurrent(series), 1),
      stats: `${t('serversPage.metrics.average')} ${cpuStats.average === null ? t('common.notAvailable') : formatPercent(cpuStats.average, 1)} · ${t('serversPage.metrics.peak')} ${cpuStats.peak === null ? t('common.notAvailable') : formatPercent(cpuStats.peak, 1)}`,
      legend: [],
      thresholds: [{ value: 80, label: t('serversPage.metrics.threshold', { value: '80%' }) }],
      hasData: seriesHasData(cpu),
      emptyKey: 'serversPage.metrics.notReported',
    },
    {
      key: 'memory',
      title: t('serversPage.metrics.memoryUsage'),
      valueKind: 'percent',
      series: memory,
      current: memoryNow?.percent === null || memoryNow?.percent === undefined ? t('common.notAvailable') : formatPercent(memoryNow.percent, 1),
      currentHint: memoryNow ? `${formatBytes(memoryNow.usedBytes)} / ${formatBytes(memoryNow.totalBytes)}` : undefined,
      stats: `${t('serversPage.metrics.average')} ${memoryStats.average === null ? t('common.notAvailable') : formatPercent(memoryStats.average, 1)} · ${t('serversPage.metrics.peak')} ${memoryStats.peak === null ? t('common.notAvailable') : formatPercent(memoryStats.peak, 1)}`,
      legend: [],
      thresholds: [{ value: 90, label: t('serversPage.metrics.threshold', { value: '90%' }) }],
      hasData: seriesHasData(memory),
      emptyKey: 'serversPage.metrics.notReported',
    },
    {
      key: 'diskIo',
      title: t('serversPage.diskIo'),
      valueKind: 'bytes',
      series: diskIo,
      current: diskNow ? `${formatRate(diskNow.read)} / ${formatRate(diskNow.write)}` : t('common.notAvailable'),
      stats: [
        `${t('serversPage.metrics.average')} ${pairedAverage(diskIo, (value) => formatRate(value))}`,
        `${t('serversPage.metrics.peak')} ${pairedPeak(diskIo, (value) => formatRate(value))}`,
      ].join(' · '),
      legend: [
        { name: t('serversPage.diskIoRead'), colorClass: 'bg-primary' },
        { name: t('serversPage.diskIoWrite'), colorClass: 'bg-info' },
      ],
      thresholds: [],
      hasData: seriesHasData(diskIo),
      emptyKey: 'serversPage.metrics.diskIoNotReported',
    },
    {
      key: 'network',
      title: t('serversPage.network'),
      valueKind: 'bytes',
      series: network,
      current: networkNow ? `${formatRate(networkNow.rx)} / ${formatRate(networkNow.tx)}` : t('common.notAvailable'),
      stats: [
        `${t('serversPage.metrics.average')} ${pairedAverage(network, (value) => formatRate(value))}`,
        `${t('serversPage.metrics.peak')} ${pairedPeak(network, (value) => formatRate(value))}`,
      ].join(' · '),
      legend: [
        { name: t('serversPage.networkRx'), colorClass: 'bg-primary' },
        { name: t('serversPage.networkTx'), colorClass: 'bg-info' },
      ],
      thresholds: [],
      hasData: seriesHasData(network),
      emptyKey: 'serversPage.metrics.notReported',
    },
  ];
});

const usesBuckets = computed(() => range.value === '1d' || range.value === '7d');

async function load(force = false) {
  // 手动刷新/切换范围时中止旧请求；请求期间重复点击由 force 中止旧请求收敛。
  if (!force && inFlight) return;
  if (force) metricsController?.abort();
  const requestId = metricsGuard.begin();
  const controller = new AbortController();
  metricsController = controller;
  loading.value = true;
  error.value = '';
  inFlight = true;
  try {
    const next = await serversApi.metricsBucketed(props.server.id, range.value, bucketSecondsForRange(range.value), { signal: controller.signal });
    if (!metricsGuard.isCurrent(requestId)) return;
    metrics.value = next;
    emit('metrics-loaded', next);
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

watch(range, () => { void load(true); });

onMounted(() => { void load(true); });
onBeforeUnmount(() => {
  metricsGuard.invalidate();
  metricsController?.abort();
});
</script>

<template>
  <div class="grid gap-3">
    <div class="flex flex-wrap items-center justify-between gap-2">
      <span class="text-xs text-muted-foreground">{{ t('serversPage.metricsSampledAt') }}: {{ formatDateTime(sampledAt, t('common.never')) }}</span>
      <div class="flex flex-wrap items-center gap-2">
        <Select v-model="range" :options="rangeOptions" class="w-24" />
        <Button size="sm" variant="ghost" :loading="loading" @click="load(true)"><RefreshCcw />{{ t('common.refresh') }}</Button>
      </div>
    </div>

    <div class="relative grid min-h-[240px] gap-3">
      <LoadingOverlay v-if="loading && !metrics" :label="t('common.loading')" />
      <EmptyState v-else-if="error && !metrics" variant="inset" :title="t('common.loadFailed')" :description="error">
        <template #actions>
          <Button size="sm" :loading="loading" @click="load(true)"><RefreshCcw />{{ t('common.retry') }}</Button>
        </template>
      </EmptyState>
      <EmptyState v-else-if="metrics && !hasSamples(metrics)" variant="inset" :title="t('serversPage.noMetrics')" :description="t('serversPage.noMetricsHint')" />

      <template v-else-if="metrics">
        <div class="grid gap-3 xl:grid-cols-2">
          <section v-for="panel in panels" :key="panel.key" class="grid min-w-0 gap-2 rounded-xl bg-muted p-3">
            <div class="flex min-w-0 items-baseline justify-between gap-3">
              <h3 class="m-0 text-xs font-medium text-muted-foreground">{{ panel.title }}</h3>
              <div class="min-w-0 text-right">
                <div class="truncate text-sm font-semibold text-foreground">{{ panel.current }}</div>
                <div v-if="panel.currentHint" class="text-[11px] text-muted-foreground">{{ panel.currentHint }}</div>
              </div>
            </div>
            <div class="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1 text-[11px] text-muted-foreground">
              <span>{{ panel.stats }}</span>
              <span v-for="item in panel.legend" :key="item.name" class="inline-flex items-center gap-1.5">
                <i class="size-2 rounded-sm" :class="item.colorClass" aria-hidden="true" />{{ item.name }}
              </span>
            </div>
            <div class="h-[200px] min-h-0 sm:h-[220px]">
              <MetricChart
                v-if="panel.hasData"
                :series="panel.series"
                :value-kind="panel.valueKind"
                :thresholds="panel.thresholds"
                :label="panel.title"
              />
              <div v-else class="grid h-full place-items-center text-xs text-muted-foreground">{{ t(panel.emptyKey) }}</div>
            </div>
          </section>
        </div>
        <p v-if="usesBuckets" class="m-0 text-[11px] text-muted-foreground">{{ t('serversPage.metrics.bucketNote') }}</p>
      </template>
    </div>
  </div>
</template>
