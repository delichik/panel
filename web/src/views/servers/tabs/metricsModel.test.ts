import { describe, expect, it } from 'vitest';
import type { ServerMetricsSeries } from '@/api/servers';
import {
  bucketSecondsForRange,
  cpuCurrent,
  cpuSeries,
  diskIoCurrent,
  diskIoSeries,
  diskUsagePercent,
  diskUsageTone,
  hasSamples,
  latestSampleTime,
  memoryCurrent,
  memorySeries,
  networkCurrent,
  networkSeries,
  rangeSpanMs,
  statsFor,
} from './metricsModel';

const base = '2026-11-16T10:00:00.000Z';

function series(overrides: Partial<ServerMetricsSeries> = {}): ServerMetricsSeries {
  return {
    range: '1h',
    cpu: [{ time: base, usagePercent: 42.5 }],
    memory: [{ time: base, usedBytes: 6 * 1024 ** 3, totalBytes: 10 * 1024 ** 3 }],
    diskCurrent: { time: base, usedBytes: 60 * 1024 ** 3, totalBytes: 160 * 1024 ** 3 },
    diskIo: [{ time: base, readBytesPerSecond: 1024, writeBytesPerSecond: 512 }],
    network: [{ time: base, rxBytesPerSecond: 2048, txBytesPerSecond: 1024 }],
    load: [],
    ...overrides,
  };
}

describe('server metrics model', () => {
  it('maps each range onto a bounded server-side bucket', () => {
    expect(bucketSecondsForRange('1h')).toBe(60);
    expect(bucketSecondsForRange('6h')).toBe(120);
    expect(bucketSecondsForRange('1d')).toBe(300);
    expect(bucketSecondsForRange('7d')).toBe(1800);
    expect(rangeSpanMs('1h')).toBe(3_600_000);
    expect(rangeSpanMs('7d')).toBe(7 * 24 * 3_600_000);
  });

  it('builds chart series while keeping missing values empty instead of zero', () => {
    expect(cpuSeries(series())[0]?.points).toEqual([{ time: base, value: 42.5 }]);
    expect(memorySeries(series())[0]?.points).toEqual([{ time: base, value: 60 }]);

    const sparse = series({ diskIo: [{ time: base, readBytesPerSecond: 1024, writeBytesPerSecond: 512 }] });
    expect(diskIoSeries(sparse).map((item) => item.points)).toEqual([
      [{ time: base, value: 1024 }],
      [{ time: base, value: 512 }],
    ]);
    expect(networkSeries(sparse).map((item) => item.points.length)).toEqual([1, 1]);
  });

  it('maps per-bucket min/max onto chart bands and keeps them absent without a range', () => {
    const sample = series({
      cpu: [{ time: base, usagePercent: 42.5, usagePercentMin: 30, usagePercentMax: 55 }],
      network: [{ time: base, rxBytesPerSecond: 2048, rxBytesPerSecondMin: 1024, rxBytesPerSecondMax: 4096, txBytesPerSecond: 1024, txBytesPerSecondMin: 512, txBytesPerSecondMax: 2048 }],
    });
    expect(cpuSeries(sample)[0]?.band).toEqual({
      min: [{ time: base, value: 30 }],
      max: [{ time: base, value: 55 }],
    });
    expect(networkSeries(sample)[0]?.band).toEqual({
      min: [{ time: base, value: 1024 }],
      max: [{ time: base, value: 4096 }],
    });
    // 没有 min/max 的老响应不生成区间。
    expect(diskIoSeries(series())[0]?.band).toBeUndefined();
    expect(memorySeries(series())[0]?.band).toBeUndefined();
  });

  it('derives current values, usage percentages and stats from the returned series', () => {
    const sample = series();
    expect(cpuCurrent(sample)).toBe(42.5);
    expect(memoryCurrent(sample)).toEqual({ usedBytes: 6 * 1024 ** 3, totalBytes: 10 * 1024 ** 3, percent: 60 });
    expect(diskUsagePercent(sample)).toBeCloseTo(37.5, 5);
    expect(diskIoCurrent(sample)).toEqual({ read: 1024, write: 512 });
    expect(networkCurrent(sample)).toEqual({ rx: 2048, tx: 1024 });
    expect(latestSampleTime(sample)).toBe(base);
    expect(hasSamples(sample)).toBe(true);
    expect(statsFor(cpuSeries(sample)[0]!.points)).toEqual({ average: 42.5, peak: 42.5, minimum: 42.5, current: 42.5 });
  });

  it('prefers the latest raw sample for current values when bucketing', () => {
    const sample = series({
      latest: {
        time: '2026-11-16T10:05:00.000Z',
        cpuUsagePercent: 55,
        memoryUsedBytes: 5 * 1024 ** 3,
        memoryTotalBytes: 8 * 1024 ** 3,
        diskUsedBytes: 70 * 1024 ** 3,
        diskTotalBytes: 100 * 1024 ** 3,
        diskSampledAt: '2026-11-16T10:00:00.000Z',
        load1: 0.4,
        rxBytesPerSecond: 4096,
        txBytesPerSecond: 2048,
      },
    });
    expect(cpuCurrent(sample)).toBe(55);
    expect(memoryCurrent(sample)?.percent).toBeCloseTo(62.5, 5);
    expect(networkCurrent(sample)).toEqual({ rx: 4096, tx: 2048 });
    expect(latestSampleTime(sample)).toBe('2026-11-16T10:05:00.000Z');
  });

  it('treats an empty or disk-total-less response as no data', () => {
    const empty = series({ cpu: [], memory: [], diskIo: [], network: [], diskCurrent: null, load: [] });
    expect(hasSamples(empty)).toBe(false);
    expect(hasSamples(null)).toBe(false);
    expect(diskUsagePercent(series({ diskCurrent: { time: base, usedBytes: 1, totalBytes: 0 } }))).toBeNull();
    expect(memoryCurrent(series({ memory: [{ time: base, usedBytes: 1, totalBytes: 0 }] }))).toBeNull();
    expect(latestSampleTime(null)).toBe('');
  });

  it('maps disk usage percentages to stable semantic tones for the ring chart', () => {
    // UI-SRV-029: the disk ring keeps the 85/95 thresholds used by the former progress bar.
    expect(diskUsageTone(null)).toBe('neutral');
    expect(diskUsageTone(84.9)).toBe('success');
    expect(diskUsageTone(85)).toBe('warning');
    expect(diskUsageTone(94.9)).toBe('warning');
    expect(diskUsageTone(95)).toBe('danger');
  });
});
