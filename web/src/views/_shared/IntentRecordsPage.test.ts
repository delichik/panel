// @vitest-environment jsdom
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils';
import { createMemoryHistory, createRouter, type Router } from 'vue-router';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import IntentRecordsPage from './IntentRecordsPage.vue';
import { eventsApi } from '@/api/events';
import { toastKey } from '@/components/ui/toast';
import { useI18n } from '@/i18n';
import type { Event, EventPage as Page } from '@/types/events';

const event = (id: string): Event => ({
  id,
  module: 'applications',
  level: 'info',
  origin: 'user',
  title: id,
  summary: `${id} summary`,
  state: 'running',
  hadError: false,
  updatedAt: '2026-09-15T00:00:00Z',
  startedAt: '2026-09-15T00:00:00Z',
});
const page = (id = 'stream-1'): Page => ({
  items: [event(id)],
  hasMore: false,
});
let wrapper: VueWrapper | undefined;
let router: Router;

async function render(url = '/events?from=2026-09-14T00:00:00Z') {
  router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/events', component: IntentRecordsPage }] });
  await router.push(url);
  await router.isReady();
  wrapper = mount(IntentRecordsPage, {
    global: {
      plugins: [router],
      provide: { [toastKey as symbol]: { push: vi.fn() } },
      stubs: {
        ConsolePage: { template: '<section><header><slot name="actions" /></header><slot /></section>' },
        ListPage: { template: '<div><slot name="toolbar" /><slot /><slot name="pagination" /></div>' },
        TableDetailLayout: { template: '<div><slot name="list" /><slot name="detail" /></div>' },
        Table: {
          props: ['rows'],
          template: '<div><div v-for="row in rows" :key="row.id">{{ row.title }}</div><slot name="actions" :row="rows[0]" /><slot name="empty" /></div>',
        },
        DateTimeRangePicker: true,
        Select: true,
        SearchInput: true,
      },
    },
  });
  await flushPromises();
  return wrapper;
}
function button(text: string) {
  return wrapper!.findAll('button').find((item) => item.text() === text)!;
}

beforeEach(() => {
  useI18n().setLocale('en');
  vi.spyOn(eventsApi, 'list').mockResolvedValue(page());
  vi.spyOn(eventsApi, 'get').mockResolvedValue(event('stream-1'));
});
afterEach(() => {
  wrapper?.unmount();
  wrapper = undefined;
  vi.restoreAllMocks();
});

describe('Events list contract', () => {
  it('loads events and opens detail by eventId', async () => {
    await render('/events?from=2026-09-14T00:00:00Z&eventId=stream-1');
    expect(eventsApi.list).toHaveBeenCalledWith(expect.objectContaining({ from: expect.any(String) }));
    expect(eventsApi.get).toHaveBeenCalledWith('stream-1');
    await button('View logs').trigger('click');
    await flushPromises();
    expect(router.currentRoute.value.path).toBe('/logs');
    expect(router.currentRoute.value.query.eventId).toBe('stream-1');
  });

  it('accepts legacy streamId query as eventId', async () => {
    await render('/events?from=2026-09-14T00:00:00Z&streamId=stream-1');
    expect(eventsApi.get).toHaveBeenCalledWith('stream-1');
  });
});
