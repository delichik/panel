// @vitest-environment jsdom
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils';
import { createMemoryHistory, createRouter, type Router } from 'vue-router';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import LogsPage from './index.vue';
import { activityApi } from '@/api/activity';
import { toastKey } from '@/components/ui/toast';
import { useI18n } from '@/i18n';
import type { ActivityEvent, ActivityPage as Page } from '@/types/activity';

const event = (id: string): ActivityEvent => ({
  eventId: id,
  seq: 1,
  eventVersion: 1,
  eventType: 'execution.finished',
  kind: 'lifecycle',
  domain: 'application',
  level: 'info',
  text: id,
  resources: [],
  occurredAt: '2026-09-15T00:00:00Z',
  recordedAt: '2026-09-15T00:00:00Z',
  actor: { kind: 'system', id: 'panel', name: 'panel' },
});
const page = (id = 'first'): Page<ActivityEvent> => ({
  items: [event(id)],
  snapshotSeq: 10,
  headSeq: 10,
  projectedThroughSeq: 10,
  indexState: 'ready',
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
          template: '<div><div v-for="row in rows" :key="row.eventId">{{ row.text }}</div><slot name="actions" :row="rows[0]" /><slot name="empty" /></div>',
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
  vi.spyOn(activityApi, 'events').mockResolvedValue(page());
  vi.spyOn(activityApi, 'event').mockResolvedValue(event('detail'));
  vi.spyOn(activityApi, 'tail').mockRejectedValue(new Error('Unexpected polling'));
  vi.spyOn(activityApi, 'summary').mockRejectedValue(new Error('Unexpected aggregation'));
  vi.spyOn(activityApi, 'operation').mockRejectedValue(new Error('Unexpected operation detail'));
});
afterEach(() => {
  wrapper?.unmount();
  wrapper = undefined;
  vi.useRealTimers();
  vi.restoreAllMocks();
});

// UI-ACT-002/003: simple list + dialog only, no timers or aggregate probes.
describe('logs simple list', () => {
  it('loads once with a default range and stays idle until manual refresh', async () => {
    const view = await render('/logs');
    expect(activityApi.events).toHaveBeenCalledTimes(1);
    expect(activityApi.events).toHaveBeenCalledWith(expect.objectContaining({ level: 'warning,error' }));
    expect(router.currentRoute.value.query.from).toBeTruthy();
    await vi.advanceTimersByTimeAsync(60_000);
    expect(activityApi.events).toHaveBeenCalledTimes(1);
    expect(activityApi.tail).not.toHaveBeenCalled();
    expect(activityApi.summary).not.toHaveBeenCalled();
    expect(activityApi.operation).not.toHaveBeenCalled();
    expect(view.text()).not.toMatch(/Snapshot #|New events|Every 5|Evidence|Technical details|View context/);
    await button('Refresh').trigger('click');
    await flushPromises();
    expect(activityApi.events).toHaveBeenCalledTimes(2);
  });

  it('pages with cursors without probing', async () => {
    await render('/logs?from=2026-09-14T00:00:00Z');
    await button('Next').trigger('click');
    await flushPromises();
    expect(activityApi.events).toHaveBeenLastCalledWith(expect.objectContaining({ cursor: 'older', snapshotSeq: '10' }));
    expect(activityApi.tail).not.toHaveBeenCalled();
    expect(activityApi.summary).not.toHaveBeenCalled();
  });

  it('does not let an old filter response replace the current result', async () => {
    let finish!: (value: Page<ActivityEvent>) => void;
    vi.mocked(activityApi.events).mockImplementationOnce(() => new Promise((resolve) => { finish = resolve; }));
    await render();
    await router.replace({ query: { from: '2026-09-14T00:00:00Z', q: 'latest' } });
    await flushPromises();
    finish(page('stale-result'));
    await flushPromises();
    expect(wrapper!.text()).not.toContain('stale-result');
    expect(wrapper!.text()).toContain('first');
  });

  it('opens a detail dialog from the row action', async () => {
    await render();
    await button('Details').trigger('click');
    await flushPromises();
    expect(wrapper!.find('[data-testid="log-detail"]').exists()).toBe(true);
    expect(wrapper!.text()).toContain('Log detail');
    expect(wrapper!.text()).toContain('first');
    expect(activityApi.operation).not.toHaveBeenCalled();
  });
});
