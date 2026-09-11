import type { ActivityCapacity, ActivityEvent } from '@/types/activity';

/** Merge transport retries by immutable event identity, preserving receive order. */
export function mergeEvents(existing: ActivityEvent[], incoming: ActivityEvent[]): ActivityEvent[] {
  const events = new Map(existing.map((event) => [event.eventId, event]));
  for (const event of incoming) if (!events.has(event.eventId)) events.set(event.eventId, event);
  return [...events.values()].sort((a, b) => a.seq - b.seq);
}

function dataString(event: ActivityEvent, key: string) {
  const value = event.data?.[key];
  return typeof value === 'string' ? value.trim() : '';
}

function errorMessage(event: ActivityEvent) {
  const error = dataString(event, 'error');
  const errorCode = dataString(event, 'errorCode');
  const detail = dataString(event, 'detail');
  if (!error && !errorCode && !detail) return '';
  return [
    errorCode && !error.includes(errorCode) ? `[${errorCode}]` : '',
    error,
    detail && detail !== error ? `${error || errorCode ? '— ' : ''}${detail}` : '',
  ].filter(Boolean).join(' ');
}

export function eventMessage(event: ActivityEvent) {
  return errorMessage(event) || event.text || String(event.data?.message || event.data?.line || event.eventType);
}

/** Render known decision reasons as user-facing explanations while preserving raw event data separately. */
export function eventDisplayMessage(
  event: ActivityEvent,
  t: (key: string, params?: Record<string, string | number>) => string,
) {
  const error = errorMessage(event);
  if (error) return error;
  if (event.eventType !== 'observation.rejected') return eventMessage(event);

  const instance = dataString(event, 'instanceId') || t('activity.system');
  const reason = dataString(event, 'reason');
  const reasonKeys: Record<string, string> = {
    stale_or_ownership_lost: 'activity.observationRejected.staleOrOwnershipLost',
    stale: 'activity.observationRejected.stale',
    lease_lost: 'activity.observationRejected.leaseLost',
    ownership_lost: 'activity.observationRejected.ownershipLost',
    instance_missing: 'activity.observationRejected.instanceMissing',
    job_missing: 'activity.observationRejected.jobMissing',
    instance_fencing_mismatch: 'activity.observationRejected.instanceFencingMismatch',
    stale_observation: 'activity.observationRejected.staleObservation',
  };
  const key = reasonKeys[reason];
  return key
    ? t(key, { instance })
    : t('activity.observationRejected.other', { instance, reason: reason || t('common.notAvailable') });
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
