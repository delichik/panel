import type { ActivityEvent } from '@/types/activity';
import { mergeEvents } from './model';

export interface TimelineWindow {
  events: ActivityEvent[];
  cursor: string;
  nextCursor: string;
  hasMore: boolean;
  previousCursors: string[];
}
export const emptyTimelineWindow = (): TimelineWindow => ({ events: [], cursor: '', nextCursor: '', hasMore: false, previousCursors: [] });

/** Only the fetched batch is retained. Cursor history contains strings, never past event bodies. */
export function replaceTimelineWindow(current: TimelineWindow, page: { items: ActivityEvent[]; nextCursor?: string; hasMore: boolean }, direction: 'initial' | 'older' | 'newer'): TimelineWindow {
  const previousCursors = direction === 'initial' ? [] : [...current.previousCursors];
  let cursor = '';
  if (direction === 'older') { previousCursors.push(current.cursor); cursor = current.nextCursor; }
  if (direction === 'newer') cursor = previousCursors.pop() || '';
  return { events: mergeEvents([], page.items), cursor, nextCursor: page.nextCursor || '', hasMore: page.hasMore, previousCursors };
}
