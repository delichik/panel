import { describe, expect, it, vi } from 'vitest';
import { initializeActivityList } from './initialLoad';

describe('initializeActivityList', () => {
  it('loads immediately while adding the default range to an empty URL', () => {
    const updateQuery = vi.fn();
    const load = vi.fn();

    initializeActivityList(false, '2026-09-13T00:00:00.000Z', updateQuery, load);

    expect(updateQuery).toHaveBeenCalledWith({ from: '2026-09-13T00:00:00.000Z' }, false);
    expect(load).toHaveBeenCalledOnce();
  });

  it('loads immediately without rewriting an explicit range', () => {
    const updateQuery = vi.fn();
    const load = vi.fn();

    initializeActivityList(true, '2026-09-13T00:00:00.000Z', updateQuery, load);

    expect(updateQuery).not.toHaveBeenCalled();
    expect(load).toHaveBeenCalledOnce();
  });
});
