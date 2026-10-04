import { describe, expect, it } from 'vitest';
import { formatDateTime } from './datetime';

describe('formatDateTime', () => {
  it('formats timestamps as yyyy-MM-dd HH:mm:ss in local time', () => {
    const local = new Date(2026, 7, 8, 9, 8, 7);
    expect(formatDateTime(local.toISOString())).toBe('2026-08-08 09:08:07');
  });

  it('returns the fallback for empty values', () => {
    expect(formatDateTime('')).toBe('');
    expect(formatDateTime(null, 'N/A')).toBe('N/A');
    expect(formatDateTime(undefined, 'N/A')).toBe('N/A');
  });

  it('returns the original value when it cannot be parsed', () => {
    expect(formatDateTime('not-a-date')).toBe('not-a-date');
  });
});
