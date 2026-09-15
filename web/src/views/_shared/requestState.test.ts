import { describe, expect, it } from 'vitest';
import { createLatestRequestGuard, normalizePage } from './requestState';

describe('request state helpers', () => {
  it('normalizes invalid and fractional page query values', () => {
    expect(normalizePage(undefined)).toBe(1);
    expect(normalizePage('invalid')).toBe(1);
    expect(normalizePage('-2')).toBe(1);
    expect(normalizePage('3.8')).toBe(3);
  });

  it('only accepts the latest request', () => {
    const guard = createLatestRequestGuard();
    const first = guard.begin();
    const second = guard.begin();

    expect(guard.isCurrent(first)).toBe(false);
    expect(guard.isCurrent(second)).toBe(true);
    guard.invalidate();
    expect(guard.isCurrent(second)).toBe(false);
  });
});
