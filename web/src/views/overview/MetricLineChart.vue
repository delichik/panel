<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue';
import VChart from 'vue-echarts';
import { use } from 'echarts/core';
import { LineChart } from 'echarts/charts';
import { GridComponent, TooltipComponent } from 'echarts/components';
import { CanvasRenderer } from 'echarts/renderers';
import type { EChartsOption } from 'echarts';

use([CanvasRenderer, LineChart, GridComponent, TooltipComponent]);

const props = defineProps<{
  labels: string[];
  series: Array<{ id: string; name: string; values: number[] }>;
  valueKind: 'percent' | 'bytes';
}>();

const palette = ref<string[]>([]);
const surface = ref('');
const border = ref('');
const text = ref('');
let themeObserver: MutationObserver | undefined;

const option = computed<EChartsOption>(() => ({
  animation: false,
  color: palette.value,
  grid: { left: 3, right: 3, top: 5, bottom: 3, containLabel: false },
  tooltip: {
    trigger: 'axis',
    confine: true,
    className: 'overview-metric-tooltip',
    renderMode: 'html',
    backgroundColor: surface.value,
    borderColor: border.value,
    borderWidth: 1,
    padding: 0,
    textStyle: { color: text.value, fontSize: 12 },
    formatter(params) {
      const rows = Array.isArray(params) ? params : [params];
      const time = rows[0]?.name ? formatTime(String(rows[0].name)) : '';
      return `<div class="overview-metric-tooltip__body">${time ? `<div class="overview-metric-tooltip__time">${escapeHtml(time)}</div>` : ''}${rows.map((row) => {
        const value = Array.isArray(row.value) ? Number(row.value.at(-1)) : Number(row.value);
        return `<div class="overview-metric-tooltip__row">${row.marker ?? ''}<span class="overview-metric-tooltip__name">${escapeHtml(String(row.seriesName ?? ''))}</span><span class="overview-metric-tooltip__value">${formatValue(value)}</span></div>`;
      }).join('')}</div>`;
    },
  },
  xAxis: {
    type: 'category',
    boundaryGap: false,
    data: props.labels,
    show: false,
  },
  yAxis: {
    type: 'value',
    show: false,
    min: props.valueKind === 'percent' ? 0 : undefined,
    max: props.valueKind === 'percent' ? 100 : undefined,
  },
  series: props.series.map((item) => ({
    id: item.id,
    name: item.name,
    type: 'line',
    data: item.values,
    showSymbol: false,
    symbol: 'circle',
    lineStyle: { width: 2 },
    emphasis: {
      focus: 'series',
      lineStyle: { width: 3 },
    },
  })),
}));

function formatValue(value: number) {
  if (props.valueKind === 'percent') return `${Math.round(value)}%`;
  if (!value) return '0 B/s';
  if (value < 1024 * 1024) return `${Math.round(value / 1024)} KB/s`;
  return `${(value / 1024 / 1024).toFixed(1)} MB/s`;
}

function formatTime(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  const pad = (number: number) => String(number).padStart(2, '0');
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

function escapeHtml(value: string) {
  return value
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

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
  ];
  surface.value = token('--panel-popover');
  border.value = token('--panel-border-strong');
  text.value = token('--panel-text');
}

onMounted(() => {
  readTheme();
  themeObserver = new MutationObserver(readTheme);
  themeObserver.observe(document.documentElement, { attributes: true, attributeFilter: ['data-theme'] });
});

onBeforeUnmount(() => themeObserver?.disconnect());
</script>

<template>
  <VChart class="h-full min-h-16 w-full" :option="option" autoresize />
</template>

<style>
.overview-metric-tooltip {
  overflow: hidden;
  border-radius: 12px;
  background: var(--panel-popover) !important;
  border: 1px solid var(--panel-border-strong) !important;
  box-shadow: var(--panel-motion-shadow-raised);
  color: var(--panel-text);
}

.overview-metric-tooltip__body {
  display: grid;
  gap: 6px;
  min-width: 180px;
  max-width: min(320px, calc(100vw - 32px));
  padding: 10px;
}

.overview-metric-tooltip__time {
  color: var(--panel-text-muted);
  font-size: 11px;
  line-height: 16px;
  letter-spacing: 0.02em;
}

.overview-metric-tooltip__row {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: center;
  gap: 8px;
  font-size: 12px;
  line-height: 18px;
}

.overview-metric-tooltip__name {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--panel-text);
  font-weight: 600;
}

.overview-metric-tooltip__value {
  color: var(--panel-text-muted);
  font-variant-numeric: tabular-nums;
}
</style>
