import { describe, expect, it } from 'vitest';
import { resolveActivityRedirect } from './activityRedirect';

describe('resolveActivityRedirect', () => {
  it('maps legacy by-operation view to tasks', () => {
    expect(resolveActivityRedirect({ view: 'operations', operationId: 'op-1', resourceType: 'application' })).toEqual({
      path: '/tasks',
      query: { operationId: 'op-1', resourceType: 'application' },
    });
  });

  it('maps default and events view to logs', () => {
    expect(resolveActivityRedirect({ eventId: 'evt-1', from: '2026-01-01T00:00:00Z' })).toEqual({
      path: '/logs',
      query: { eventId: 'evt-1', from: '2026-01-01T00:00:00Z' },
    });
    expect(resolveActivityRedirect({ view: 'events', q: 'fail' })).toEqual({
      path: '/logs',
      query: { q: 'fail' },
    });
  });
});
