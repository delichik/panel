<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue';
import VChart from 'vue-echarts';
import { use } from 'echarts/core';
import { LineChart } from 'echarts/charts';
import { GridComponent, MarkLineComponent, TooltipComponent } from 'echarts/components';
import { CanvasRenderer } from 'echarts/renderers';
import type { EChartsOption } from 'echarts';
import type { MetricSeriesData, MetricThreshold } from '@/utils/metrics';
import { resolveCssColor } from '@/utils/cssColor';
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
/** forced-colors / prefers-contrast: more 时降级为系统色可读摘要。 */
const highContrast = ref(false);
let themeObserver: MutationObserver | undefined;
const contrastQueries: MediaQueryList[] = [];
const contrastCleanups: Array<() => void> = [];

/** Canvas 需要具体颜色；CSS token 可能是 `color-mix(...)`/`var(...)`。 */
function tokenColor(styles: CSSStyleDeclaration, name: string): string {
  return resolveCssColor(styles.getPropertyValue(name).trim());
}

/** 读取系统色（forced-colors 下 Canvas/CanvasText 可读）；不支持时回退主题黑/白。 */
function systemColor(name: 'Canvas' | 'CanvasText', fallback: string): string {
  if (typeof document === 'undefined' || !document.body) return fallback;
  const probe = document.createElement('span');
  probe.style.color = name;
  probe.style.position = 'absolute';
  probe.style.visibility = 'hidden';
  document.body.appendChild(probe);
  try {
    const raw = window.getComputedStyle(probe).color;
    const resolved = resolveCssColor(raw);
    // jsdom/旧浏览器可能不解析系统色（返回空或原样关键字）→ 回退主题黑/白。
    if (!resolved || resolved.toLowerCase() === name.toLowerCase()) return fallback;
    return resolved;
  } finally {
    probe.remove();
  }
}

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

/** 第二条及后续序列用虚线 + 不同 symbol，避免只靠颜色区分（HC 下颜色会趋同）。 */
function seriesLineType(index: number): 'solid' | 'dashed' {
  return index % 2 === 1 ? 'dashed' : 'solid';
}

function seriesSymbol(index: number): 'circle' | 'diamond' | 'rect' {
  if (index === 1) return 'diamond';
  return index % 2 === 1 ? 'rect' : 'circle';
}

const option = computed<EChartsOption>(() => {
  const colors = palette.value;
  const thresholds = props.thresholds;
  const hc = highContrast.value;
  const multipleSeries = props.series.length > 1;
  const lineWidth = hc ? 3 : 2;
  const axisFontSize = hc ? 11 : 10;
  const showSymbol = hc || multipleSeries;
  const symbolSize = hc ? 7 : 5;
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
      borderWidth: hc ? 2 : 1,
      padding: 0,
      textStyle: { color: text.value, fontSize: 12 },
      axisPointer: {
        type: 'line',
        lineStyle: { color: textMuted.value, width: hc ? 2 : 1, type: hc ? 'solid' : 'dashed', opacity: hc ? 1 : 0.6 },
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
        fontSize: axisFontSize,
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
        fontSize: axisFontSize,
        formatter(value: number) {
          if (props.valueKind === 'latency') return `${value} ms`;
          return props.valueKind === 'percent' ? `${value}%` : formatRateAxis(value);
        },
      },
      splitLine: { lineStyle: { color: gridLine.value, type: hc ? 'solid' : 'dashed', opacity: hc ? 1 : 1 } },
    },
    series: props.series.map((item, index) => ({
      id: item.id,
      name: item.name,
      type: 'line',
      data: timePairs(item.points),
      showSymbol,
      symbol: seriesSymbol(index),
      symbolSize,
      connectNulls: false,
      lineStyle: { width: lineWidth, color: colors[index % Math.max(colors.length, 1)], type: seriesLineType(index) },
      itemStyle: { color: colors[index % Math.max(colors.length, 1)] },
      // HC 下不铺面积，避免低对比色块影响读数；单序列正常模式保留轻面积。
      areaStyle: !hc && props.series.length === 1 && index === 0
        ? { opacity: 0.16, color: colors[0] }
        : undefined,
      // 悬停只加重当前线，不因 focus:'series' 把其他线压暗/压没（暗色下尤其明显）。
      emphasis: { focus: 'none', lineStyle: { width: hc ? 4 : 3 } },
      markLine: index === 0 && thresholds.length
        ? {
            silent: true,
            symbol: 'none',
            label: { position: 'insideEndTop', color: textMuted.value, fontSize: axisFontSize },
            lineStyle: { color: hc ? palette.value[0] : (colors[3] ?? colors[0]), type: 'dashed', width: hc ? 2 : 1, opacity: hc ? 1 : 0.7 },
            data: thresholds.map((threshold) => ({ yAxis: threshold.value, name: threshold.label, label: { formatter: threshold.label } })),
          }
        : undefined,
    })),
  } as EChartsOption;
});

