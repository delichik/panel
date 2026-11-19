// @vitest-environment jsdom
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
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
const toastPush = vi.fn();

async function render(url = '/logs?eventId=first&from=2026-09-14T00:00:00Z&level=info,warning,error') {
  router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/logs', component: LogsPage }] });
  await router.push(url);
  await router.isReady();
  wrapper = mount(LogsPage, {
    global: {
      plugins: [router],
      provide: { [toastKey as symbol]: { push: toastPush } },
      stubs: {
        ConsolePage: { template: '<section><header><slot name="actions" /></header><slot /></section>' },
        ListPage: {
          props: ['state'],
          template: '<div><slot name="toolbar" /><slot v-if="state === \'error\'" name="error" /><slot v-else /><slot name="pagination" /></div>',
        },
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
  toastPush.mockClear();
  vi.spyOn(logsApi, 'list').mockResolvedValue(page());
  vi.spyOn(eventsApi, 'get').mockResolvedValue(eventMeta);
  vi.spyOn(eventsApi, 'list').mockResolvedValue({ items: [eventMeta], hasMore: false });
});
afterEach(() => {
  wrapper?.unmount();
  wrapper = undefined;
  vi.restoreAllMocks();
});

describe('Logs narrow viewport contract', () => {
  const source = readFileSync(resolve(__dirname, './index.vue'), 'utf8');
  const columnsSource = source.slice(source.indexOf('const columns = computed'), source.indexOf('const selectedLine'));

  it('hides eventId below wide and the time column below sm', () => {
    expect(columnsSource).toMatch(/key: 'eventId'[^\n]*hideBelow: 'wide'/);
    expect(columnsSource).toMatch(/key: 'ts'[^\n]*hideBelow: 'sm'/);
    // <sm 只保留 msg 与操作两列。
    expect(columnsSource).not.toMatch(/key: 'msg'[^\n]*hideBelow/);
    expect(columnsSource).not.toMatch(/key: 'actions'[^\n]*hideBelow/);
    // 旧 level 独立列已移除（并入时间列）。
    expect(columnsSource).not.toContain("key: 'level'");
  });

  it('keeps the multi-line message renderer, eventId tooltip and stacked dialog footer', () => {
    expect(source).toContain('whitespace-pre-wrap');
    expect(source).toContain(':title="String(row.eventId || eventId)"');
    expect((source.match(/max-sm:w-full/g) ?? []).length).toBeGreaterThanOrEqual(2);
    expect(source).toContain('flex w-full flex-col gap-2 sm:flex-row');
  });
});

describe('logs line list', () => {
  it('loads lines for eventId with diagnose levels', async () => {
    const view = await render();
    expect(logsApi.list).toHaveBeenCalledWith(expect.objectContaining({ eventId: 'first', level: 'info,warning,error' }));
    expect(view.text()).toContain('first');
    await button('Refresh').trigger('click');
    await flushPromises();
    expect(logsApi.list).toHaveBeenCalledTimes(2);
  });

  it('loads global lines when eventId is missing', async () => {
    const view = await render('/logs?level=warning,error');
    expect(logsApi.list).toHaveBeenCalled();
    const args = vi.mocked(logsApi.list).mock.calls[0]?.[0] as Record<string, unknown>;
    expect(args.level).toBe('warning,error');
    expect(args.eventId).toBeUndefined();
    expect(view.text()).toContain('first');
    expect(view.text()).not.toContain('Choose an event');
  });

  it('resolves operationId to eventId before loading lines', async () => {
    await render('/logs?operationId=op-demo-deploy&level=warning,error');
    await flushPromises();
    expect(eventsApi.list).toHaveBeenCalledWith(expect.objectContaining({ operationId: 'op-demo-deploy' }));
    expect(logsApi.list).toHaveBeenCalledWith(expect.objectContaining({ eventId: 'first' }));
  });

  it('keeps load failures inline with a retry and never raises a toast', async () => {
    vi.mocked(logsApi.list).mockRejectedValue(new Error('logs down'));
    const view = await render();
    expect(view.text()).toContain(useI18n().t('common.loadFailed'));
    expect(view.text()).toContain('logs down');
    expect(toastPush).not.toHaveBeenCalled();
  });
});
