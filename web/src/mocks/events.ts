import type { Event, EventPage, LogLine, LogLinesPage } from '@/types/events';

const now = Date.now();

function iso(offsetMs: number) {
  return new Date(now - offsetMs).toISOString();
}

export const mockEvents: Event[] = [
  {
    id: 'stream-deploy',
    module: 'applications',
    level: 'info',
    origin: 'user',
    operationId: 'op-demo-deploy',
    executionId: 'exec-demo-2',
    title: 'Deploying Storefront revision r18',
    summary: 'Retry verified: all targets match revision r18',
    state: 'succeeded',
    hadError: true,
    startedAt: iso(3600_000),
    updatedAt: iso(60_000),
    finishedAt: iso(60_000),
  },
  {
    id: 'stream-uncertain',
    module: 'servers',
    level: 'warning',
    origin: 'user',
    operationId: 'op-demo-uncertain',
    executionId: 'task-demo-unknown',
    title: 'Restart worker service',
    summary: 'Remote request timed out; execution result requires verification',
    state: 'unknown',
    hadError: false,
    startedAt: iso(1800_000),
    updatedAt: iso(120_000),
  },
  {
    id: 'stream-reconcile',
    module: 'orchestrator',
    level: 'info',
    origin: 'system',
    operationId: 'op-demo-reconcile',
    executionId: 'exec-reconcile-1',
    title: 'Reconcile Storefront desired state',
    summary: 'Observed state matches desired revision',
    state: 'succeeded',
    hadError: false,
    startedAt: iso(900_000),
    updatedAt: iso(90_000),
    finishedAt: iso(90_000),
  },
  {
    id: 'stream-process',
    module: 'http',
    level: 'warning',
    origin: 'process',
    title: 'Panel HTTP request handling',
    summary: 'Request completed with upstream timeout warning',
    state: 'running',
    hadError: true,
    startedAt: iso(300_000),
    updatedAt: iso(30_000),
  },
];

const mockLinesByEvent: Record<string, LogLine[]> = {
  'stream-deploy': [
    { ts: iso(3_500_000), level: 'info', module: 'applications', eventId: 'stream-deploy', msg: 'Deploying Storefront revision r18' },
    { ts: iso(2_000_000), level: 'error', module: 'applications', eventId: 'stream-deploy', msg: 'Gateway verification failed: upstream timeout', errorCode: 'upstream_timeout', error: 'upstream timeout', detail: 'probe exceeded 5s' },
    { ts: iso(1_000_000), level: 'warning', module: 'applications', eventId: 'stream-deploy', msg: 'First attempt partially succeeded' },
    { ts: iso(60_000), level: 'info', module: 'applications', eventId: 'stream-deploy', msg: 'Retry verified: all targets match revision r18' },
  ],
  'stream-uncertain': [
    { ts: iso(1_800_000), level: 'info', module: 'servers', eventId: 'stream-uncertain', msg: 'Restart worker service' },
    { ts: iso(120_000), level: 'warning', module: 'servers', eventId: 'stream-uncertain', msg: 'Remote request timed out; execution result requires verification' },
  ],
  'stream-reconcile': [
    { ts: iso(900_000), level: 'info', module: 'orchestrator', eventId: 'stream-reconcile', msg: 'Reconcile Storefront desired state' },
    { ts: iso(90_000), level: 'info', module: 'orchestrator', eventId: 'stream-reconcile', msg: 'Observed state matches desired revision' },
  ],
  'stream-process': [
    { ts: iso(300_000), level: 'debug', module: 'http', eventId: 'stream-process', msg: 'GET /api/v1/applications' },
    { ts: iso(30_000), level: 'warning', module: 'http', eventId: 'stream-process', msg: 'Upstream proxy timed out' },
  ],
};

function filterEvents(params: URLSearchParams): Event[] {
  return mockEvents.filter((item) => {
    if (params.get('origin') && item.origin !== params.get('origin')) return false;
    if (params.get('operationId') && item.operationId !== params.get('operationId')) return false;
    if (params.get('executionId') && item.executionId !== params.get('executionId') && !item.executionId?.startsWith(`${params.get('executionId')}:`)) return false;
    if (params.get('module') && item.module !== params.get('module')) return false;
    if (params.get('state') && item.state !== params.get('state')) return false;
    if (params.get('level') && item.level !== params.get('level')) return false;
    if (params.get('hadError') === 'true' && !item.hadError) return false;
    if (params.get('hadError') === 'false' && item.hadError) return false;
    if (params.get('from') && (item.updatedAt || item.startedAt || '') < params.get('from')!) return false;
    if (params.get('to') && (item.updatedAt || item.startedAt || '') > params.get('to')!) return false;
    return true;
  });
}

