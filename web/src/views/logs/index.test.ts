// @vitest-environment jsdom
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils';
import { createMemoryHistory, createRouter, type Router } from 'vue-router';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import LogsPage from './index.vue';
import { eventsApi } from '@/api/events';
import { logsApi } from '@/api/logs';
import { toastKey } from '@/components/ui/toast';
import { useI18n } from '@/i18n';
import type { Event, LogLine, LogLinesPage as Page } from '@/types/events';

const line = (msg: string): LogLine => ({
  ts: '2026-09-15T00:00:00Z',
  level: 'error',
  module: 'applications',
  eventId: 'first',
  msg,
});
const page = (): Page => ({
  items: [line('first')],
  hasMore: true,
  nextCursor: '2',
});
const eventMeta: Event = {
  id: 'first',
  module: 'applications',
  level: 'error',
  origin: 'user',
  title: 'first',
  state: 'failed',
  hadError: true,
};
let wrapper: VueWrapper | undefined;
let router: Router;

async function render(url = '/logs?eventId=first&from=2026-09-14T00:00:00Z') {
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
          template: '<div><div v-for="(row, index) in rows" :key="index">{{ row.msg }}</div><slot name="empty" /></div>',
        },
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
  vi.spyOn(logsApi, 'list').mockResolvedValue(page());
  vi.spyOn(eventsApi, 'get').mockResolvedValue(eventMeta);
  vi.spyOn(eventsApi, 'list').mockResolvedValue({ items: [eventMeta], hasMore: false });
});
afterEach(() => {
  wrapper?.unmount();
  wrapper = undefined;
  vi.restoreAllMocks();
});

describe('logs line list', () => {
  it('loads lines for eventId with diagnose levels', async () => {
    const view = await render();
    expect(logsApi.list).toHaveBeenCalledWith(expect.objectContaining({ eventId: 'first', level: 'warning,error' }));
    expect(view.text()).toContain('first');
    await button('Refresh').trigger('click');
    await flushPromises();
    expect(logsApi.list).toHaveBeenCalledTimes(2);
  });

  it('prompts to pick an event when eventId is missing', async () => {
    const view = await render('/logs');
    expect(view.text()).toContain('Choose an event');
    expect(logsApi.list).not.toHaveBeenCalled();
  });

  it('resolves operationId to eventId before loading lines', async () => {
    await render('/logs?operationId=op-demo-deploy');
    await flushPromises();
    expect(eventsApi.list).toHaveBeenCalledWith(expect.objectContaining({ operationId: 'op-demo-deploy' }));
    expect(logsApi.list).toHaveBeenCalledWith(expect.objectContaining({ eventId: 'first' }));
  });
});
