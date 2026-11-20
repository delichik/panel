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
  label: string;
  /** 警告阈值；达到/超过该值的平均线与最小~最大区间显示红色。 */
  threshold?: MetricThreshold;
  /** 阴影带 tooltip 文案；不传时 tooltip 只显示数值。 */
  bandLabels?: { average: string; minimum: string; maximum: string };
}>(), { threshold: undefined, bandLabels: undefined });

const palette = ref<string[]>([]);
const surface = ref('');
const border = ref('');
const text = ref('');
const textMuted = ref('');
const gridLine = ref('');
const danger = ref('');
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

/** 存在最小~最大阴影带时，tooltip 使用原始序列而不是堆叠差值。 */
const hasBands = computed(() => props.series.some((item) => Boolean(item.band && item.band.min.length)));

const timeSpanMs = computed(() => {  let min = Number.POSITIVE_INFINITY;
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

/**
 * 按警告阈值拆分平均线：<= 阈值进入 normal，>= 阈值进入 over，并在穿越点插入
 * 阈值交点，避免两条线在跨越处断线。无阈值时只返回 normal。
 */
function splitAverageByThreshold(
  points: MetricSeriesData['points'],
  threshold?: MetricThreshold,
): { normal: Array<[number, number | null]>; over: Array<[number, number | null]> } {
  const raw = points.map((point) => ({ at: new Date(point.time).getTime(), value: point.value }));
  const normal: Array<[number, number | null]> = [];
  const over: Array<[number, number | null]> = [];
  if (!threshold) return { normal: raw.map((point) => [point.at, point.value]), over: [] };
  for (let index = 0; index < raw.length; index += 1) {
    const point = raw[index]!;
    if (point.value === null) {
      normal.push([point.at, null]);
      over.push([point.at, null]);
      continue;
    }
    const above = point.value >= threshold.value;
    // 两侧都保留同一时间轴：不属于自己的一段写 null，避免跨过另一段时错误连线。
    normal.push([point.at, above ? null : point.value]);
    over.push([point.at, above ? point.value : null]);
    const previous = raw[index - 1];
    if (!previous || previous.value === null) continue;
    const previousAbove = previous.value >= threshold.value;
    if (previousAbove === above) continue;
    const span = point.value - previous.value;
    const ratio = span === 0 ? 0 : (threshold.value - previous.value) / span;
    const crossing = previous.at + ratio * (point.at - previous.at);
    normal.push([crossing, threshold.value]);
    over.push([crossing, threshold.value]);
  }
  normal.sort((a, b) => a[0] - b[0]);
  over.sort((a, b) => a[0] - b[0]);
  return { normal, over };
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

/** 取某个时间点（毫秒）的值；时间轴按毫秒对齐，找不到返回 null。 */
function valueAt(points: MetricSeriesData['points'], timeMs: number): number | null {
  for (const point of points) {
    if (new Date(point.time).getTime() === timeMs) return point.value;
  }
  return null;
}

/**
 * 最小~最大阴影带：用透明折线堆叠出区间。设置警告阈值后拆成“正常区间”和
 * “超阈值区间”两层，后者用 danger 色填充，使平均线与区间同时表达越界。
 */
function bandSeriesFor(item: MetricSeriesData, index: number, color: string, threshold?: MetricThreshold) {
  const band = item.band;
  if (!band || !band.min.length || !band.max.length) return [];
  const lower = band.min.map((point) => [new Date(point.time).getTime(), point.value] as [number, number | null]);
  const normal = band.max.map((point, position) => {
    const at = new Date(point.time).getTime();
    const minValue = band.min[position]?.value;
    const maxValue = point.value;
    if (minValue === null || minValue === undefined || maxValue === null) return [at, null] as [number, number | null];
    const top = threshold ? Math.min(maxValue, threshold.value) : maxValue;
    return [at, Math.max(0, top - minValue)] as [number, number | null];
  });
  const over = threshold
    ? band.max.map((point, position) => {
      const at = new Date(point.time).getTime();
      const minValue = band.min[position]?.value;
      const maxValue = point.value;
      if (minValue === null || minValue === undefined || maxValue === null || maxValue <= threshold.value) {
        return [at, null] as [number, number | null];
      }
      return [at, Math.max(0, maxValue - Math.max(minValue, threshold.value))] as [number, number | null];
    })
    : [];
  const stack = `metric-band-${item.id || index}`;
  const series = [
    {
      id: `${item.id}-band-min`,
      name: `${item.name} band min`,
      type: 'line' as const,
      data: lower,
      stack,
      showSymbol: false,
      symbol: 'none',
      silent: true,
      z: 1,
      connectNulls: false,
      lineStyle: { opacity: 0, width: 0 },
      emphasis: { disabled: true },
    },
    {
      id: `${item.id}-band-normal`,
      name: `${item.name} band normal`,
      type: 'line' as const,
      data: normal,
      stack,
      showSymbol: false,
      symbol: 'none',
      silent: true,
      z: 1,
      connectNulls: false,
      lineStyle: { opacity: 0, width: 0 },
      areaStyle: { opacity: highContrast.value ? 0.22 : 0.14, color },
      emphasis: { disabled: true },
    },
  ];
  if (threshold) {
    series.push({
      id: `${item.id}-band-over`,
      name: `${item.name} band over`,
      type: 'line' as const,
      data: over,
      stack,
      showSymbol: false,
      symbol: 'none',
      silent: true,
      z: 1,
      connectNulls: false,
      lineStyle: { opacity: 0, width: 0 },
      areaStyle: { opacity: highContrast.value ? 0.32 : 0.24, color: danger.value },
      emphasis: { disabled: true },
    });
  }
  return series;
}

const option = computed<EChartsOption>(() => {
  const colors = palette.value;
  const hc = highContrast.value;
  const multipleSeries = props.series.length > 1;
  const lineWidth = hc ? 3 : 2;
  const axisFontSize = hc ? 11 : 10;
  const showSymbol = hc || multipleSeries;
  const symbolSize = hc ? 7 : 5;
  const threshold = props.threshold;
  const seriesList: Record<string, unknown>[] = [];
  props.series.forEach((item, index) => {
    const color = colors[index % Math.max(colors.length, 1)];
    seriesList.push(...bandSeriesFor(item, index, color, threshold));
    const split = splitAverageByThreshold(item.points, threshold);
    const hasOver = split.over.some((point) => typeof point[1] === 'number');
    const markLine = threshold
      ? {
          silent: true,
          symbol: 'none',
          label: { position: 'insideEndTop', color: textMuted.value, fontSize: axisFontSize },
          lineStyle: { color: hc ? palette.value[0] : danger.value, type: 'dashed', width: hc ? 2 : 1, opacity: hc ? 1 : 0.75 },
          data: [{ yAxis: threshold.value, name: threshold.label, label: { formatter: threshold.label } }],
        }
      : undefined;
    const normalSeries = {
      id: item.id,
      name: item.name,
      type: 'line',
      data: split.normal,
      showSymbol,
      symbol: seriesSymbol(index),
      symbolSize,
      connectNulls: false,
      lineStyle: { width: lineWidth, color, type: seriesLineType(index) },
      itemStyle: { color },
      // HC 下不铺面积；有阴影带时不给平均线铺面积，避免叠色。
      areaStyle: !hc && !item.band && props.series.length === 1 && index === 0
        ? { opacity: 0.16, color }
        : undefined,
      emphasis: { focus: 'none', lineStyle: { width: hc ? 4 : 3, color } },
      markLine: !hasOver ? markLine : undefined,
    };
    seriesList.push(normalSeries);
    if (threshold) {
      seriesList.push({
        id: `${item.id}-over`,
        name: item.name,
        type: 'line',
        data: split.over,
        showSymbol,
        symbol: seriesSymbol(index),
        symbolSize,
        connectNulls: false,
        lineStyle: { width: lineWidth, color: hc ? palette.value[0] : danger.value, type: seriesLineType(index) },
        itemStyle: { color: hc ? palette.value[0] : danger.value },
        areaStyle: !hc && !item.band && props.series.length === 1 && index === 0
          ? { opacity: 0.16, color: danger.value }
          : undefined,
        emphasis: { focus: 'none', lineStyle: { width: hc ? 4 : 3, color: hc ? palette.value[0] : danger.value } },
        markLine: hasOver ? markLine : undefined,
      });
    }
  });
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
          seriesId?: string;
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
        // 有 band 时按时间点从原始序列取平均/最小/最大；堆叠序列的值只是差值，
        // 不能直接展示。tooltip 行数固定为每个实际序列一行。
        if ((hasBands.value || threshold) && typeof rawTime === 'number') {
          const markers = new Map<string, string>();
          for (const row of rows) {
            if (row.seriesId && row.marker) markers.set(row.seriesId, row.marker);
          }
          const labels = props.bandLabels;
          const body = props.series.map((item) => {
            const average = valueAt(item.points, rawTime);
            const minimum = item.band ? valueAt(item.band.min, rawTime) : null;
            const maximum = item.band ? valueAt(item.band.max, rawTime) : null;
            if (average === null && minimum === null && maximum === null) return '';
            const pieces: string[] = [];
            if (average !== null) {
              pieces.push(labels ? `${labels.average} ${formatValue(average)}` : formatValue(average));
            }
            if (minimum !== null && maximum !== null) {
              pieces.push(labels
                ? `${labels.minimum} ${formatValue(minimum)} – ${labels.maximum} ${formatValue(maximum)}`
                : `${formatValue(minimum)} – ${formatValue(maximum)}`);
            }
            return `<div class="metric-chart-tooltip__row">${markers.get(item.id) ?? markers.get(`${item.id}-over`) ?? ''}<span class="metric-chart-tooltip__name">${escapeHtml(item.name)}</span><span class="metric-chart-tooltip__value">${escapeHtml(pieces.join(' · '))}</span></div>`;
          }).join('');
          return `<div class="metric-chart-tooltip__body">${time ? `<div class="metric-chart-tooltip__time">${escapeHtml(time)}</div>` : ''}${body}</div>`;
        }
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
    series: seriesList,
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
  danger.value = hcText;
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
  danger.value = token('--panel-danger');
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
