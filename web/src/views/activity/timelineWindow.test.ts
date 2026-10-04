import { describe, expect, it } from 'vitest';
import type { ActivityEvent } from '@/types/activity';
import { emptyTimelineWindow, replaceTimelineWindow } from './timelineWindow';
const batch = (number: number) => ({ items: Array.from({ length: 100 }, (_, index) => ({ eventId: `event-${number}-${index}`, seq: 10000 - number * 100 - index } as ActivityEvent)), nextCursor: `cursor-${number + 1}`, hasMore: number < 24 });
describe('bounded timeline navigation', () => {
  it('reads 2500 records in bounded batches and returns through adjacent snapshot cursors', () => {
    let window = replaceTimelineWindow(emptyTimelineWindow(), batch(0), 'initial');
    const seen = new Set(window.events.map(event => event.eventId));
    for (let number = 1; number < 25; number++) {
      window = replaceTimelineWindow(window, batch(number), 'older');
      expect(window.events).toHaveLength(100);
      window.events.forEach(event => seen.add(event.eventId));
      expect(window.previousCursors.every(cursor => typeof cursor === 'string')).toBe(true);
    }
    expect(seen.size).toBe(2500);
    expect(window.hasMore).toBe(false);
    for (let number = 23; number >= 0; number--) {
      window = replaceTimelineWindow(window, batch(number), 'newer');
      expect(window.cursor).toBe(number ? `cursor-${number}` : '');
      expect(window.events).toHaveLength(100);
      expect(window.events.every(event => event.eventId.startsWith(`event-${number}-`))).toBe(true);
    }
    expect(window.previousCursors).toEqual([]);
  });
});
