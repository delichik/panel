import type { ServerMetricsLatest, ServerMetricsRange, ServerMetricsSeries } from '@/api/servers';
import { percentOf, seriesStats, type MetricSeriesBand, type MetricSeriesData, type MetricSeriesPoint, type SeriesStats } from '@/utils/metrics';

/** 服务器指标范围与降采样桶；点数上限约 60/180/288/336，避免 7d 全量原始点。 */
export const metricsRangeOptions: Array<{ value: ServerMetricsRange; label: string }> = [
  { value: '1h', label: '1h' },
  { value: '6h', label: '6h' },
  { value: '1d', label: '1d' },
  { value: '7d', label: '7d' },
];

export function bucketSecondsForRange(range: ServerMetricsRange): number {
  switch (range) {
    case '6h': return 120;
    case '1d': return 300;
    case '7d': return 1800;
    default: return 60;
  }
}

export function rangeSpanMs(range: ServerMetricsRange): number {
  switch (range) {
    case '6h': return 6 * 60 * 60 * 1000;
    case '1d': return 24 * 60 * 60 * 1000;
    case '7d': return 7 * 24 * 60 * 60 * 1000;
    default: return 60 * 60 * 1000;
  }
}

export type MetricChartSeriesData = MetricSeriesData;
export type MetricChartPoint = MetricSeriesPoint;

function toPoints<T extends { time: string }>(points: T[], value: (point: T) => number | null | undefined): MetricChartSeriesData['points'] {
  return points.map((point) => {
    const next = value(point);
    return { time: point.time, value: typeof next === 'number' && Number.isFinite(next) ? next : null };
  });
}

/**
 * 把同一时间桶的最小/最大值转成 chart band；两侧都有效才形成区间，否则该点
 * 为 null（断带），不补 0。没有任一点存在区间时返回 undefined。
 */
function toBand<T extends { time: string }>(
  points: T[],
  minimum: (point: T) => number | null | undefined,
  maximum: (point: T) => number | null | undefined,
): MetricSeriesBand | undefined {
  let hasRange = false;
  const min: MetricSeriesPoint[] = [];
  const max: MetricSeriesPoint[] = [];
  for (const point of points) {
    const low = minimum(point);
    const high = maximum(point);
    if (typeof low === 'number' && Number.isFinite(low) && typeof high === 'number' && Number.isFinite(high)) {
      min.push({ time: point.time, value: Math.min(low, high) });
      max.push({ time: point.time, value: Math.max(low, high) });
      hasRange = true;
    } else {
      min.push({ time: point.time, value: null });
      max.push({ time: point.time, value: null });
    }
  }
  return hasRange ? { min, max } : undefined;
}

export function cpuSeries(series: ServerMetricsSeries): MetricChartSeriesData[] {
  return [{
    id: 'cpu',
    name: 'CPU',
    points: toPoints(series.cpu, (point) => point.usagePercent),
    band: toBand(series.cpu, (point) => point.usagePercentMin, (point) => point.usagePercentMax),
  }];
}

export function memorySeries(series: ServerMetricsSeries): MetricChartSeriesData[] {
  return [{
    id: 'memory',
    name: 'memory',
    points: toPoints(series.memory, (point) => percentOf(point.usedBytes, point.totalBytes)),
  }];
}

export function diskIoSeries(series: ServerMetricsSeries): MetricChartSeriesData[] {
  return [
    {
      id: 'disk-read',
      name: 'read',
      points: toPoints(series.diskIo, (point) => point.readBytesPerSecond ?? 0),
      band: toBand(series.diskIo, (point) => point.readBytesPerSecondMin, (point) => point.readBytesPerSecondMax),
    },
    {
      id: 'disk-write',
      name: 'write',
      points: toPoints(series.diskIo, (point) => point.writeBytesPerSecond ?? 0),
      band: toBand(series.diskIo, (point) => point.writeBytesPerSecondMin, (point) => point.writeBytesPerSecondMax),
    },
  ];
}

