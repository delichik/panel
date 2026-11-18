<script setup lang="ts">
import { computed, defineAsyncComponent } from 'vue';
import { useI18n } from '@/i18n';
import type { MetricSeriesData } from '@/utils/metrics';
import { formatLatency, formatPercent } from '@/utils/metrics';
import ChartCard from './ChartCard.vue';

const MetricChart = defineAsyncComponent(() => import('@/components/patterns/MetricChart.vue'));

const props = defineProps<{
  latency: MetricSeriesData;
  loss: MetricSeriesData;
  latencyCurrent?: number | null;
  lossCurrent?: number | null;
}>();

const { t } = useI18n();

const hasLatency = computed(() => props.latency.points.some((point) => point.value !== null));
const hasLoss = computed(() => props.loss.points.some((point) => point.value !== null));

/**
 * 卡片始终渲染：使用共享 ChartCard（唯一 surface-card 表面）承载标题/当前值，
 * 没有历史时在图表高度盒子里展示空态，而不是整块消失；不再自带卡片，
 * 避免服务器概览里出现 card-in-card。
 */
const cards = computed(() => [
  {
    key: 'latency',
    title: t('serversPage.probe.latencyHistory'),
    current: formatLatency(props.latencyCurrent),
    series: [props.latency],
    valueKind: 'latency' as const,
    hasData: hasLatency.value,
  },
  {
    key: 'loss',
    title: t('serversPage.probe.packetLossHistory'),
    current: formatPercent(props.lossCurrent, 0),
    series: [props.loss],
    valueKind: 'percent' as const,
    hasData: hasLoss.value,
  },
]);
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
        />
        <div v-else class="grid h-full place-items-center rounded-control border border-dashed border-stroke px-3 text-center text-caption text-muted-foreground">
          {{ t('serversPage.probe.noHistory') }}
        </div>
      </div>
    </ChartCard>
  </div>
</template>
