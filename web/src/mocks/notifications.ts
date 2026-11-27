import type { EventNotificationPage } from '@/types/notifications';

/** Simulated long-poll wait. The real hub parks up to 20s; the mock parks ~1s. */
const MOCK_WAIT_MS = 1000;

function parseAfter(raw: string | null) {
  if (!raw) return 0;
  const after = Number(raw);
  return Number.isFinite(after) && after > 0 ? Math.floor(after) : 0;
}

function waitFor(milliseconds: number, signal?: AbortSignal | null) {
  return new Promise<void>((resolve, reject) => {
    if (signal?.aborted) {
      reject(new DOMException('Aborted', 'AbortError'));
      return;
    }
    const onAbort = () => {
      clearTimeout(timer);
      reject(new DOMException('Aborted', 'AbortError'));
    };
    const timer = setTimeout(() => {
      signal?.removeEventListener('abort', onAbort);
      resolve();
    }, milliseconds);
    signal?.addEventListener('abort', onAbort);
  });
}

export function isNotificationsRoute(url: URL, method: string) {
  return method === 'GET' && url.pathname === '/api/v1/notifications';
}

/**
 * Mock long-poll endpoint.
 *
 * Answers after ~1s with an empty page and echoes the request cursor, so the dev
 * server / tests exercise the real "request parks, then resumes" contract instead
 * of a tight request loop. The request `AbortSignal` is honoured: aborting the
 * fetch (shell unmount, route change) rejects with an `AbortError`, which the API
 * client normalizes into `request_aborted`.
 */
export async function mockNotificationsRoute(url: URL, init?: RequestInit): Promise<Response> {
  const after = parseAfter(url.searchParams.get('after'));
  await waitFor(MOCK_WAIT_MS, init?.signal);

  const data: EventNotificationPage = { items: [], cursor: after };
  return new Response(JSON.stringify({ data }), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  });
}
