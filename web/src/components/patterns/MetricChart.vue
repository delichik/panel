<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue';
import VChart from 'vue-echarts';
import { use } from 'echarts/core';
import { LineChart } from 'echarts/charts';
import { GridComponent, MarkLineComponent, TooltipComponent } from 'echarts/components';
import { CanvasRenderer } from 'echarts/renderers';
import type { EChartsOption } from 'echarts';
import type { MetricSeriesData, MetricThreshold } from '@/utils/metrics';
import { formatLatency, formatMetricAxisTime, formatMetricDateTime, formatPercent, formatRate, formatRateAxis } from '@/utils/metrics';

use([CanvasRenderer, LineChart, GridComponent, TooltipComponent, MarkLineComponent]);

const props = withDefaults(defineProps<{
  series: MetricSeriesData[];
  valueKind: 'percent' | 'bytes' | 'latency';
  thresholds?: MetricThreshold[];
  label: string;
}>(), { thresholds: () => [] });

const palette = ref<string[]>([]);
const surface = ref('');
const border = ref('');
const text = ref('');
const textMuted = ref('');
const gridLine = ref('');
let themeObserver: MutationObserver | undefined;

const timeSpanMs = computed(() => {
  let min = Number.POSITIVE_INFINITY;
  let max = Number.NEGATIVE_INFINITY;
  for (const item of props.series) {
    for (const point of item.points) {
      const at = new Date(point.time).getTime();
      if (!Number.isFinite(at)) continue;
      min = Math.min(min, at);
      max = Math.max(max, at);
    }
  }
  return Number.isFinite(min) && Number.isFinite(max) && max > min ? max - min : 0;
});

function timePairs(points: MetricSeriesData['points']): Array<[number, number | null]> {
  return points.map((point) => [new Date(point.time).getTime(), point.value]);
}

function formatValue(value: number) {
  if (props.valueKind === 'latency') return formatLatency(value);
  return props.valueKind === 'percent' ? formatPercent(value, 1) : formatRate(value);
}

