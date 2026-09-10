import { describe, expect, it } from 'vitest';
import type { ActivityEvent } from '@/types/activity';
import { mergeEvents, manualResolution, capacityNotice } from './model';
const event = (eventId: string, seq: number, text = '') => ({ eventId, seq, text } as ActivityEvent);
describe('activity timeline', () => {
  it('deduplicates retransmission without replacing accepted facts and keeps receive order', () => {
    expect(mergeEvents([event('a', 1, 'original'), event('c', 3)], [event('a', 1, 'changed'), event('b', 2)]).map(item => [item.eventId, item.text])).toEqual([['a', 'original'], ['b', ''], ['c', '']]);
  });
  it('keeps late output after a finished event', () => {
    expect(mergeEvents([event('finished', 20)], [event('late-output', 21)])).toHaveLength(2);
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
