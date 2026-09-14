import { describe, expect, it } from 'vitest';
import type { ActivityEvent } from '@/types/activity';
import { mergeEvents, manualResolution, capacityNotice, eventDisplayMessage, eventMessage } from './model';
const event = (eventId: string, seq: number, text = '') => ({ eventId, seq, text } as ActivityEvent);
// UI-ACT-001: event summaries expose real errors and explain observation rejection semantics.
describe('activity timeline', () => {
  it('deduplicates retransmission without replacing accepted facts and keeps receive order', () => {
    expect(mergeEvents([event('a', 1, 'original'), event('c', 3)], [event('a', 1, 'changed'), event('b', 2)]).map(item => [item.eventId, item.text])).toEqual([['a', 'original'], ['b', ''], ['c', '']]);
  });
  it('keeps late output after a finished event', () => {
    expect(mergeEvents([event('finished', 20)], [event('late-output', 21)])).toHaveLength(2);
  });

  it('prioritizes a structured error, code and detail over a generic event summary', () => {
    const item = {
      ...event('failed', 22, 'Application operation failed'),
      eventType: 'job.failed',
      data: { errorCode: 'agent_unavailable', error: 'Agent is unavailable', detail: 'Check the server connection' },
    } as ActivityEvent;
    expect(eventMessage(item)).toBe('[agent_unavailable] Agent is unavailable — Check the server connection');
    expect(eventMessage({ ...item, data: { errorCode: 'lease_lost', detail: 'The job lease expired' } })).toBe('[lease_lost] — The job lease expired');
  });

  it('explains a rejected stale observation without presenting it as a deployment retry', () => {
    const item = {
      ...event('rejected', 23),
      eventType: 'observation.rejected',
      data: { instanceId: 'facility-reverse-proxy-srv-1', reason: 'stale_or_ownership_lost' },
    } as ActivityEvent;
    const translate = (key: string, params?: Record<string, string | number>) => `${key}:${params?.instance ?? ''}`;
    expect(eventDisplayMessage(item, translate)).toBe('activity.observationRejected.staleOrOwnershipLost:facility-reverse-proxy-srv-1');
  });
});

describe('manual outcome verification', () => {
  it('requires an explicit verified outcome and nonblank evidence reason', () => {
    expect(manualResolution('succeeded', '  ')).toBeUndefined();
    expect(manualResolution('unknown', 'checked the server')).toBeUndefined();
    expect(manualResolution('failed', '  remote command exited 1; service remains stopped  ')).toEqual({ outcome: 'failed', reason: 'remote command exited 1; service remains stopped' });
  });
});

describe('log storage capacity notice', () => {
  it('shows warning and admission-blocked states without claiming an unknown capacity is safe', () => {
    expect(capacityNotice()).toBeUndefined();
    expect(capacityNotice({ state: 'unknown', availableBytes: 0, totalBytes: 0 })).toBeUndefined();
    expect(capacityNotice({ state: 'ok', availableBytes: 4000, totalBytes: 5000 })).toBeUndefined();
    expect(capacityNotice({ state: 'warning', availableBytes: 1000, totalBytes: 5000 })).toBe('warning');
    expect(capacityNotice({ state: 'blocked', availableBytes: 500, totalBytes: 5000 })).toBe('blocked');
  });
});
