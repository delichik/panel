<script setup lang="ts">
import { computed, defineAsyncComponent, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { RefreshCcw } from '@/icons';
import { serversApi, type ServerMetricsRange, type ServerMetricsSeries } from '@/api/servers';
import { metricsFacilityApi } from '@/api/metricsFacility';
import Button from '@/components/ui/Button.vue';
import EmptyState from '@/components/ui/EmptyState.vue';
import LoadingOverlay from '@/components/ui/LoadingOverlay.vue';
import Select from '@/components/ui/Select.vue';
import DataRefreshControl from '@/components/patterns/DataRefreshControl.vue';
import ChartCard from '@/components/patterns/ChartCard.vue';
import { useAutoRefresh } from '@/composables/useAutoRefresh';
import { useI18n } from '@/i18n';
import type { ServerDto } from '@/types/servers';
import type { MetricsWarningThresholds } from '@/types/metricsFacility';
import { formatDateTime } from '@/utils/datetime';
import { formatBytes, formatPercent, formatRate, type MetricSeriesData, type MetricThreshold, type SeriesStats } from '@/utils/metrics';
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

const props = defineProps<{ server: ServerDto; active?: boolean }>();
const emit = defineEmits<{ 'metrics-loaded': [series: ServerMetricsSeries] }>();
const { t } = useI18n();
const autoRefresh = useAutoRefresh();
const MetricChart = defineAsyncComponent(() => import('@/components/patterns/MetricChart.vue'));

const metrics = ref<ServerMetricsSeries | null>(null);
const warningThresholds = ref<MetricsWarningThresholds>({});
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
  threshold?: MetricThreshold;
  hasData: boolean;
  hasBand: boolean;
  emptyKey: string;
}

function seriesHasData(series: MetricSeriesData[]): boolean {
  return series.some((item) => item.points.some((point) => point.value !== null));
}

/** 百分比统计项：标签 + 值；无有效样本显示统一占位，不伪造 0。 */
function percentStat(label: string, value: number | null): string {
  return `${label} ${value === null ? t('common.notAvailable') : formatPercent(value, 1)}`;
}

/**
 * 单体序列的整体统计：平均取桶平均序列，最大/最小优先取阴影带的真实极值，
 * 没有 band 时回退到平均序列（内存等不提供区间）。
 */
function statsForMetric(item: MetricSeriesData): SeriesStats {
  const base = statsFor(item.points);
  if (!item.band) return base;
  let minimum: number | null = null;
  let peak: number | null = null;
  for (const point of item.band.min) {
    if (typeof point.value === 'number' && Number.isFinite(point.value)) {
      minimum = minimum === null ? point.value : Math.min(minimum, point.value);
    }
  }
  for (const point of item.band.max) {
    if (typeof point.value === 'number' && Number.isFinite(point.value)) {
      peak = peak === null ? point.value : Math.max(peak, point.value);
    }
  }
  return {
    ...base,
    minimum: minimum ?? base.minimum,
    peak: peak ?? base.peak,
  };
}

/** 字节速率统计行：平均 / 最大 / 最小。 */
function rateStatLine(item: MetricSeriesData): string {
  const stats = statsForMetric(item);
  const fallback = t('common.notAvailable');
  return [
    `${t('serversPage.metrics.average')} ${stats.average === null ? fallback : formatRate(stats.average)}`,
    `${t('serversPage.metrics.maximum')} ${stats.peak === null ? fallback : formatRate(stats.peak)}`,
    `${t('serversPage.metrics.minimum')} ${stats.minimum === null ? fallback : formatRate(stats.minimum)}`,
  ].join(' · ');
}

/** 阈值为空/非法时不启用；标签按图表单位格式化。 */
function thresholdFor(value: number | undefined, formatter: (value: number) => string): MetricThreshold | undefined {
  if (typeof value !== 'number' || !Number.isFinite(value) || value <= 0) return undefined;
  return { value, label: t('serversPage.metrics.warningThreshold', { value: formatter(value) }) };
}

