// @vitest-environment jsdom
import { flushPromises, shallowMount, type VueWrapper } from '@vue/test-utils';
import { createPinia } from 'pinia';
import { createMemoryHistory, createRouter } from 'vue-router';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { activityApi } from '@/api/activity';
import { toastKey } from '@/components/ui/toast';
import ActivityPage from './index.vue';

let wrapper: VueWrapper | undefined;

afterEach(() => {
  wrapper?.unmount();
  wrapper = undefined;
  vi.restoreAllMocks();
});

describe('ActivityPage initial load', () => {
  it('loads the event list immediately when the default range is added to the URL', async () => {
    const events = vi.spyOn(activityApi, 'events').mockResolvedValue({
      items: [],
      nextCursor: '',
      hasMore: false,
      snapshotSeq: 7,
      headSeq: 7,
      projectedThroughSeq: 7,
      indexState: 'ready',
    });
    const summary = vi.spyOn(activityApi, 'summary').mockResolvedValue({
      total: 0,
      byLevel: {},
      byDomain: {},
      snapshotSeq: 7,
      headSeq: 7,
      projectedThroughSeq: 7,
    });
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: '/activity', component: ActivityPage }],
    });
    await router.push('/activity');
    await router.isReady();

    wrapper = shallowMount(ActivityPage, {
      global: {
        plugins: [createPinia(), router],
        provide: { [toastKey as symbol]: { push: vi.fn(), remove: vi.fn() } },
      },
    });
    await flushPromises();

    expect(events).toHaveBeenCalledOnce();
    const params = events.mock.calls[0]?.[0];
    expect(params).toEqual(expect.objectContaining({ limit: 100, level: 'info,warning,error', from: expect.any(String) }));
    expect(summary).toHaveBeenCalledOnce();
    expect(summary).toHaveBeenCalledWith(expect.objectContaining({ from: params?.from, snapshotSeq: 7 }));
    expect(router.currentRoute.value.query.from).toBe(params?.from);
  });
});
