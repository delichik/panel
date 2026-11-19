/** 指标展示的共享格式化与统计工具：概览、服务器详情图表共用。 */

const BYTE_UNITS = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];

export interface SeriesStats {
  average: number | null;
  /** 序列最大值（峰值）；无有效值时 null。 */
  peak: number | null;
  /** 序列最小值；无有效值时 null。 */
  minimum: number | null;
  current: number | null;
}

/** 图表通用数据契约：时间点 + 可空值；缺失桶保持 null 以断线而不是补 0。 */
export interface MetricSeriesPoint {
  time: string;
  value: number | null;
}

export interface MetricSeriesData {
  id: string;
  name: string;
  points: MetricSeriesPoint[];
}

export interface MetricThreshold {
  value: number;
  label: string;
}

export function formatPercent(value: number | null | undefined, digits = 1): string {
  if (value === null || value === undefined || !Number.isFinite(value)) return '—';
  return `${value.toFixed(digits)}%`;
}

export function formatBytes(value: number | null | undefined): string {
  if (value === null || value === undefined || !Number.isFinite(value) || value < 0) return '—';
  let scaled = value;
  let unit = 0;
  while (scaled >= 1024 && unit < BYTE_UNITS.length - 1) {
    scaled /= 1024;
    unit += 1;
  }
  return `${scaled >= 100 || unit === 0 ? scaled.toFixed(0) : scaled.toFixed(1)} ${BYTE_UNITS[unit]}`;
}

export function formatRate(value: number | null | undefined): string {
  const formatted = formatBytes(value);
  return formatted === '—' ? '—' : `${formatted}/s`;
}

/** 探测延迟展示：毫秒，保留 1 位；缺失值不伪造 0。 */
export function formatLatency(value: number | null | undefined): string {
  if (value === null || value === undefined || !Number.isFinite(value)) return '—';
  return `${value.toFixed(1)} ms`;
}

/** 图表 Y 轴用：省略 `/s` 以压缩标签宽度；单位由面板标题表达。 */
export function formatRateAxis(value: number | null | undefined): string {
  return formatBytes(value);
}

/** 百分比派生的展示值：总量缺失或非法时返回 null，不伪造 0%。 */
export function percentOf(used: number | null | undefined, total: number | null | undefined): number | null {
  if (typeof used !== 'number' || typeof total !== 'number' || !Number.isFinite(used) || !Number.isFinite(total) || total <= 0) return null;
  return Math.min(100, Math.max(0, (used / total) * 100));
}

/** 忽略 null/NaN 后的平均、峰值、最小值与最后一个有效值；无有效值时全为 null。 */
export function seriesStats(values: Array<number | null | undefined>): SeriesStats {
  let sum = 0;
  let count = 0;
  let peak: number | null = null;
  let minimum: number | null = null;
  let current: number | null = null;
  for (const value of values) {
    if (typeof value !== 'number' || !Number.isFinite(value)) continue;
    sum += value;
    count += 1;
    peak = peak === null ? value : Math.max(peak, value);
    minimum = minimum === null ? value : Math.min(minimum, value);
    current = value;
  }
  return {
    average: count ? sum / count : null,
    peak,
    minimum,
    current,
  };
}

const pad = (value: number) => String(value).padStart(2, '0');

/** 图表轴标签使用的本地时间短格式。 */
export function formatMetricAxisTime(value: number | string, spanMs: number): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '';
  if (spanMs >= 2 * 24 * 60 * 60 * 1000) return `${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;
  return `${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

/** Tooltip / 采样时间使用的本地完整时间。 */
export function formatMetricDateTime(value?: string | number | null): string {
  if (value === null || value === undefined || value === '') return '';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '';
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`;
}
