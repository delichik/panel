import type { ActivityCapacity, ActivityEvent } from '@/types/activity';

/** Merge transport retries by immutable event identity, preserving receive order. */
export function mergeEvents(existing: ActivityEvent[], incoming: ActivityEvent[]): ActivityEvent[] {
  const events = new Map(existing.map((event) => [event.eventId, event]));
  for (const event of incoming) if (!events.has(event.eventId)) events.set(event.eventId, event);
  return [...events.values()].sort((a, b) => a.seq - b.seq);
}
export function eventMessage(event: ActivityEvent) {
  return event.text || String(event.data?.message || event.data?.line || event.eventType);
}
export function eventStream(event: ActivityEvent) { return event.stream || String(event.data?.stream || ''); }
export function eventTone(level: string) {
  return level === 'error' ? 'danger' : level === 'warning' ? 'warning' : level === 'debug' ? 'neutral' : 'info';
}
export function operationTone(result?: string) {
  return result === 'succeeded' || result === 'success' || result === 'no_change' ? 'success' : result === 'failure' || result === 'failed' ? 'danger' : result === 'partial' || result === 'partial_success' || result === 'unknown' ? 'warning' : 'info';
}

export function manualResolution(outcome: string, reason: string): { outcome: 'succeeded' | 'failed'; reason: string } | undefined {
  if ((outcome !== 'succeeded' && outcome !== 'failed') || !reason.trim()) return;
  return { outcome, reason: reason.trim() };
}

export function capacityNotice(capacity?: ActivityCapacity): 'warning' | 'blocked' | undefined {
  return capacity?.state === 'warning' || capacity?.state === 'blocked' ? capacity.state : undefined;
}