const panels = computed<PanelView[]>(() => {
  const series = metrics.value;
  if (!series) return [];
  const cpu = cpuSeries(series);
  const memory = memorySeries(series).map((item) => ({ ...item, name: t('serversPage.memory') }));
  const diskIo = diskIoSeries(series);
  const diskRead = { ...diskIo[0]!, name: t('serversPage.diskIoRead') };
  const diskWrite = { ...diskIo[1]!, name: t('serversPage.diskIoWrite') };
  const network = networkSeries(series);
  const networkRx = { ...network[0]!, name: t('serversPage.networkRx') };
  const networkTx = { ...network[1]!, name: t('serversPage.networkTx') };
  const memoryNow = memoryCurrent(series);
  const diskNow = diskIoCurrent(series);
  const networkNow = networkCurrent(series);

  const cpuStats = statsForMetric(cpu[0]!);
  const memoryStats = statsFor(memory[0]?.points ?? []);

  return [
    {
      key: 'cpu',
      title: t('serversPage.metrics.cpuUsage'),
      valueKind: 'percent',
      series: cpu,
      current: cpuCurrent(series) === null ? t('common.notAvailable') : formatPercent(cpuCurrent(series), 1),
      stats: [
        percentStat(t('serversPage.metrics.average'), cpuStats.average),
        percentStat(t('serversPage.metrics.maximum'), cpuStats.peak),
        percentStat(t('serversPage.metrics.minimum'), cpuStats.minimum),
      ].join(' · '),
      legend: [],
      threshold: thresholdFor(warningThresholds.value.cpuUsagePercent, (value) => formatPercent(value, 1)),
      hasData: seriesHasData(cpu),
      hasBand: Boolean(cpu[0]?.band),
      emptyKey: 'serversPage.metrics.notReported',
    },
    {
      key: 'memory',
      title: t('serversPage.metrics.memoryUsage'),
      valueKind: 'percent',
      series: memory,
      current: memoryNow?.percent === null || memoryNow?.percent === undefined ? t('common.notAvailable') : formatPercent(memoryNow.percent, 1),
      currentHint: memoryNow ? `${formatBytes(memoryNow.usedBytes)} / ${formatBytes(memoryNow.totalBytes)}` : undefined,
      stats: [
        percentStat(t('serversPage.metrics.average'), memoryStats.average),
        percentStat(t('serversPage.metrics.maximum'), memoryStats.peak),
        percentStat(t('serversPage.metrics.minimum'), memoryStats.minimum),
      ].join(' · '),
      legend: [],
      threshold: thresholdFor(warningThresholds.value.memoryUsagePercent, (value) => formatPercent(value, 1)),
      hasData: seriesHasData(memory),
      hasBand: false,
      emptyKey: 'serversPage.metrics.notReported',
    },
    {
      key: 'disk-read',
      title: t('serversPage.diskIoRead'),
      valueKind: 'bytes',
      series: [diskRead],
      current: diskNow ? formatRate(diskNow.read) : t('common.notAvailable'),
      stats: rateStatLine(diskRead),
      legend: [],
      threshold: thresholdFor(warningThresholds.value.diskReadBytesPerSecond, formatRate),
      hasData: seriesHasData([diskRead]),
      hasBand: Boolean(diskRead.band),
      emptyKey: 'serversPage.metrics.diskIoNotReported',
    },
    {
      key: 'disk-write',
      title: t('serversPage.diskIoWrite'),
      valueKind: 'bytes',
      series: [diskWrite],
      current: diskNow ? formatRate(diskNow.write) : t('common.notAvailable'),
      stats: rateStatLine(diskWrite),
      legend: [],
      threshold: thresholdFor(warningThresholds.value.diskWriteBytesPerSecond, formatRate),
      hasData: seriesHasData([diskWrite]),
      hasBand: Boolean(diskWrite.band),
      emptyKey: 'serversPage.metrics.diskIoNotReported',
    },
    {
      key: 'network-rx',
      title: t('serversPage.networkRx'),
      valueKind: 'bytes',
      series: [networkRx],
      current: networkNow ? formatRate(networkNow.rx) : t('common.notAvailable'),
      stats: rateStatLine(networkRx),
      legend: [],
      threshold: thresholdFor(warningThresholds.value.networkRxBytesPerSecond, formatRate),
      hasData: seriesHasData([networkRx]),
      hasBand: Boolean(networkRx.band),
      emptyKey: 'serversPage.metrics.notReported',
    },
    {
      key: 'network-tx',
      title: t('serversPage.networkTx'),
      valueKind: 'bytes',
      series: [networkTx],
      current: networkNow ? formatRate(networkNow.tx) : t('common.notAvailable'),
      stats: rateStatLine(networkTx),
      legend: [],
      threshold: thresholdFor(warningThresholds.value.networkTxBytesPerSecond, formatRate),
      hasData: seriesHasData([networkTx]),
      hasBand: Boolean(networkTx.band),
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
  } finally {
    if (metricsGuard.isCurrent(requestId)) {
      loading.value = false;
      inFlight = false;
    }
  }
}

async function loadWarningThresholds() {
  try {
    const facility = await metricsFacilityApi.config();
    warningThresholds.value = facility.thresholds ?? {};
  } catch {
    // 阈值是展示增强：读取失败时不画阈值线，不影响指标加载。
    warningThresholds.value = {};
  }
}

watch(range, () => { void load(true); });

onMounted(() => {
  void load(true);
  void loadWarningThresholds();
  // 指标展示页复用全局自动刷新档位；非当前 tab / 加载中时跳过这一拍。
  autoRefresh.start(() => {
    if (props.active === false || inFlight) return;
    void load();
  });
});
onBeforeUnmount(() => {
  metricsGuard.invalidate();
  metricsController?.abort();
});
</script>

<template>
  <div class="grid gap-3">
    <div class="flex flex-wrap items-center justify-between gap-2">
      <span class="text-caption text-muted-foreground">{{ t('serversPage.metricsSampledAt') }}: {{ formatDateTime(sampledAt, t('common.never')) }}</span>
      <div class="flex flex-wrap items-center gap-2">
        <Select v-model="range" :options="rangeOptions" class="w-24" />
        <DataRefreshControl />
        <Button size="sm" variant="ghost" :loading="loading" @click="load(true)"><RefreshCcw />{{ t('common.refresh') }}</Button>
      </div>
    </div>

    <div class="relative grid min-h-60 gap-3">
      <LoadingOverlay v-if="loading && !metrics" :label="t('common.loading')" />
      <EmptyState v-else-if="error && !metrics" variant="inset" :title="t('common.loadFailed')" :description="error">
        <template #actions>
          <Button size="sm" :loading="loading" @click="load(true)"><RefreshCcw />{{ t('common.retry') }}</Button>
        </template>
      </EmptyState>
      <EmptyState v-else-if="metrics && !hasSamples(metrics)" variant="inset" :title="t('serversPage.noMetrics')" :description="t('serversPage.noMetricsHint')" />

      <template v-else-if="metrics">
        <div class="grid gap-3 wide:grid-cols-2">
          <ChartCard
            v-for="panel in panels"
            :key="panel.key"
            :title="panel.title"
            :value="panel.hasData ? panel.current : null"
            :value-label="panel.currentHint ? `${panel.title}: ${panel.currentHint}` : panel.title"
          >
            <div class="h-48 min-h-0 sm:h-55">
              <MetricChart
                v-if="panel.hasData"
                :series="panel.series"
                :value-kind="panel.valueKind"
                :label="panel.title"
                :threshold="panel.threshold"
                :band-labels="{
                  average: t('serversPage.metrics.average'),
                  minimum: t('serversPage.metrics.minimum'),
                  maximum: t('serversPage.metrics.maximum'),
                }"
              />
              <div v-else class="grid h-full place-items-center rounded-control border border-dashed border-stroke px-3 text-center text-caption text-muted-foreground">{{ t(panel.emptyKey) }}</div>
            </div>
            <template #footer>
              <div class="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1 text-caption text-muted-foreground">
                <span>{{ panel.stats }}</span>
                <span v-if="panel.hasBand">{{ t('serversPage.metrics.bandHint') }}</span>
                <span v-if="panel.threshold">{{ t('serversPage.metrics.thresholdHint') }}</span>
                <span v-for="item in panel.legend" :key="item.name" class="inline-flex items-center gap-1.5">
                  <i class="size-2 rounded-micro" :class="item.colorClass" aria-hidden="true" />{{ item.name }}
                </span>
                <span v-if="panel.currentHint" class="tabular-nums">{{ panel.currentHint }}</span>
              </div>
            </template>
          </ChartCard>
        </div>
        <p v-if="usesBuckets" class="m-0 text-caption text-muted-foreground">{{ t('serversPage.metrics.bucketNote') }}</p>
      </template>
    </div>
  </div>
</template>
