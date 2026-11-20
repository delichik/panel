import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { notificationsApi } from './notifications';

const fetchMock = vi.fn();
vi.stubGlobal('fetch', fetchMock);

function jsonResponse(data: unknown) {
  return new Response(JSON.stringify({ data }), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  });
}

beforeEach(() => {
  fetchMock.mockReset();
  fetchMock.mockResolvedValue(jsonResponse({ items: [], cursor: 0 }));
});

afterEach(() => {
  vi.useRealTimers();
});

describe('notifications API', () => {
  it('long-polls the notification cursor with the default 20s window', async () => {
    fetchMock.mockResolvedValue(jsonResponse({
      items: [{ seq: 3, eventId: 'evt-1', module: 'applications', title: 'Deploying', level: 'info', hadError: false, updatedAt: '2026-11-19T00:00:00Z' }],
      cursor: 3,
    }));

    const page = await notificationsApi.wait(12);

    expect(fetchMock.mock.calls[0]?.[0]).toBe('/api/v1/notifications?after=12&wait=20');
    expect(page.cursor).toBe(3);
    expect(page.items).toHaveLength(1);
    expect(page.items[0]?.eventId).toBe('evt-1');
  });

  it('honours an explicit wait window and baseline cursor', async () => {
    await notificationsApi.wait(0, 5);
    expect(fetchMock.mock.calls[0]?.[0]).toBe('/api/v1/notifications?after=0&wait=5');
  });

  it('aborts the request after the wait window plus 10 seconds', async () => {
    vi.useFakeTimers();
    let capturedSignal: AbortSignal | undefined;
    fetchMock.mockImplementation((_input, init) => new Promise((_resolve, reject) => {
      capturedSignal = (init as RequestInit).signal as AbortSignal;
      capturedSignal.addEventListener('abort', () => reject(new DOMException('Aborted', 'AbortError')));
    }));

    const pending = notificationsApi.wait(0, 5).catch((error: unknown) => error);
    await vi.advanceTimersByTimeAsync(14_999);
    expect(capturedSignal?.aborted).toBe(false);

    await vi.advanceTimersByTimeAsync(1);
    expect(capturedSignal?.aborted).toBe(true);
    await expect(pending).resolves.toMatchObject({ code: 'request_timeout' });
  });

  it('propagates an external abort signal as request_aborted', async () => {
    fetchMock.mockImplementation((_input, init) => new Promise((_resolve, reject) => {
      (init as RequestInit).signal!.addEventListener('abort', () => reject(new DOMException('Aborted', 'AbortError')));
    }));

    const controller = new AbortController();
    const pending = notificationsApi.wait(3, 20, controller.signal).catch((error: unknown) => error);
    controller.abort();

    await expect(pending).resolves.toMatchObject({ code: 'request_aborted' });
  });
});
