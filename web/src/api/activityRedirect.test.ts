import { describe, expect, it } from 'vitest';
import { resolveActivityRedirect } from './activityRedirect';

describe('resolveActivityRedirect', () => {
  it('maps legacy by-operation view to events', () => {
    expect(resolveActivityRedirect({ view: 'operations', operationId: 'op-1', resourceType: 'application' })).toEqual({
      path: '/events',
      query: { operationId: 'op-1', resourceType: 'application' },
    });
  });

  it('maps event deep links to logs and streamId to eventId', () => {
    expect(resolveActivityRedirect({ eventId: 'evt-1', from: '2026-01-01T00:00:00Z' })).toEqual({
      path: '/logs',
      query: { eventId: 'evt-1', from: '2026-01-01T00:00:00Z', level: 'warning,error' },
    });
    expect(resolveActivityRedirect({ streamId: 'evt-legacy', from: '2026-01-01T00:00:00Z' })).toEqual({
      path: '/logs',
      query: { eventId: 'evt-legacy', from: '2026-01-01T00:00:00Z', level: 'warning,error' },
    });
    expect(resolveActivityRedirect({ view: 'events', q: 'fail' })).toEqual({
      path: '/logs',
      query: { q: 'fail' },
    });
  });
});
