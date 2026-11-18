// @vitest-environment jsdom
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils';
import { createMemoryHistory, createRouter, type Router } from 'vue-router';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import IntentRecordsPage from './IntentRecordsPage.vue';
import { eventsApi } from '@/api/events';
import { toastKey } from '@/components/ui/toast';
import { useI18n } from '@/i18n';
import type { Event, EventPage as Page } from '@/types/events';

const event = (id: string, extra: Partial<Event> = {}): Event => ({
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
  ...extra,
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
        Dialog: {
          props: ['open', 'title'],
          template: '<div v-if="open" role="dialog"><h2>{{ title }}</h2><slot /><footer><slot name="footer" /></footer></div>',
        },
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

describe('Events narrow viewport contract', () => {
  const source = readFileSync(resolve(__dirname, './IntentRecordsPage.vue'), 'utf8');
  const columnsSource = source.slice(source.indexOf('const columns = computed'), source.indexOf('const detailProperties'));

  it('hides secondary columns below wide and keeps time/level, title, actions', () => {
    expect(columnsSource).toMatch(/key: 'module'[^\n]*hideBelow: 'wide'/);
    expect(columnsSource).toMatch(/key: 'state'[^\n]*hideBelow: 'wide'/);
    // 主列不隐藏：时间+级别、标题、操作。
    expect(columnsSource).not.toMatch(/key: 'updatedAt'[^\n]*hideBelow/);
    expect(columnsSource).not.toMatch(/key: 'title'[^\n]*hideBelow/);
    expect(columnsSource).not.toMatch(/key: 'actions'[^\n]*hideBelow/);
    // 标题列占剩余宽度（不固定宽度），且旧 level 独立列已移除。
    expect(columnsSource).not.toMatch(/key: 'title'[^\n]*width/);
    expect(columnsSource).not.toContain("key: 'level'");
  });

  it('folds the secondary row action into an overflow menu below wide', () => {
    expect(source).toContain('max-wide:hidden');
    expect(source).toContain('wide:hidden');
    expect(source).toContain('common.moreActions');
    expect(source).toMatch(/<Dropdown[\s\S]*?DropdownItem[\s\S]*?eventsPage\.viewLogs/);
  });
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

  it('shows paired coordination and execution log deep links', async () => {
    const paired = event('intent-1', {
      title: 'Deploy app → server',
      summary: 'Entered monitor (dispatch_wait)',
      operationId: 'intent-1',
      executionId: 'exec-9',
    });
    vi.spyOn(eventsApi, 'list').mockResolvedValue({ items: [paired], hasMore: false });
    vi.spyOn(eventsApi, 'get').mockResolvedValue(paired);
    await render('/events?from=2026-09-14T00:00:00Z&eventId=intent-1');
    expect(wrapper!.text()).toContain('Entered monitor (dispatch_wait)');
    await button('Execution evidence logs').trigger('click');
    await flushPromises();
    expect(router.currentRoute.value.path).toBe('/logs');
    expect(router.currentRoute.value.query.eventId).toBe('exec-9');
    expect(router.currentRoute.value.query.operationId).toBe('intent-1');
    expect(router.currentRoute.value.query.executionId).toBe('exec-9');
  });
});
