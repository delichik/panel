import type { ActivityEvent, ActivityOperation, ActivityPage } from '@/types/activity';

const now = Date.now();
export const mockActivityEvents: ActivityEvent[] = Array.from({ length: 246 }, (_, index) => {
  const seq = index + 1;
  const execution = seq < 241 ? 'exec-demo-1' : 'exec-demo-2';
  const eventType = seq === 1 ? 'operation.requested' : seq === 2 || seq === 241 ? 'execution.started' : seq === 239 ? 'execution.failed' : seq === 240 ? 'run.finished' : seq === 245 ? 'operation.finished' : seq === 246 ? 'agent.disconnected' : 'output.appended';
  return {
    eventId: `evt-demo-${seq}`, seq, eventVersion: 1, eventType,
    kind: eventType === 'output.appended' ? 'output' : seq === 1 ? 'request' : seq === 246 ? 'observation' : 'lifecycle',
    level: seq === 239 ? 'error' : seq === 246 ? 'warning' : 'info',
    domain: seq === 246 ? 'server' : 'application', action: seq === 246 ? 'agent.connect' : 'application.deploy',
    operationId: seq === 246 ? undefined : 'op-demo-deploy', runId: seq === 246 ? undefined : seq < 241 ? 'run-demo-1' : 'run-demo-2',
    executionId: seq === 246 ? undefined : execution, stepId: eventType === 'output.appended' ? `step-${execution}` : undefined,
    sourceId: 'agent/srv-edge-sgp', sourceEpoch: 'boot-demo', sourceStreamId: execution, sourceSeq: seq,
    occurredAt: new Date(now - (247 - seq) * 1500).toISOString(), recordedAt: new Date(now - (247 - seq) * 1500 + 50).toISOString(),
    actor: { kind: 'agent', id: 'srv-edge-sgp', name: 'Singapore edge' }, initiator: { kind: 'user', id: 'admin', name: 'admin' }, trigger: 'user',
    resources: [{ resourceType: 'server', resourceId: 'srv-edge-sgp', nameSnapshot: 'Singapore edge', role: 'target' }, ...(seq === 246 ? [] : [{ resourceType: 'application', resourceId: 'app-storefront', nameSnapshot: 'Storefront', revisionId: 'r18', role: 'subject' }])],
    stream: eventType === 'output.appended' ? 'stdout' : undefined,
    text: seq === 1 ? 'Deploying Storefront revision r18' : seq === 239 ? 'Gateway verification failed: upstream timeout' : seq === 240 ? 'First attempt partially succeeded' : seq === 245 ? 'Retry verified: all targets match revision r18' : seq === 246 ? 'Agent disconnected' : eventType === 'output.appended' ? `Preparing deployment output ${seq}: ${seq === 238 ? 'diagnostic-detail '.repeat(1000) : 'checking resource readiness'}` : 'Execution started',
    data: seq === 245 ? { result: 'succeeded' } : seq === 240 ? { result: 'partial' } : {},
  };
});
for (const [seq, eventType, text] of [[247, 'operation.requested', 'Restart worker service'], [248, 'uncertainty.detected', 'Remote request timed out; execution result requires verification']] as const) {
  mockActivityEvents.push({ ...mockActivityEvents[0]!, eventId: `evt-demo-${seq}`, seq, operationId: 'op-demo-uncertain', executionId: 'task-demo-unknown:1', eventType, kind: seq === 247 ? 'request' : 'integrity', level: seq === 248 ? 'warning' : 'info', domain: 'server', action: 'server.service_restart', sourceSeq: seq, text, occurredAt: new Date(now - (250 - seq) * 500).toISOString(), recordedAt: new Date(now - (250 - seq) * 500).toISOString(), data: seq === 248 ? { uncertainty: true } : {} });
}
function uncertainOperationAt(snapshot: number): ActivityOperation | undefined {
  const events = mockActivityEvents.filter(event => event.operationId === 'op-demo-uncertain' && event.seq <= snapshot);
  if (!events.length) return;
  const resolution = events.find(event => event.eventType === 'execution.manually_verified');
  return { operationId: 'op-demo-uncertain', title: 'Restart worker service', domain: 'server', action: 'server.service_restart', trigger: 'user', actor: { kind: 'user', id: 'admin', name: 'admin' }, resources: events[0]!.resources, phase: resolution ? 'ended' : 'waiting', result: resolution ? String(resolution.data?.outcome) : undefined, attention: !resolution || resolution.data?.outcome === 'failed', uncertainty: !resolution, failureSummary: resolution ? undefined : 'Remote execution outcome has not been verified', createdAt: events[0]!.recordedAt, updatedAt: events.at(-1)!.recordedAt, firstSeq: 247, lastSeq: events.at(-1)!.seq, eventCount: events.length, attemptCount: 1, evidenceComplete: Boolean(resolution), hadError: false };
}
export function resolveMockExecution(executionId: string, input: unknown): { operationId: string; taskId: string } | undefined {
  if (executionId !== 'task-demo-unknown' || mockActivityEvents.some(event => event.eventType === 'execution.manually_verified')) return;
  const value = input as { outcome?: string; reason?: string };
  if (!['succeeded', 'failed'].includes(value?.outcome || '') || !value.reason?.trim()) return;
  const seq = (mockActivityEvents.at(-1)?.seq || 0) + 1;
  mockActivityEvents.push({ ...mockActivityEvents.at(-1)!, eventId: `evt-demo-${seq}`, seq, sourceSeq: seq, occurredAt: new Date().toISOString(), recordedAt: new Date().toISOString(), eventType: 'execution.manually_verified', kind: 'observation', level: 'info', actor: { kind: 'user', id: 'admin', name: 'admin' }, text: 'User recorded a manually verified execution outcome', data: { outcome: value.outcome, reason: value.reason.trim(), verification: 'manual' } });
  return { operationId: 'op-demo-uncertain', taskId: executionId };
}
function operationAt(snapshotSeq: number): ActivityOperation {
  const events = mockActivityEvents.filter(event => event.seq <= snapshotSeq && event.operationId === 'op-demo-deploy');
  const latest = events.at(-1);
  return {
    operationId: 'op-demo-deploy', title: 'Deploying Storefront revision r18', domain: 'application', action: 'application.deploy', trigger: 'user', actor: { kind: 'user', id: 'admin', name: 'admin' },
    resources: events[0]?.resources || [], phase: snapshotSeq >= 245 ? 'ended' : snapshotSeq >= 241 ? 'running' : snapshotSeq >= 240 ? 'ended' : 'running',
    result: snapshotSeq >= 245 ? 'succeeded' : snapshotSeq >= 240 && snapshotSeq < 241 ? 'partial' : undefined,
    attention: snapshotSeq >= 239 && snapshotSeq < 245, uncertainty: false, hadError: snapshotSeq >= 239,
    failureSummary: snapshotSeq >= 239 && snapshotSeq < 245 ? 'Gateway verification failed: upstream timeout' : undefined,
    createdAt: events[0]?.recordedAt || '', updatedAt: latest?.recordedAt || '', firstSeq: 1, lastSeq: latest?.seq || 0,
    eventCount: events.length, attemptCount: snapshotSeq >= 241 ? 2 : 1, evidenceComplete: snapshotSeq >= 245,
  };
}
function filterEvents(params: URLSearchParams, snapshot: number) {
  return mockActivityEvents.filter(event => {
    if (event.seq > snapshot || event.seq <= Number(params.get('afterSeq') || 0)) return false;
    for (const key of ['operationId', 'executionId', 'stepId', 'domain', 'action', 'kind', 'trigger'] as const) if (params.get(key) && event[key] !== params.get(key) && !(key === 'executionId' && event.executionId?.startsWith(params.get(key)! + ':'))) return false;
    if (params.get('level') && !params.get('level')!.split(',').includes(event.level)) return false;
    if (params.get('actorId') && event.actor?.id !== params.get('actorId')) return false;
    if (params.get('resourceId') && !event.resources.some(resource => resource.resourceId === params.get('resourceId') && (!params.get('resourceType') || resource.resourceType === params.get('resourceType')))) return false;
    if (params.get('from') && event.occurredAt < params.get('from')!) return false;
    if (params.get('to') && event.occurredAt > params.get('to')!) return false;
    if (params.get('q') && !JSON.stringify(event).toLowerCase().includes(params.get('q')!.toLowerCase())) return false;
    return true;
  });
}
export function mockActivityRoute(url: URL, method: string): Response | undefined {
  if (!url.pathname.startsWith('/api/v1/activity/')) return;
  const json = (data: unknown) => new Response(JSON.stringify({ data }), { headers: { 'Content-Type': 'application/json' } });
  const fail = (code: string, status = 400) => new Response(JSON.stringify({ error: { code, message: code } }), { status, headers: { 'Content-Type': 'application/json' } });
  if (method !== 'GET') return fail('activity_read_only', 405);
  const params = url.searchParams;
  const snapshot = Number(params.get('snapshotSeq') || mockActivityEvents.at(-1)?.seq || 0);
  const limit = Number(params.get('limit') || 100);
  if (!Number.isInteger(limit) || limit < 1 || limit > 500) return fail('activity_invalid_limit');
  const headSeq = mockActivityEvents.at(-1)?.seq || 0;
  function page<T>(all: T[]): ActivityPage<T> & { total: number } {
    const cursor = params.get('cursor');
    const offset = cursor ? Number(cursor.split(':')[1]) : 0;
    const items = all.slice(offset, offset + limit);
    return { items, total: all.length, nextCursor: offset + limit < all.length ? `${snapshot}:${offset + limit}` : '', hasMore: offset + limit < all.length, snapshotSeq: snapshot, headSeq, projectedThroughSeq: snapshot, indexState: 'ready' };
  }
  const matching = filterEvents(params, snapshot);
  if (url.pathname.endsWith('/events') || url.pathname.endsWith('/tail')) return json(page(url.pathname.endsWith('/tail') ? matching : [...matching].reverse()));
  const context = url.pathname.match(/\/events\/([^/]+)\/context$/);
  const eventMatch = url.pathname.match(/\/events\/([^/]+)$/);
  const event = (context || eventMatch) && mockActivityEvents.find(item => item.eventId === decodeURIComponent((context || eventMatch)![1]!));
  if (context) {
    if (!event) return fail('activity_event_not_found', 404);
    const related = mockActivityEvents.filter(item => event.operationId ? item.operationId === event.operationId : item.resources.some(resource => event.resources.some(target => target.resourceId === resource.resourceId)));
    const index = related.findIndex(item => item.eventId === event.eventId);
    return json({ ...page(related.slice(Math.max(0, index - Number(params.get('before') || 30)), index + Number(params.get('after') || 30) + 1)), hasMore: false });
  }
  if (eventMatch) return event ? json(event) : fail('activity_event_not_found', 404);
  if (url.pathname.endsWith('/operations')) {
    const candidates = [uncertainOperationAt(snapshot), operationAt(snapshot)].filter((item): item is ActivityOperation => Boolean(item));
    const items = candidates.filter(operation => matching.some(item => item.operationId === operation.operationId) && (!params.get('phase') || params.get('phase') === operation.phase) && (!params.get('result') || params.get('result') === operation.result) && (!params.get('attention') || params.get('attention') === String(operation.attention)));
    return json(page(items));
  }
  const operationMatch = url.pathname.match(/\/operations\/([^/]+)$/);
  if (operationMatch) {
    if (operationMatch[1] === 'op-demo-uncertain') {
      const operation = uncertainOperationAt(snapshot);
      if (!operation) return fail('activity_operation_not_found', 404);
      return json({ operation, events: mockActivityEvents.filter(event => event.operationId === operation.operationId && event.seq <= snapshot), executions: [{ executionId: 'task-demo-unknown:1', phase: operation.phase, result: operation.result, resources: operation.resources }], steps: [], relatedOperations: [], availableCommands: operation.uncertainty ? [{ kind: 'resolve', executionId: 'task-demo-unknown', label: 'Record manual verification' }] : [], snapshotSeq: snapshot, headSeq, hasMore: false });
    }
    if (operationMatch[1] !== 'op-demo-deploy') return fail('activity_operation_not_found', 404);
    const result = page(mockActivityEvents.filter(item => item.operationId === 'op-demo-deploy' && item.seq <= snapshot).reverse());
    return json({ operation: operationAt(snapshot), events: result.items, executions: [{ executionId: 'exec-demo-1', runId: 'run-demo-1', phase: 'ended', result: 'partial', resources: mockActivityEvents[0]!.resources }, { executionId: 'exec-demo-2', runId: 'run-demo-2', phase: 'ended', result: 'succeeded', resources: mockActivityEvents[0]!.resources }], steps: [{ stepId: 'step-exec-demo-1', executionId: 'exec-demo-1', name: 'Verify gateway', phase: 'ended', result: 'failed' }, { stepId: 'step-exec-demo-2', executionId: 'exec-demo-2', name: 'Verify gateway', phase: 'ended', result: 'succeeded' }], relatedOperations: [], availableCommands: [], snapshotSeq: snapshot, headSeq, hasMore: result.hasMore, nextCursor: result.nextCursor });
  }
  if (url.pathname.endsWith('/summary')) {
    const byLevel: Record<string, number> = {}; const byDomain: Record<string, number> = {};
    for (const event of matching) { byLevel[event.level] = (byLevel[event.level] || 0) + 1; byDomain[event.domain] = (byDomain[event.domain] || 0) + 1; }
    return json({ total: matching.length, byLevel, byDomain, snapshotSeq: snapshot, headSeq, projectedThroughSeq: snapshot });
  }
  if (url.pathname.endsWith('/export')) return new Response([JSON.stringify({ kind: 'manifest', snapshotSeq: snapshot, count: matching.length }), ...matching.map(event => JSON.stringify(event))].join('\n'), { headers: { 'Content-Type': 'application/x-ndjson', 'Content-Disposition': 'attachment; filename="activity.jsonl"' } });
  return fail('activity_route_not_found', 404);
}