function escapeHtml(value: string) {
  return value
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

const option = computed<EChartsOption>(() => {
  const colors = palette.value;
  const thresholds = props.thresholds;
  return {
    animation: false,
    color: colors,
    grid: { left: 4, right: 10, top: 10, bottom: 4, containLabel: true },
    tooltip: {
      trigger: 'axis',
      appendToBody: true,
      className: 'metric-chart-tooltip',
      renderMode: 'html',
      backgroundColor: surface.value,
      borderColor: border.value,
      borderWidth: 1,
      padding: 0,
      textStyle: { color: text.value, fontSize: 12 },
      axisPointer: {
        type: 'line',
        lineStyle: { color: textMuted.value, width: 1, type: 'dashed', opacity: 0.6 },
      },
      formatter(params) {
        const rows = (Array.isArray(params) ? params : [params]) as Array<{
          name?: string;
          seriesName?: string;
          marker?: string;
          value?: unknown;
        }>;
        const rendered = rows.filter((row) => {
          const value = Array.isArray(row.value) ? row.value.at(-1) : row.value;
          return typeof value === 'number' && Number.isFinite(value);
        });
        if (!rendered.length) return '';
        const rawTime = Array.isArray(rendered[0]?.value) ? rendered[0]?.value?.[0] : undefined;
        const time = typeof rawTime === 'number' ? formatMetricDateTime(rawTime) : (rendered[0]?.name ?? '');
        const body = rendered.map((row) => {
          const value = Array.isArray(row.value) ? Number(row.value.at(-1)) : Number(row.value);
          return `<div class="metric-chart-tooltip__row">${row.marker ?? ''}<span class="metric-chart-tooltip__name">${escapeHtml(String(row.seriesName ?? ''))}</span><span class="metric-chart-tooltip__value">${formatValue(value)}</span></div>`;
        }).join('');
        return `<div class="metric-chart-tooltip__body">${time ? `<div class="metric-chart-tooltip__time">${escapeHtml(time)}</div>` : ''}${body}</div>`;
      },
    },
    xAxis: {
      type: 'time',
      axisLine: { lineStyle: { color: gridLine.value } },
      axisTick: { show: false },
      axisLabel: {
        color: textMuted.value,
        fontSize: 10,
        hideOverlap: true,
        formatter(value: number) {
          return formatMetricAxisTime(value, timeSpanMs.value);
        },
      },
      splitLine: { show: false },
    },
    yAxis: {
      type: 'value',
      min: 0,
      max: props.valueKind === 'percent' ? 100 : undefined,
      axisLine: { show: false },
      axisTick: { show: false },
      axisLabel: {
        color: textMuted.value,
        fontSize: 10,
        formatter(value: number) {
          if (props.valueKind === 'latency') return `${value} ms`;
          return props.valueKind === 'percent' ? `${value}%` : formatRateAxis(value);
        },
      },
      splitLine: { lineStyle: { color: gridLine.value, type: 'dashed' } },
    },
    series: props.series.map((item, index) => ({
      id: item.id,
      name: item.name,
      type: 'line',
      data: timePairs(item.points),
      showSymbol: false,
      symbol: 'circle',
      symbolSize: 5,
      connectNulls: false,
      lineStyle: { width: 2, color: colors[index % Math.max(colors.length, 1)] },
      itemStyle: { color: colors[index % Math.max(colors.length, 1)] },
      areaStyle: props.series.length === 1 && index === 0
        ? { opacity: 0.16, color: colors[0] }
        : undefined,
      emphasis: { focus: 'series', lineStyle: { width: 3 } },
      markLine: index === 0 && thresholds.length
        ? {
            silent: true,
            symbol: 'none',
            label: { position: 'insideEndTop', color: textMuted.value, fontSize: 10 },
            lineStyle: { color: colors[3] ?? colors[0], type: 'dashed', width: 1, opacity: 0.7 },
            data: thresholds.map((threshold) => ({ yAxis: threshold.value, name: threshold.label, label: { formatter: threshold.label } })),
          }
        : undefined,
    })),
  } as EChartsOption;
});

function readTheme() {
  const styles = window.getComputedStyle(document.documentElement);
  const token = (name: string) => styles.getPropertyValue(name).trim();
  palette.value = [
    token('--panel-primary'),
    token('--panel-info'),
    token('--panel-success'),
    token('--panel-warning'),
    token('--panel-danger'),
    token('--panel-neutral'),
  ].filter(Boolean);
  surface.value = token('--panel-popover');
  border.value = token('--panel-border-strong');
  text.value = token('--panel-text');
  textMuted.value = token('--panel-text-muted');
  gridLine.value = token('--panel-border');
}

onMounted(() => {
  readTheme();
  themeObserver = new MutationObserver(readTheme);
  themeObserver.observe(document.documentElement, { attributes: true, attributeFilter: ['data-theme', 'data-scheme'] });
});

onBeforeUnmount(() => {
  themeObserver?.disconnect();
});
</script>

<template>
  <div class="h-full min-h-0 w-full" role="img" :aria-label="label" data-testid="metric-chart">
    <VChart class="h-full w-full" :option="option" autoresize aria-hidden="true" />
  </div>
</template>

<style>
.metric-chart-tooltip {
  overflow: hidden;
  border-radius: 12px;
  background: var(--panel-popover) !important;
  border: 1px solid var(--panel-border-strong) !important;
  box-shadow: var(--panel-motion-shadow-raised);
  color: var(--panel-text);
}

.metric-chart-tooltip__body {
  display: grid;
  gap: 6px;
  min-width: 180px;
  max-width: min(320px, calc(100vw - 32px));
  padding: 10px;
}

.metric-chart-tooltip__time {
  color: var(--panel-text-muted);
  font-size: 11px;
  line-height: 16px;
  letter-spacing: 0.02em;
}

.metric-chart-tooltip__row {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: center;
  gap: 8px;
  font-size: 12px;
  line-height: 18px;
}

.metric-chart-tooltip__name {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--panel-text);
  font-weight: 600;
}

.metric-chart-tooltip__value {
  color: var(--panel-text-muted);
  font-variant-numeric: tabular-nums;
}
</style>