function applyHighContrastPalette() {
  const dark = typeof document !== 'undefined' && document.documentElement.dataset.theme === 'dark';
  // HC 下统一用系统前景/背景色：两条线同色，靠线型 + symbol 区分。
  const hcText = systemColor('CanvasText', dark ? '#ffffff' : '#000000');
  const hcSurface = systemColor('Canvas', dark ? '#000000' : '#ffffff');
  palette.value = [hcText, hcText, hcText, hcText, hcText, hcText];
  surface.value = hcSurface;
  border.value = hcText;
  text.value = hcText;
  textMuted.value = hcText;
  gridLine.value = hcText;
}

function readTheme() {
  if (highContrast.value) {
    applyHighContrastPalette();
    return;
  }
  const styles = window.getComputedStyle(document.documentElement);
  const token = (name: string) => tokenColor(styles, name);
  // 首序列优先取 accent ramp（随 data-accent 变化），其余沿用语义状态色。
  palette.value = [
    token('--accent-fill-default') || token('--panel-primary'),
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

function syncContrast() {
  highContrast.value = contrastQueries.some((query) => query.matches);
  readTheme();
}

function watchContrastQuery(query: MediaQueryList) {
  const handler = () => syncContrast();
  if (typeof query.addEventListener === 'function') {
    query.addEventListener('change', handler);
    contrastCleanups.push(() => query.removeEventListener('change', handler));
  } else if (typeof query.addListener === 'function') {
    query.addListener(handler);
    contrastCleanups.push(() => query.removeListener(handler));
  }
}

onMounted(() => {
  if (typeof window !== 'undefined' && typeof window.matchMedia === 'function') {
    for (const media of ['(forced-colors: active)', '(prefers-contrast: more)']) {
      try {
        const query = window.matchMedia(media);
        contrastQueries.push(query);
        watchContrastQuery(query);
      } catch {
        // 老环境不支持该媒体查询时忽略，按普通主题渲染。
      }
    }
    highContrast.value = contrastQueries.some((query) => query.matches);
  }
  readTheme();
  themeObserver = new MutationObserver(readTheme);
  // accent / background / mica / theme 变化都会影响图表取用的 token，必须重新读取。
  themeObserver.observe(document.documentElement, {
    attributes: true,
    attributeFilter: ['data-theme', 'data-accent', 'data-background', 'data-mica'],
  });
});

onBeforeUnmount(() => {
  themeObserver?.disconnect();
  themeObserver = undefined;
  for (const cleanup of contrastCleanups) cleanup();
  contrastCleanups.length = 0;
  contrastQueries.length = 0;
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
  border-radius: 8px;
  background: var(--panel-popover) !important;
  border: 1px solid var(--panel-border-strong) !important;
  box-shadow: var(--panel-shadow-flyout);
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

/* forced-colors 下 HTML tooltip 跟随系统 Canvas/CanvasText，避免自定义色被强制吞掉。 */
@media (forced-colors: active) {
  .metric-chart-tooltip {
    background: Canvas !important;
    color: CanvasText !important;
    border-color: CanvasText !important;
    box-shadow: none !important;
  }
}
</style>
