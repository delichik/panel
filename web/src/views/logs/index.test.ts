// @vitest-environment jsdom
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils';
import { createMemoryHistory, createRouter, type Router } from 'vue-router';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import LogsPage from './index.vue';
import { logContextsApi } from '@/api/logContexts';
import { toastKey } from '@/components/ui/toast';
import { useI18n } from '@/i18n';
import type { LogContext, LogContextLine, LogContextPage as Page } from '@/types/logContexts';

const context = (id: string): LogContext => ({
  streamId: id,
  module: 'applications',
  origin: 'user',
  title: id,
  summary: `${id} summary`,
  hadError: true,
  updatedAt: '2026-09-15T00:00:00Z',
  startedAt: '2026-09-15T00:00:00Z',
});
const line = (msg: string): LogContextLine => ({
  ts: '2026-09-15T00:00:00Z',
  level: 'error',
  module: 'applications',
  streamId: 'first',
  msg,
});
const page = (id = 'first'): Page => ({
  items: [context(id)],
  hasMore: true,
  nextCursor: 'older',
});
let wrapper: VueWrapper | undefined;
let router: Router;

async function render(url = '/logs?from=2026-09-14T00:00:00Z') {
  router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/logs', component: LogsPage }] });
  await router.push(url);
  await router.isReady();
  wrapper = mount(LogsPage, {
    global: {
      plugins: [router],
      provide: { [toastKey as symbol]: { push: vi.fn() } },
      stubs: {
        ConsolePage: { template: '<section><header><slot name="actions" /></header><slot /></section>' },
        ListPage: { template: '<div><slot name="toolbar" /><slot /><slot name="pagination" /></div>' },
        Table: {
          props: ['rows'],
          template: '<div><div v-for="row in rows" :key="row.streamId">{{ row.title }}</div><slot name="actions" :row="rows[0]" /><slot name="empty" /></div>',
        },
        DateTimeRangePicker: true,
        Select: true,
        SearchInput: true,
        Dialog: {
          props: ['open', 'title'],
          template: '<div v-if="open" data-testid="log-detail"><h2>{{ title }}</h2><slot /></div>',
        },
      },
    },
  });
  await flushPromises();
  return wrapper;
}
function button(text: string) {
  return wrapper!.findAll('button').find((item) => item.text() === text)!;
}

function createStorage(): Storage {
  let store = new Map<string, string>();
  return {
    get length() { return store.size; },
    clear() { store = new Map(); },
    getItem(key: string) { return store.has(key) ? store.get(key)! : null; },
    key(index: number) { return Array.from(store.keys())[index] ?? null; },
    removeItem(key: string) { store.delete(key); },
    setItem(key: string, value: string) { store.set(key, String(value)); },
  };
}
vi.stubGlobal('localStorage', createStorage());

beforeEach(() => {
  vi.useFakeTimers();
  localStorage.setItem('panel.autoRefresh', '5');
  useI18n().setLocale('en');
  vi.spyOn(logContextsApi, 'list').mockResolvedValue(page());
  vi.spyOn(logContextsApi, 'get').mockResolvedValue(context('detail'));
  vi.spyOn(logContextsApi, 'lines').mockResolvedValue({ items: [line('first')], hasMore: false });
});
afterEach(() => {
  wrapper?.unmount();
  wrapper = undefined;
  vi.useRealTimers();
  vi.restoreAllMocks();
});

// UI-ACT-002/003: context list + lines dialog only, no timers or aggregate probes.
describe('logs simple list', () => {
  it('loads once with a default range and stays idle until manual refresh', async () => {
    const view = await render('/logs');
    expect(logContextsApi.list).toHaveBeenCalledTimes(1);
    expect(logContextsApi.list).toHaveBeenCalledWith(expect.objectContaining({ from: expect.any(String) }));
    expect(router.currentRoute.value.query.from).toBeTruthy();
    await vi.advanceTimersByTimeAsync(60_000);
    expect(logContextsApi.list).toHaveBeenCalledTimes(1);
    expect(view.text()).not.toMatch(/Snapshot #|New events|Every 5|Evidence|Technical details|View context/);
    await button('Refresh').trigger('click');
    await flushPromises();
    expect(logContextsApi.list).toHaveBeenCalledTimes(2);
  });

  it('pages with cursors without probing', async () => {
    await render('/logs?from=2026-09-14T00:00:00Z');
    await button('Next').trigger('click');
    await flushPromises();
    expect(logContextsApi.list).toHaveBeenLastCalledWith(expect.objectContaining({ cursor: 'older' }));
  });

  it('does not let an old filter response replace the current result', async () => {
    let finish!: (value: Page) => void;
    vi.mocked(logContextsApi.list).mockImplementationOnce(() => new Promise((resolve) => { finish = resolve; }));
    await render();
    await router.replace({ query: { from: '2026-09-14T00:00:00Z', operationId: 'op-latest' } });
    await flushPromises();
    finish(page('stale-result'));
    await flushPromises();
    expect(wrapper!.text()).not.toContain('stale-result');
    expect(wrapper!.text()).toContain('first');
  });

  it('opens a detail dialog and loads lines with diagnose levels', async () => {
    await render();
    await button('Details').trigger('click');
    await flushPromises();
    expect(wrapper!.find('[data-testid="log-detail"]').exists()).toBe(true);
    expect(wrapper!.text()).toContain('first');
    expect(logContextsApi.lines).toHaveBeenCalledWith('first', expect.objectContaining({ level: 'warning,error' }));
  });
});
