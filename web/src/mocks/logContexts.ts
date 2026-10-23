import type { LogContext, LogContextLine, LogContextPage, LogContextLinesPage } from '@/types/logContexts';

const now = Date.now();

function iso(offsetMs: number) {
  return new Date(now - offsetMs).toISOString();
}

export const mockLogContexts: LogContext[] = [
  {
    streamId: 'stream-deploy',
    module: 'applications',
    origin: 'user',
    operationId: 'op-demo-deploy',
    executionId: 'exec-demo-2',
    title: 'Deploying Storefront revision r18',
    summary: 'Retry verified: all targets match revision r18',
    result: 'succeeded',
    hadError: true,
    startedAt: iso(3600_000),
    updatedAt: iso(60_000),
    finishedAt: iso(60_000),
    actor: { kind: 'user', id: 'admin', name: 'admin' },
    resources: [
      { resourceType: 'application', resourceId: 'app-storefront', nameSnapshot: 'Storefront', role: 'subject' },
      { resourceType: 'server', resourceId: 'srv-edge-sgp', nameSnapshot: 'Singapore edge', role: 'target' },
    ],
  },
  {
    streamId: 'stream-uncertain',
    module: 'servers',
    origin: 'user',
    operationId: 'op-demo-uncertain',
    executionId: 'task-demo-unknown',
    title: 'Restart worker service',
    summary: 'Remote request timed out; execution result requires verification',
    result: 'unknown',
    hadError: false,
    startedAt: iso(1800_000),
    updatedAt: iso(120_000),
    actor: { kind: 'user', id: 'admin', name: 'admin' },
    resources: [{ resourceType: 'server', resourceId: 'srv-edge-sgp', nameSnapshot: 'Singapore edge', role: 'target' }],
  },
  {
    streamId: 'stream-reconcile',
    module: 'orchestrator',
    origin: 'system',
    operationId: 'op-demo-reconcile',
    executionId: 'exec-reconcile-1',
    title: 'Reconcile Storefront desired state',
    summary: 'Observed state matches desired revision',
    result: 'succeeded',
    hadError: false,
    startedAt: iso(900_000),
    updatedAt: iso(90_000),
    finishedAt: iso(90_000),
  },
  {
    streamId: 'stream-process',
    module: 'http',
    origin: 'process',
    title: 'Panel HTTP request handling',
    summary: 'Request completed with upstream timeout warning',
    result: '',
    hadError: true,
    startedAt: iso(300_000),
    updatedAt: iso(30_000),
  },
];

const mockLinesByStream: Record<string, LogContextLine[]> = {
  'stream-deploy': [
    { ts: iso(3_500_000), level: 'info', module: 'applications', streamId: 'stream-deploy', msg: 'Deploying Storefront revision r18' },
    { ts: iso(2_000_000), level: 'error', module: 'applications', streamId: 'stream-deploy', msg: 'Gateway verification failed: upstream timeout', errorCode: 'upstream_timeout', error: 'upstream timeout', detail: 'probe exceeded 5s' },
    { ts: iso(1_000_000), level: 'warning', module: 'applications', streamId: 'stream-deploy', msg: 'First attempt partially succeeded' },
    { ts: iso(60_000), level: 'info', module: 'applications', streamId: 'stream-deploy', msg: 'Retry verified: all targets match revision r18' },
  ],
  'stream-uncertain': [
    { ts: iso(1_800_000), level: 'info', module: 'servers', streamId: 'stream-uncertain', msg: 'Restart worker service' },
    { ts: iso(120_000), level: 'warning', module: 'servers', streamId: 'stream-uncertain', msg: 'Remote request timed out; execution result requires verification' },
  ],
  'stream-reconcile': [
    { ts: iso(900_000), level: 'info', module: 'orchestrator', streamId: 'stream-reconcile', msg: 'Reconcile Storefront desired state' },
    { ts: iso(90_000), level: 'info', module: 'orchestrator', streamId: 'stream-reconcile', msg: 'Observed state matches desired revision' },
  ],
  'stream-process': [
    { ts: iso(300_000), level: 'debug', module: 'http', streamId: 'stream-process', msg: 'GET /api/v1/applications' },
    { ts: iso(30_000), level: 'warning', module: 'http', streamId: 'stream-process', msg: 'Upstream proxy timed out' },
  ],
};

function filterContexts(params: URLSearchParams): LogContext[] {
  return mockLogContexts.filter((item) => {
    if (params.get('origin') && item.origin !== params.get('origin')) return false;
    if (params.get('operationId') && item.operationId !== params.get('operationId')) return false;
    if (params.get('executionId') && item.executionId !== params.get('executionId') && !item.executionId?.startsWith(`${params.get('executionId')}:`)) return false;
    if (params.get('module') && item.module !== params.get('module')) return false;
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

function filterLines(streamId: string, params: URLSearchParams): LogContextLine[] {
  const levels = (params.get('level') || '').split(',').map((item) => item.trim()).filter(Boolean);
  return (mockLinesByStream[streamId] || []).filter((line) => !levels.length || levels.includes(String(line.level || '')));
}

export function mockLogContextsRoute(url: URL, method: string): Response | undefined {
  if (!url.pathname.startsWith('/api/v1/log-contexts')) return;
  const json = (data: unknown) => new Response(JSON.stringify({ data }), { headers: { 'Content-Type': 'application/json' } });
  const fail = (code: string, status = 400) => new Response(JSON.stringify({ error: { code, message: code } }), { status, headers: { 'Content-Type': 'application/json' } });
  if (method !== 'GET') return fail('log_contexts_read_only', 405);

  const params = url.searchParams;
  if (url.pathname === '/api/v1/log-contexts' || url.pathname === '/api/v1/log-contexts/') {
    const matching = filterContexts(params);
    const page = pageOf(matching, params, 100);
    return json({ items: page.items, nextCursor: page.nextCursor, hasMore: page.hasMore } satisfies LogContextPage);
  }
  if (url.pathname === '/api/v1/log-contexts/export') {
    const matching = filterContexts(params);
    const body = [JSON.stringify({ type: 'manifest', formatVersion: 1, totalHint: matching.length }), ...matching.map((item) => JSON.stringify(item))].join('\n');
    return new Response(body, {
      headers: {
        'Content-Type': 'application/x-ndjson',
        'Content-Disposition': 'attachment; filename="log-contexts.jsonl"',
      },
    });
  }
  const linesMatch = url.pathname.match(/^\/api\/v1\/log-contexts\/([^/]+)\/lines$/);
  if (linesMatch) {
    const streamId = decodeURIComponent(linesMatch[1]!);
    if (!mockLogContexts.some((item) => item.streamId === streamId)) return fail('log_context_not_found', 404);
    const page = pageOf(filterLines(streamId, params), params, 200);
    return json({ items: page.items, nextCursor: page.nextCursor, hasMore: page.hasMore } satisfies LogContextLinesPage);
  }
  const getMatch = url.pathname.match(/^\/api\/v1\/log-contexts\/([^/]+)$/);
  if (getMatch) {
    const streamId = decodeURIComponent(getMatch[1]!);
    const found = mockLogContexts.find((item) => item.streamId === streamId);
    return found ? json(found) : fail('log_context_not_found', 404);
  }
  return fail('log_contexts_route_not_found', 404);
}
