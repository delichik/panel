<script setup lang="ts">
import { computed, defineAsyncComponent } from 'vue';
import { useI18n } from '@/i18n';
import type { MetricSeriesData } from '@/utils/metrics';
import { formatLatency, formatPercent } from '@/utils/metrics';

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

/** 卡片始终渲染：没有历史时展示明确的空态，而不是整块消失。 */
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
    <section v-for="card in cards" :key="card.key" class="grid min-w-0 gap-2 rounded-xl bg-muted p-3">
      <div class="flex min-w-0 items-baseline justify-between gap-3">
        <h3 class="m-0 text-xs font-medium text-muted-foreground">{{ card.title }}</h3>
        <span class="shrink-0 text-sm font-semibold tabular-nums text-foreground">{{ card.current }}</span>
      </div>
      <div class="relative h-[180px] min-h-0 sm:h-[200px]">
        <MetricChart
          v-if="card.hasData"
          :series="card.series"
          :value-kind="card.valueKind"
          :label="card.title"
        />
        <div v-else class="grid h-full place-items-center rounded-lg border border-dashed border-border px-3 text-center text-xs text-muted-foreground">
          {{ t('serversPage.probe.noHistory') }}
        </div>
      </div>
    </section>
  </div>
</template>
