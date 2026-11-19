// @vitest-environment jsdom
import { mount, type VueWrapper } from '@vue/test-utils';
import { defineComponent, nextTick } from 'vue';
import { createMemoryHistory, createRouter } from 'vue-router';
import { afterEach, beforeEach, describe, expect, it, vi, type MockInstance } from 'vitest';
import { ApiError } from '@/api/client';
import { notificationsApi } from '@/api/notifications';
import ToastProvider from '@/components/ui/ToastProvider.vue';
import { useI18n } from '@/i18n';
import type { EventNotification, EventNotificationPage } from '@/types/notifications';
import EventNotificationBridge from './EventNotificationBridge.vue';

const mounts: VueWrapper[] = [];
let waitMock: MockInstance<typeof notificationsApi.wait>;

function page(items: EventNotification[] = [], cursor = 0): EventNotificationPage {
  return { items, cursor };
}

function notification(overrides: Partial<EventNotification> = {}): EventNotification {
  return {
    seq: 1,
    eventId: 'evt-1',
    module: 'applications',
    title: 'Deployment started',
    summary: '',
    state: 'running',
    level: 'info',
    origin: 'system',
    hadError: false,
    updatedAt: '2026-11-19T00:00:00Z',
    ...overrides,
  };
}

/** Never-resolving long poll for the trailing round of a test. */
function pending(): Promise<EventNotificationPage> {
  return new Promise<EventNotificationPage>(() => {});
}

async function mountBridge() {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { template: '<div />' } },
      { path: '/events', component: { template: '<div />' } },
    ],
  });
  await router.push('/');
  await router.isReady();
  const Host = defineComponent({
    components: { ToastProvider, EventNotificationBridge },
    template: '<ToastProvider><EventNotificationBridge /></ToastProvider>',
  });
  const wrapper = mount(Host, { attachTo: document.body, global: { plugins: [router] } });
  mounts.push(wrapper);
  return { wrapper, router };
}

/** Advance fake time and flush the microtasks (and Vue DOM updates) it triggers. */
async function tick(ms = 0) {
  await vi.advanceTimersByTimeAsync(ms);
  await nextTick();
}

function bubbles() {
  return [...document.querySelectorAll<HTMLElement>('section[data-tone]')];
}

beforeEach(() => {
  useI18n().setLocale('en');
  waitMock = vi.spyOn(notificationsApi, 'wait');
  vi.useFakeTimers();
});

afterEach(() => {
  while (mounts.length) mounts.pop()!.unmount();
  document.body.innerHTML = '';
  vi.restoreAllMocks();
  vi.useRealTimers();
  useI18n().setLocale('zh-CN');
});

