import { describe, expect, it } from 'vitest';
import {
  formatBytes,
  formatMetricAxisTime,
  formatMetricDateTime,
  formatPercent,
  formatRate,
  formatRateAxis,
  percentOf,
  seriesStats,
} from './metrics';

describe('metrics formatting and stats', () => {
  it('formats percentages, bytes and rates consistently', () => {
    expect(formatPercent(42.56, 1)).toBe('42.6%');
    expect(formatPercent(null)).toBe('—');
    expect(formatBytes(0)).toBe('0 B');
    expect(formatBytes(1536)).toBe('1.5 KB');
    expect(formatBytes(160 * 1024 ** 3)).toBe('160 GB');
    expect(formatRate(1536)).toBe('1.5 KB/s');
    expect(formatRateAxis(1536)).toBe('1.5 KB');
    expect(formatRate(null)).toBe('—');
  });

  it('computes average, peak, minimum and current while ignoring null samples', () => {
    expect(seriesStats([10, null, 30, undefined, 20])).toEqual({ average: 20, peak: 30, minimum: 10, current: 20 });
    expect(seriesStats([null, undefined])).toEqual({ average: null, peak: null, minimum: null, current: null });
  });

  it('derives percentages without inventing values for missing totals', () => {
    expect(percentOf(5, 10)).toBe(50);
    expect(percentOf(12, 10)).toBe(100);
    expect(percentOf(5, 0)).toBeNull();
    expect(percentOf(undefined, 10)).toBeNull();
  });

  it('formats chart axis labels and full timestamps in local time', () => {
    const at = new Date(2026, 10, 16, 9, 5, 0).getTime();
    expect(formatMetricAxisTime(at, 60 * 60 * 1000)).toBe('09:05');
    expect(formatMetricAxisTime(at, 7 * 24 * 60 * 60 * 1000)).toBe('11-16');
    expect(formatMetricDateTime(at)).toBe('2026-11-16 09:05:00');
    expect(formatMetricDateTime(null)).toBe('');
  });
});