function pageOf<T>(all: T[], params: URLSearchParams, limitDefault: number): { items: T[]; nextCursor: string; hasMore: boolean } {
  const limit = Number(params.get('limit') || limitDefault);
  const offset = Number(params.get('cursor') || params.get('after') || 0);
  const items = all.slice(offset, offset + limit);
  const hasMore = offset + limit < all.length;
  return { items, nextCursor: hasMore ? String(offset + limit) : '', hasMore };
}

function filterLines(eventId: string, params: URLSearchParams): LogLine[] {
  const levels = (params.get('level') || '').split(',').map((item) => item.trim()).filter(Boolean);
  const source = eventId
    ? (mockLinesByEvent[eventId] || [])
    : Object.values(mockLinesByEvent).flat();
  return source.filter((line) => !levels.length || levels.includes(String(line.level || '')));
}

function jsonResponse(data: unknown) {
  return new Response(JSON.stringify({ data }), { headers: { 'Content-Type': 'application/json' } });
}

function failResponse(code: string, status = 400) {
  return new Response(JSON.stringify({ error: { code, message: code } }), { status, headers: { 'Content-Type': 'application/json' } });
}

function eventListResponse(params: URLSearchParams) {
  const matching = filterEvents(params);
  const page = pageOf(matching, params, 100);
  return jsonResponse({ items: page.items, nextCursor: page.nextCursor, hasMore: page.hasMore } satisfies EventPage);
}

function eventExportResponse(params: URLSearchParams) {
  const matching = filterEvents(params);
  const body = [JSON.stringify({ type: 'manifest', formatVersion: 1, totalHint: matching.length }), ...matching.map((item) => JSON.stringify(item))].join('\n');
  return new Response(body, {
    headers: {
      'Content-Type': 'application/x-ndjson',
      'Content-Disposition': 'attachment; filename="events.jsonl"',
    },
  });
}

function eventLinesResponse(eventId: string, params: URLSearchParams, requireEvent = true) {
  if (eventId && requireEvent && !mockEvents.some((item) => item.id === eventId)) {
    return failResponse('event_not_found', 404);
  }
  const page = pageOf(filterLines(eventId, params), params, 200);
  return jsonResponse({ items: page.items, nextCursor: page.nextCursor, hasMore: page.hasMore } satisfies LogLinesPage);
}

function eventGetResponse(eventId: string) {
  const found = mockEvents.find((item) => item.id === eventId);
  return found ? jsonResponse(found) : failResponse('event_not_found', 404);
}

export function mockEventsRoute(url: URL, method: string): Response | undefined {
  if (method !== 'GET') {
    if (url.pathname.startsWith('/api/v1/events') || url.pathname.startsWith('/api/v1/log-contexts') || url.pathname === '/api/v1/logs') {
      return failResponse('events_read_only', 405);
    }
    return;
  }

  const params = url.searchParams;

  if (url.pathname === '/api/v1/events' || url.pathname === '/api/v1/events/') {
    return eventListResponse(params);
  }
  if (url.pathname === '/api/v1/events/export') {
    return eventExportResponse(params);
  }
  if (url.pathname === '/api/v1/log-contexts' || url.pathname === '/api/v1/log-contexts/') {
    return eventListResponse(params);
  }
  if (url.pathname === '/api/v1/log-contexts/export') {
    return eventExportResponse(params);
  }
  if (url.pathname === '/api/v1/logs' || url.pathname === '/api/v1/logs/') {
    const eventId = decodeURIComponent(params.get('eventId') || '');
    return eventLinesResponse(eventId, params, false);
  }

  const eventLogsMatch = url.pathname.match(/^\/api\/v1\/events\/([^/]+)\/(?:logs|lines)$/);
  if (eventLogsMatch) {
    return eventLinesResponse(decodeURIComponent(eventLogsMatch[1]!), params);
  }
  const eventGetMatch = url.pathname.match(/^\/api\/v1\/events\/([^/]+)$/);
  if (eventGetMatch) {
    return eventGetResponse(decodeURIComponent(eventGetMatch[1]!));
  }

  const legacyLinesMatch = url.pathname.match(/^\/api\/v1\/log-contexts\/([^/]+)\/lines$/);
  if (legacyLinesMatch) {
    return eventLinesResponse(decodeURIComponent(legacyLinesMatch[1]!), params);
  }
  const legacyGetMatch = url.pathname.match(/^\/api\/v1\/log-contexts\/([^/]+)$/);
  if (legacyGetMatch) {
    return eventGetResponse(decodeURIComponent(legacyGetMatch[1]!));
  }

  if (url.pathname.startsWith('/api/v1/events') || url.pathname.startsWith('/api/v1/log-contexts')) {
    return failResponse('events_route_not_found', 404);
  }
  return;
}

/** @deprecated Use mockEventsRoute */
export const mockLogContextsRoute = mockEventsRoute;
