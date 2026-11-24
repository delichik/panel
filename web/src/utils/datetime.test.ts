import { describe, expect, it } from 'vitest';
import {
  browserTimeZoneLabel,
  formatDateTime,
  localTimeOfDayToUtc,
  parseDateTime,
  utcTimeOfDayToLocal,
} from './datetime';

describe('formatDateTime', () => {
  it('formats UTC RFC3339 timestamps as yyyy-MM-dd HH:mm:ss in browser local time', () => {
    const local = new Date(2026, 7, 8, 9, 8, 7);
    expect(formatDateTime(local.toISOString())).toBe('2026-08-08 09:08:07');
  });

  it('formats Unix seconds and milliseconds (number or numeric string) without showing raw timestamps', () => {
    const local = new Date(2026, 7, 8, 9, 8, 7);
    const ms = local.getTime();
    const sec = Math.floor(ms / 1000);
    expect(formatDateTime(sec)).toBe('2026-08-08 09:08:07');
    expect(formatDateTime(ms)).toBe('2026-08-08 09:08:07');
    expect(formatDateTime(String(sec))).toBe('2026-08-08 09:08:07');
    expect(formatDateTime(String(ms))).toBe('2026-08-08 09:08:07');
  });

  it('treats naive SQL/ISO datetime strings without timezone as UTC', () => {
    const parsed = parseDateTime('2026-08-08 01:08:07');
    expect(parsed?.toISOString()).toBe('2026-08-08T01:08:07.000Z');
    expect(formatDateTime('2026-08-08 01:08:07')).toBe(formatDateTime('2026-08-08T01:08:07Z'));
  });

  it('returns the fallback for empty or zero values', () => {
    expect(formatDateTime('')).toBe('');
    expect(formatDateTime(0, 'N/A')).toBe('N/A');
    expect(formatDateTime('0', 'N/A')).toBe('N/A');
    expect(formatDateTime(null, 'N/A')).toBe('N/A');
    expect(formatDateTime(undefined, 'N/A')).toBe('N/A');
  });

  it('returns the original value when a non-date string cannot be parsed', () => {
    expect(formatDateTime('not-a-date')).toBe('not-a-date');
  });
});

describe('time-of-day UTC/local conversion', () => {
  it('round-trips HH:MM between UTC and browser local time', () => {
    const ref = new Date('2026-08-08T00:00:00Z');
    for (const utc of ['00:00', '04:00', '16:30', '23:59']) {
      const local = utcTimeOfDayToLocal(utc, ref);
      expect(local).toMatch(/^([01]\d|2[0-3]):[0-5]\d$/);
      expect(localTimeOfDayToUtc(local, ref)).toBe(utc);
    }
  });

  it('returns a non-empty browser timezone label', () => {
    expect(browserTimeZoneLabel()).toMatch(/UTC/);
  });
});
