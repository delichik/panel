import { describe, expect, it } from 'vitest';
import {
  isMetricsCleanupSchedule,
  isMetricsFacilityMode,
  isValidMetricsCollectionInterval,
  isValidMetricsRetention,
  metricsFacilityDefaults,
  parseMetricsPositiveInteger,
} from './metricsFacility';

describe('metrics facility contract helpers', () => {
  it('exposes the backend defaults', () => {
    expect(metricsFacilityDefaults).toEqual({
      mode: 'all',
      serverIds: [],
      retentionDays: 7,
      collectionIntervalSeconds: 60,
      cleanupSchedule: 'daily',
      thresholds: {},
    });
  });

  it('accepts only the contract modes and cleanup schedules', () => {
    expect(isMetricsFacilityMode('all')).toBe(true);
    expect(isMetricsFacilityMode('selected')).toBe(true);
    expect(isMetricsFacilityMode('every')).toBe(false);
    expect(isMetricsCleanupSchedule('hourly')).toBe(true);
    expect(isMetricsCleanupSchedule('daily')).toBe(true);
    expect(isMetricsCleanupSchedule('weekly')).toBe(true);
    expect(isMetricsCleanupSchedule('monthly')).toBe(false);
  });

  it('requires positive integers for retention and collection interval', () => {
    expect(isValidMetricsRetention(1)).toBe(true);
    expect(isValidMetricsRetention(0)).toBe(false);
    expect(isValidMetricsRetention(1.5)).toBe(false);
    expect(isValidMetricsCollectionInterval(60)).toBe(true);
    expect(isValidMetricsCollectionInterval(-1)).toBe(false);
    expect(isValidMetricsCollectionInterval('60')).toBe(false);
  });

  it('parses positive integer form input without silently rounding', () => {
    expect(parseMetricsPositiveInteger(' 30 ')).toBe(30);
    expect(parseMetricsPositiveInteger('0')).toBeNull();
    expect(parseMetricsPositiveInteger('-5')).toBeNull();
    expect(parseMetricsPositiveInteger('2.5')).toBeNull();
    expect(parseMetricsPositiveInteger('')).toBeNull();
  });
});
