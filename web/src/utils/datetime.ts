const pad = (number: number) => String(number).padStart(2, '0');

const numericPattern = /^-?\d+(?:\.\d+)?$/;
const naiveDateTimePattern = /^(\d{4}-\d{2}-\d{2})[ T](\d{2}:\d{2}:\d{2}(?:\.\d+)?)$/;
const timeOfDayPattern = /^([01]\d|2[0-3]):([0-5]\d)$/;

/**
 * Parse a timestamp (UTC RFC3339/ISO string, naive UTC SQL string, Unix seconds/ms
 * number or numeric string, or Date) into a valid Date instance.
 */
export function parseDateTime(value?: string | number | Date | null): Date | null {
  if (value === null || value === undefined || value === '') return null;
  if (value instanceof Date) {
    return Number.isNaN(value.getTime()) ? null : value;
  }
  if (typeof value === 'number') {
    if (!Number.isFinite(value) || value === 0) return null;
    const ms = Math.abs(value) < 1e11 ? value * 1000 : value;
    const date = new Date(ms);
    return Number.isNaN(date.getTime()) ? null : date;
  }
  const trimmed = value.trim();
  if (!trimmed) return null;
  if (numericPattern.test(trimmed)) {
    const num = Number(trimmed);
    if (!Number.isFinite(num) || num === 0) return null;
    const ms = Math.abs(num) < 1e11 ? num * 1000 : num;
    const date = new Date(ms);
    return Number.isNaN(date.getTime()) ? null : date;
  }
  const naive = naiveDateTimePattern.exec(trimmed);
  const normalized = naive ? `${naive[1]}T${naive[2]}Z` : trimmed;
  const date = new Date(normalized);
  return Number.isNaN(date.getTime()) ? null : date;
}

/**
 * Format any timestamp in the user's browser local timezone using the unified
 * `yyyy-MM-dd HH:mm:ss` format. Never displays raw Unix timestamps.
 */
export function formatDateTime(value?: string | number | Date | null, fallback = ''): string {
  if (value === null || value === undefined || value === '' || value === 0) return fallback;
  if (typeof value === 'string') {
    const trimmed = value.trim();
    if (!trimmed) return fallback;
    if (numericPattern.test(trimmed) && Number(trimmed) === 0) return fallback;
  }
  const date = parseDateTime(value);
  if (!date) return typeof value === 'string' ? value : fallback;
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`;
}

/** Convert a UTC `HH:MM` daily schedule time to the browser's local `HH:MM`. */
export function utcTimeOfDayToLocal(utcTime: string, referenceDate = new Date()): string {
  const match = timeOfDayPattern.exec(utcTime.trim());
  if (!match) return utcTime;
  const date = new Date(Date.UTC(
    referenceDate.getUTCFullYear(),
    referenceDate.getUTCMonth(),
    referenceDate.getUTCDate(),
    Number(match[1]),
    Number(match[2]),
    0,
    0,
  ));
  return `${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

/** Convert a browser local `HH:MM` daily schedule time to UTC `HH:MM`. */
export function localTimeOfDayToUtc(localTime: string, referenceDate = new Date()): string {
  const match = timeOfDayPattern.exec(localTime.trim());
  if (!match) return localTime;
  const date = new Date(
    referenceDate.getFullYear(),
    referenceDate.getMonth(),
    referenceDate.getDate(),
    Number(match[1]),
    Number(match[2]),
    0,
    0,
  );
  return `${pad(date.getUTCHours())}:${pad(date.getUTCMinutes())}`;
}

/** Human-readable label for the user's browser timezone, e.g. `Asia/Shanghai (UTC+08:00)`. */
export function browserTimeZoneLabel(referenceDate = new Date()): string {
  const offsetMinutes = -referenceDate.getTimezoneOffset();
  const sign = offsetMinutes >= 0 ? '+' : '-';
  const abs = Math.abs(offsetMinutes);
  const offset = `UTC${sign}${pad(Math.floor(abs / 60))}:${pad(abs % 60)}`;
  try {
    const zone = Intl.DateTimeFormat().resolvedOptions().timeZone;
    if (zone && zone !== 'UTC') return `${zone} (${offset})`;
    if (zone === 'UTC' && offsetMinutes === 0) return 'UTC';
  } catch {
    // Fallback to numeric offset when Intl timezone resolution is unavailable.
  }
  return offset;
}