export function networkSeries(series: ServerMetricsSeries): MetricChartSeriesData[] {
  return [
    {
      id: 'network-rx',
      name: 'rx',
      points: toPoints(series.network, (point) => point.rxBytesPerSecond),
      band: toBand(series.network, (point) => point.rxBytesPerSecondMin, (point) => point.rxBytesPerSecondMax),
    },
    {
      id: 'network-tx',
      name: 'tx',
      points: toPoints(series.network, (point) => point.txBytesPerSecond),
      band: toBand(series.network, (point) => point.txBytesPerSecondMin, (point) => point.txBytesPerSecondMax),
    },
  ];
}

export function statsFor(points: MetricChartSeriesData['points']): SeriesStats {
  return seriesStats(points.map((point) => point.value));
}

export function latestSample(series: ServerMetricsSeries | null | undefined): ServerMetricsLatest | undefined {
  return series?.latest;
}

/** 采样时间优先取最近原始样本，其次取任一序列最后一点；都没有时为空。 */
export function latestSampleTime(series: ServerMetricsSeries | null | undefined): string {
  if (!series) return '';
  if (series.latest?.time) return series.latest.time;
  const candidates = [
    series.cpu.at(-1)?.time,
    series.memory.at(-1)?.time,
    series.diskIo.at(-1)?.time,
    series.network.at(-1)?.time,
    series.diskCurrent?.time,
  ].filter((value): value is string => Boolean(value));
  return candidates.sort().at(-1) ?? '';
}

export function diskUsagePercent(series: ServerMetricsSeries | null | undefined): number | null {
  const disk = series?.diskCurrent;
  return disk ? percentOf(disk.usedBytes, disk.totalBytes) : null;
}

/**
 * 磁盘使用率语义 tone：与摘要卡阈值保持一致（>=95 danger、>=85 warning），
 * 无有效样本返回 neutral；只用于展示，不参与任何能力门禁。
 */
export function diskUsageTone(percent: number | null | undefined): 'success' | 'warning' | 'danger' | 'neutral' {
  if (percent === null || percent === undefined || !Number.isFinite(percent)) return 'neutral';
  if (percent >= 95) return 'danger';
  if (percent >= 85) return 'warning';
  return 'success';
}

export function cpuCurrent(series: ServerMetricsSeries | null | undefined): number | null {
  if (!series) return null;
  if (series.latest) return series.latest.cpuUsagePercent;
  return series.cpu.at(-1)?.usagePercent ?? null;
}

export function memoryCurrent(series: ServerMetricsSeries | null | undefined): { usedBytes: number; totalBytes: number; percent: number | null } | null {
  if (!series) return null;
  const latest = series.latest;
  const point = series.memory.at(-1);
  const usedBytes = latest?.memoryUsedBytes ?? point?.usedBytes;
  const totalBytes = latest?.memoryTotalBytes ?? point?.totalBytes;
  if (typeof usedBytes !== 'number' || typeof totalBytes !== 'number' || totalBytes <= 0) return null;
  return { usedBytes, totalBytes, percent: percentOf(usedBytes, totalBytes) };
}

export function diskIoCurrent(series: ServerMetricsSeries | null | undefined): { read: number; write: number } | null {
  const point = series?.diskIo.at(-1);
  if (!point) return null;
  return { read: point.readBytesPerSecond ?? 0, write: point.writeBytesPerSecond ?? 0 };
}

export function networkCurrent(series: ServerMetricsSeries | null | undefined): { rx: number; tx: number } | null {
  if (!series) return null;
  if (series.latest) return { rx: series.latest.rxBytesPerSecond, tx: series.latest.txBytesPerSecond };
  const point = series.network.at(-1);
  if (!point) return null;
  return { rx: point.rxBytesPerSecond, tx: point.txBytesPerSecond };
}

/** 有任意序列或当前磁盘值即可渲染仪表盘；空数组视为尚未收到指标。 */
export function hasSamples(series: ServerMetricsSeries | null | undefined): boolean {
  if (!series) return false;
  return Boolean(
    series.cpu.length
    || series.memory.length
    || series.diskIo.length
    || series.network.length
    || series.diskCurrent,
  );
}
