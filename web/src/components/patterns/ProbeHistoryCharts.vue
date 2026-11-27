<script setup lang="ts">
import { computed, defineAsyncComponent } from 'vue';
import { useI18n } from '@/i18n';
import type { MetricSeriesData, MetricThreshold } from '@/utils/metrics';
import { formatLatency, formatPercent, seriesStats, type SeriesStats } from '@/utils/metrics';
import type { NetworkProbeThresholds } from '@/types/networkProbe';
import ChartCard from './ChartCard.vue';

const MetricChart = defineAsyncComponent(() => import('@/components/patterns/MetricChart.vue'));

const props = defineProps<{
  latency: MetricSeriesData;
  loss: MetricSeriesData;
  latencyCurrent?: number | null;
  lossCurrent?: number | null;
  thresholds?: NetworkProbeThresholds;
}>();

const { t } = useI18n();

const hasLatency = computed(() => props.latency.points.some((point) => point.value !== null));
const hasLoss = computed(() => props.loss.points.some((point) => point.value !== null));

/** 统计项：标签 + 格式化值；无有效样本显示统一占位，不伪造 0。 */
function statText(label: string, value: number | null, formatter: (value: number) => string): string {
  return `${label} ${value === null ? t('common.notAvailable') : formatter(value)}`;
}

/** 整体统计：最大/最小优先取阴影带极值，没有区间时回退平均序列。 */
function statsFor(item: MetricSeriesData): SeriesStats {
  const base = seriesStats(item.points.map((point) => point.value));
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
  return { ...base, minimum: minimum ?? base.minimum, peak: peak ?? base.peak };
}

function statsText(
  stats: SeriesStats,
  formatter: (value: number) => string,
): string {
  return [
    statText(t('serversPage.metrics.average'), stats.average, formatter),
    statText(t('serversPage.metrics.maximum'), stats.peak, formatter),
    statText(t('serversPage.metrics.minimum'), stats.minimum, formatter),
  ].join(' · ');
}

/**
 * 卡片始终渲染：使用共享 ChartCard（唯一 surface-card 表面）承载标题/当前值，
 * 没有历史时在图表高度盒子里展示空态，而不是整块消失；不再自带卡片，
 * 避免服务器概览里出现 card-in-card。
 */
function thresholdFor(value: number | undefined, formatter: (value: number) => string): MetricThreshold | undefined {
  if (typeof value !== 'number' || !Number.isFinite(value) || value <= 0) return undefined;
  return { value, label: t('serversPage.metrics.warningThreshold', { value: formatter(value) }) };
}

const cards = computed(() => {
  const latencyStats = statsFor(props.latency);
  const lossStats = statsFor(props.loss);
  return [
    {
      key: 'latency',
      title: t('serversPage.probe.latencyHistory'),
      current: formatLatency(props.latencyCurrent),
      series: [props.latency],
      valueKind: 'latency' as const,
      hasData: hasLatency.value,
      hasBand: Boolean(props.latency.band),
      threshold: thresholdFor(props.thresholds?.latencyMs, formatLatency),
      stats: statsText(latencyStats, formatLatency),
    },
    {
      key: 'loss',
      title: t('serversPage.probe.packetLossHistory'),
      current: formatPercent(props.lossCurrent, 0),
      series: [props.loss],
      valueKind: 'percent' as const,
      hasData: hasLoss.value,
      hasBand: Boolean(props.loss.band),
      threshold: thresholdFor(props.thresholds?.packetLossPercent, formatPercent),
      stats: statsText(lossStats, formatPercent),
    },
  ];
});
</script>

<template>
  <div class="grid min-w-0 gap-3 xl:grid-cols-2" data-testid="probe-history-charts">
    <ChartCard v-for="card in cards" :key="card.key" :title="card.title" :value="card.hasData ? card.current : null">
      <div class="h-44 min-h-0 sm:h-48">
        <MetricChart
          v-if="card.hasData"
          :series="card.series"
          :value-kind="card.valueKind"
          :label="card.title"
          :threshold="card.threshold"
          :band-labels="{
            average: t('serversPage.metrics.average'),
            minimum: t('serversPage.metrics.minimum'),
            maximum: t('serversPage.metrics.maximum'),
          }"
        />
        <div v-else class="grid h-full place-items-center rounded-control border border-dashed border-stroke px-3 text-center text-caption text-muted-foreground">
          {{ t('serversPage.probe.noHistory') }}
        </div>
      </div>
      <template #footer>
        <div class="flex flex-wrap items-center gap-x-3 gap-y-1 text-caption tabular-nums text-muted-foreground">
          <span>{{ card.stats }}</span>
          <span v-if="card.hasBand">{{ t('serversPage.metrics.bandHint') }}</span>
          <span v-if="card.threshold">{{ t('serversPage.metrics.thresholdHint') }}</span>
        </div>
      </template>
    </ChartCard>
  </div>
</template>
