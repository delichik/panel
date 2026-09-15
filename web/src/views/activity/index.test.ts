// @vitest-environment jsdom
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils';
import { createMemoryHistory, createRouter, type Router } from 'vue-router';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import ActivityPage from './index.vue';
import { activityApi } from '@/api/activity';
import { toastKey } from '@/components/ui/toast';
import { useI18n } from '@/i18n';
import type { ActivityEvent, ActivityOperationDetail, ActivityPage as Page } from '@/types/activity';

const event = (id: string): ActivityEvent => ({ eventId: id, seq: 1, eventVersion: 1, eventType: 'execution.finished', kind: 'lifecycle', domain: 'application', level: 'info', text: id, resources: [], occurredAt: '2026-09-15T00:00:00Z', recordedAt: '2026-09-15T00:00:00Z' });
const page = (id = 'first'): Page<ActivityEvent> => ({ items: [event(id)], snapshotSeq: 10, headSeq: 10, projectedThroughSeq: 10, indexState: 'ready', hasMore: true, nextCursor: 'older' });
const operation: ActivityOperationDetail = { operation: { operationId: 'op-1', domain: 'application', action: 'apply', title: 'Deployment', phase: 'finished', result: 'succeeded', attention: false, createdAt: '', updatedAt: '', firstSeq: 1, lastSeq: 10, eventCount: 10, attemptCount: 1, evidenceComplete: true, resources: [] }, events: [event('detail-first')], executions: [], steps: [], relatedOperations: [], availableCommands: [], snapshotSeq: 10, headSeq: 10, hasMore: true, nextCursor: 'detail-older' };
let wrapper: VueWrapper | undefined;
let router: Router;

async function render(url = '/activity?from=2026-09-14T00:00:00Z') {
  router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/activity', component: ActivityPage }] });
  await router.push(url);
  await router.isReady();
  wrapper = mount(ActivityPage, { global: {
    plugins: [router], provide: { [toastKey as symbol]: { push: vi.fn() } },
    stubs: {
      PageHeader: { template: '<header><slot name="actions" /></header>' },
      MasterDetailLayout: { template: '<main><slot name="master" /><slot name="detail" /></main>' },
      Tabs: { template: '<div><slot /></div>' },
      DateTimeRangePicker: true, Select: true, SearchInput: true,
      ConfirmDialog: true, Dialog: true,
      EventTimeline: { name: 'EventTimeline', props: ['events'], emits: ['context'], template: '<div><span v-for="event in events" :key="event.eventId">{{ event.text }}</span></div>' },
    },
  } });
  await flushPromises();
  return wrapper;
}
function button(text: string) { return wrapper!.findAll('button').find(item => item.text() === text)!; }

beforeEach(() => {
  vi.useFakeTimers();
  localStorage.setItem('panel.autoRefresh', '5');
  useI18n().setLocale('en');
  vi.spyOn(activityApi, 'events').mockResolvedValue(page());
  vi.spyOn(activityApi, 'operations').mockResolvedValue({ ...page(), items: [] });
  vi.spyOn(activityApi, 'operation').mockResolvedValue(operation);
  vi.spyOn(activityApi, 'tail').mockRejectedValue(new Error('Unexpected polling'));
  vi.spyOn(activityApi, 'summary').mockRejectedValue(new Error('Unexpected aggregation'));
});
afterEach(() => { wrapper?.unmount(); wrapper = undefined; vi.useRealTimers(); vi.restoreAllMocks(); });

// UI-ACT-002/003: no timers, new-data probing or aggregate requests while idle.
describe('activity manual loading', () => {
  it('loads once with a default range and stays idle until manual refresh', async () => {
    const view = await render('/activity');
    expect(activityApi.events).toHaveBeenCalledTimes(1);
    expect(router.currentRoute.value.query.from).toBeTruthy();
    await vi.advanceTimersByTimeAsync(60_000);
    expect(activityApi.events).toHaveBeenCalledTimes(1);
    expect(activityApi.tail).not.toHaveBeenCalled();
    expect(activityApi.summary).not.toHaveBeenCalled();
    expect(view.text()).not.toMatch(/events ·|Snapshot #|New events|Every 5|Every 10/);
    await button('Refresh').trigger('click');
    await flushPromises();
    expect(activityApi.events).toHaveBeenCalledTimes(2);
    expect(activityApi.summary).not.toHaveBeenCalled();
  });

  it('keeps cursor pagination and manual detail refresh without probing selected operations', async () => {
    await render('/activity?from=2026-09-14T00:00:00Z&operationId=op-1');
    expect(activityApi.operation).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(60_000);
    expect(activityApi.tail).not.toHaveBeenCalled();
    expect(activityApi.summary).not.toHaveBeenCalled();
    expect(activityApi.operation).toHaveBeenCalledTimes(1);
    await button('Next').trigger('click');
    await flushPromises();
    expect(activityApi.events).toHaveBeenLastCalledWith(expect.objectContaining({ cursor: 'older', snapshotSeq: '10' }));
    await button('Refresh records').trigger('click');
    await flushPromises();
    expect(activityApi.operation).toHaveBeenCalledTimes(2);
  });

  it('does not let an old filter response replace the current result', async () => {
    let finish!: (value: Page<ActivityEvent>) => void;
    vi.mocked(activityApi.events).mockImplementationOnce(() => new Promise(resolve => { finish = resolve; }));
    await render();
    await router.replace({ query: { from: '2026-09-14T00:00:00Z', q: 'latest' } });
    await flushPromises();
    finish(page('stale-result'));
    await flushPromises();
    expect(wrapper!.text()).not.toContain('stale-result');
    expect(wrapper!.text()).toContain('first');
    expect(activityApi.summary).not.toHaveBeenCalled();
  });

  it('discards pending context when the user moves to another timeline batch', async () => {
    let finish!: (value: Page<ActivityEvent>) => void;
    vi.spyOn(activityApi, 'context').mockImplementation(() => new Promise(resolve => { finish = resolve; }));
    await render('/activity?from=2026-09-14T00:00:00Z&operationId=op-1');
    wrapper!.findComponent({ name: 'EventTimeline' }).vm.$emit('context', event('detail-first'));
    await flushPromises();
    vi.mocked(activityApi.events).mockResolvedValueOnce(page('older-result'));
    await button('Read older batch').trigger('click');
    await flushPromises();
    finish(page('stale-context'));
    await flushPromises();
    expect(wrapper!.text()).toContain('older-result');
    expect(wrapper!.text()).not.toContain('stale-context');
  });
});
