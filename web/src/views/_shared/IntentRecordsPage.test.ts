// @vitest-environment jsdom
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils';
import { createMemoryHistory, createRouter, type Router } from 'vue-router';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import IntentRecordsPage from './IntentRecordsPage.vue';
import { activityApi } from '@/api/activity';
import { toastKey } from '@/components/ui/toast';
import { useI18n } from '@/i18n';
import type { ActivityOperation, ActivityOperationDetail, ActivityPage as Page } from '@/types/activity';

const operation = (id: string): ActivityOperation => ({
  operationId: id,
  domain: 'application',
  title: id,
  action: 'deploy',
  phase: 'finished',
  result: 'failed',
  failureSummary: 'boom',
  attemptCount: 1,
  hadError: true,
  uncertainty: false,
  evidenceComplete: true,
  attention: true,
  createdAt: '2026-09-15T00:00:00Z',
  updatedAt: '2026-09-15T00:00:00Z',
  firstSeq: 1,
  lastSeq: 1,
  eventCount: 1,
  resources: [{ resourceType: 'application', resourceId: 'app-1', nameSnapshot: 'App' }],
  actor: { kind: 'user', id: 'admin', name: 'admin' },
});

const detail = (id: string): ActivityOperationDetail => ({
  operation: operation(id),
  events: [],
  executions: [],
  steps: [],
  relatedOperations: [],
  availableCommands: [],
  snapshotSeq: 10,
  headSeq: 10,
  hasMore: false,
});

const page = (id = 'op-1'): Page<ActivityOperation> => ({
  items: [operation(id)],
  snapshotSeq: 10,
  headSeq: 10,
  projectedThroughSeq: 10,
  indexState: 'ready',
  hasMore: false,
  nextCursor: '',
});

let wrapper: VueWrapper | undefined;
let router: Router;

async function render(origin: 'user' | 'system', url: string) {
  router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/operations', component: IntentRecordsPage },
      { path: '/tasks', component: IntentRecordsPage },
      { path: '/logs', component: { template: '<div />' } },
    ],
  });
  await router.push(url);
  await router.isReady();
  wrapper = mount(IntentRecordsPage, {
    props: {
      origin,
      titleKey: origin === 'user' ? 'routes.operations.title' : 'routes.tasks.title',
      descriptionKey: origin === 'user' ? 'routes.operations.description' : 'routes.tasks.description',
      emptyKey: origin === 'user' ? 'operationsPage.empty' : 'systemTasksPage.empty',
      emptyHintKey: origin === 'user' ? 'operationsPage.emptyHint' : 'systemTasksPage.emptyHint',
    },
    global: {
      plugins: [router],
      provide: { [toastKey as symbol]: { push: vi.fn() } },
      stubs: {
        ConsolePage: {
          props: ['title', 'backLabel'],
          emits: ['back'],
          template: '<section><header><slot name="actions" /></header><h1>{{ title }}</h1><slot /></section>',
        },
        ListPage: { template: '<div><slot name="toolbar" /><slot /><slot name="pagination" /></div>' },
        TableDetailLayout: {
          props: ['hasDetail'],
          template: '<div><div data-testid="list"><slot name="list" /></div><div v-if="hasDetail" data-testid="detail"><slot name="detail" /></div></div>',
        },
        Table: {
          props: ['rows'],
          template: '<div><div v-for="row in rows" :key="row.operationId">{{ row.title }}</div><slot name="actions" :row="rows[0]" /><slot name="empty" /></div>',
        },
        DateTimeRangePicker: true,
        Select: true,
        SearchInput: true,
        Dialog: true,
        ConfirmDialog: true,
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
  useI18n().setLocale('en');
  vi.spyOn(activityApi, 'operations').mockResolvedValue(page());
  vi.spyOn(activityApi, 'operation').mockResolvedValue(detail('op-1'));
  vi.spyOn(activityApi, 'events').mockRejectedValue(new Error('Unexpected events'));
  vi.spyOn(activityApi, 'tail').mockRejectedValue(new Error('Unexpected polling'));
  vi.spyOn(activityApi, 'summary').mockRejectedValue(new Error('Unexpected aggregation'));
});
afterEach(() => {
  wrapper?.unmount();
  wrapper = undefined;
  vi.restoreAllMocks();
});

describe('IntentRecordsPage diagnose contract', () => {
  it('loads user operations with origin=user and opens a light detail', async () => {
    await render('user', '/operations?from=2026-09-14T00:00:00Z&operationId=op-1');
    expect(activityApi.operations).toHaveBeenCalledWith(expect.objectContaining({ origin: 'user' }));
    expect(activityApi.operation).toHaveBeenCalledWith('op-1', expect.anything());
    const [calledId, calledParams] = vi.mocked(activityApi.operation).mock.calls[0]!;
    expect(calledId).toBe('op-1');
    // activityApi.operation always merges detail=summary for product UI.
    expect(JSON.stringify(calledParams ?? {})).not.toContain('"full"');
    expect(wrapper!.find('[data-testid="detail"]').exists()).toBe(true);
    expect(wrapper!.text()).toContain('boom');
    expect(wrapper!.text()).toContain('View logs');
    expect(wrapper!.text()).not.toMatch(/Executions|Steps|Evidence|View context|Timeline/);
    expect(activityApi.events).not.toHaveBeenCalled();
    expect(activityApi.tail).not.toHaveBeenCalled();
    expect(activityApi.summary).not.toHaveBeenCalled();
  });

  it('loads system tasks with origin=system', async () => {
    await render('system', '/tasks?from=2026-09-14T00:00:00Z');
    expect(activityApi.operations).toHaveBeenCalledWith(expect.objectContaining({ origin: 'system' }));
  });

  it('jumps to diagnostic logs with warning,error for the selected intent', async () => {
    await render('user', '/operations?from=2026-09-14T00:00:00Z&operationId=op-1');
    await button('View logs').trigger('click');
    await flushPromises();
    expect(router.currentRoute.value.path).toBe('/logs');
    expect(router.currentRoute.value.query).toMatchObject({
      operationId: 'op-1',
      level: 'warning,error',
    });
  });
});