describe('EventNotificationBridge', () => {
  it('uses the first long poll only to establish the baseline and never replays it', async () => {
    const pages = [page([notification({ seq: 7 })], 9), pending()];
    waitMock.mockImplementation(() => Promise.resolve(pages.shift()!));

    await mountBridge();
    await tick(0);

    expect(waitMock.mock.calls[0]?.slice(0, 2)).toEqual([0, 20]);
    expect(waitMock.mock.calls[1]?.slice(0, 2)).toEqual([9, 20]);
    expect(bubbles()).toHaveLength(0);
  });

  it('keeps at least a one-second idle gap when the server returns empty pages immediately', async () => {
    // Every round resolves instantly with an empty page. Without the idle guard
    // the loop would hammer the endpoint at 0ms intervals (busy polling).
    const pages = [page([], 0), page([], 0), page([], 0), pending()];
    waitMock.mockImplementation(() => Promise.resolve(pages.shift()!));

    await mountBridge();
    await tick(0);
    expect(waitMock).toHaveBeenCalledTimes(1);

    await tick(999);
    expect(waitMock).toHaveBeenCalledTimes(1);
    await tick(1);
    expect(waitMock).toHaveBeenCalledTimes(2);

    await tick(999);
    expect(waitMock).toHaveBeenCalledTimes(2);
    await tick(1);
    expect(waitMock).toHaveBeenCalledTimes(3);
  });

  it('turns event items into toned toasts and continues poll immediately when data arrived', async () => {
    const pages = [
      page([], 10),
      page([
        notification({ seq: 11, eventId: 'evt-run', title: 'Deploy storefront', state: 'running', summary: 'Deploy storefront' }),
        notification({ seq: 12, eventId: 'evt-fail', title: '', state: 'failed', summary: 'Gateway verification failed' }),
        notification({ seq: 13, eventId: 'evt-partial', title: 'Deploy partially succeeded', state: 'partial_success', summary: 'Two of three targets updated' }),
      ], 13),
      pending(),
    ];
    waitMock.mockImplementation(() => Promise.resolve(pages.shift()!));

    await mountBridge();
    await tick(0);
    expect(waitMock).toHaveBeenCalledTimes(1);

    // The baseline returned an empty page after ~0ms, so the next round waits 1s.
    await tick(1000);
    expect(waitMock).toHaveBeenCalledTimes(2);
    expect(waitMock.mock.calls[1]?.slice(0, 2)).toEqual([10, 20]);
    // A data-bearing batch reschedules with no idle delay: the next round starts immediately.
    await tick(1);
    expect(waitMock).toHaveBeenCalledTimes(3);
    expect(waitMock.mock.calls[2]?.slice(0, 2)).toEqual([13, 20]);

    const rendered = bubbles();
    expect(rendered).toHaveLength(3);
    expect(rendered[0]?.dataset.tone).toBe('info');
    expect(rendered[0]?.textContent).toContain('Deploy storefront');
    expect(rendered[0]?.textContent).toContain('Running');
    expect(rendered[1]?.dataset.tone).toBe('danger');
    expect(rendered[1]?.textContent).toContain('evt-fail');
    expect(rendered[1]?.textContent).toContain('Failed · Gateway verification failed');
    expect(rendered[2]?.dataset.tone).toBe('warning');
    expect(rendered[2]?.textContent).toContain('Partially succeeded · Two of three targets updated');
    const links = rendered.map((bubble) => bubble.querySelector('a')?.getAttribute('href'));
    expect(links).toEqual([
      '/events?eventId=evt-run',
      '/events?eventId=evt-fail',
      '/events?eventId=evt-partial',
    ]);
  });

  it('maps success and empty states to the documented tones and de-duplicates by seq', async () => {
    const pages = [
      page([], 0),
      page([notification({ seq: 21, eventId: 'evt-ok', title: 'Backup finished', state: 'succeeded' })], 21),
      page([
        notification({ seq: 21, eventId: 'evt-ok', title: 'Backup finished', state: 'succeeded' }),
        notification({ seq: 22, eventId: 'evt-info', title: 'Certificate renewal started', state: '' }),
      ], 22),
      pending(),
    ];
    waitMock.mockImplementation(() => Promise.resolve(pages.shift()!));

    await mountBridge();
    await tick(0);
    await tick(1000);
    // The second data page is fetched immediately after the first one (no 1s idle gap).
    await tick(1);

    const rendered = bubbles();
    expect(rendered).toHaveLength(2);
    expect(rendered[0]?.dataset.tone).toBe('success');
    expect(rendered[1]?.dataset.tone).toBe('info');
    expect(rendered.map((bubble) => bubble.textContent)).toEqual([
      expect.stringContaining('Backup finished'),
      expect.stringContaining('Certificate renewal started'),
    ]);
  });

  it('pauses polling while the page is hidden and resumes when visible again', async () => {
    const visibility = vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden');
    const pages = [page([], 4), pending()];
    waitMock.mockImplementation(() => Promise.resolve(pages.shift()!));

    await mountBridge();
    await tick(0);
    expect(waitMock).not.toHaveBeenCalled();

    visibility.mockReturnValue('visible');
    document.dispatchEvent(new Event('visibilitychange'));
    await tick(0);
    expect(waitMock).toHaveBeenCalledTimes(1);
    expect(waitMock.mock.calls[0]?.slice(0, 2)).toEqual([0, 20]);

    await tick(1000);
    expect(waitMock).toHaveBeenCalledTimes(2);
  });

  it('backs off for five seconds on server errors without toasting or opening the error dialog', async () => {
    waitMock.mockRejectedValue(new ApiError('offline', 0, 'network_error'));

    await mountBridge();
    await tick(0);

    expect(waitMock).toHaveBeenCalledTimes(1);
    expect(bubbles()).toHaveLength(0);
    expect(document.querySelector('[data-testid="error-dialog"]')).toBeNull();

    await tick(4_999);
    expect(waitMock).toHaveBeenCalledTimes(1);

    await tick(1);
    expect(waitMock).toHaveBeenCalledTimes(2);
  });

  it('silently stops polling when the request is aborted', async () => {
    waitMock.mockRejectedValue(new ApiError('aborted', 0, 'request_aborted'));

    await mountBridge();
    await tick(0);
    await tick(5_000);

    expect(waitMock).toHaveBeenCalledTimes(1);
    expect(bubbles()).toHaveLength(0);
  });

  it('aborts the in-flight long poll when the shell unmounts', async () => {
    waitMock.mockImplementation((_after, _wait, signal) => new Promise<EventNotificationPage>((_resolve, reject) => {
      signal?.addEventListener('abort', () => reject(new ApiError('aborted', 0, 'request_aborted')));
    }));

    const { wrapper } = await mountBridge();
    await tick(0);
    const signal = waitMock.mock.calls[0]?.[2] as AbortSignal;
    expect(signal.aborted).toBe(false);

    wrapper.unmount();
    await tick(0);

    expect(signal.aborted).toBe(true);
  });
});

