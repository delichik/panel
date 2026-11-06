import { createMemoryHistory, createRouter } from 'vue-router';
import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  beginRouteNavigation,
  finishRouteNavigation,
  installRouteNavigationFeedback,
  routeNavigation,
} from './navigationState';

afterEach(() => finishRouteNavigation());

describe('route navigation feedback', () => {
  it('stays visible while an async page resolves and clears after success', async () => {
    let resolvePage!: (component: { template: string }) => void;
    const page = new Promise<{ template: string }>((resolve) => { resolvePage = resolve; });
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/', component: { template: '<div />' } },
        { path: '/slow', component: () => page, meta: { titleKey: 'routes.servers.title' } },
      ],
    });
    installRouteNavigationFeedback(router);
    await router.push('/');

    const navigation = router.push('/slow');
    await vi.waitFor(() => expect(routeNavigation.pending.value).toBe(true));
    expect(routeNavigation.targetPath.value).toBe('/slow');
    expect(routeNavigation.titleKey.value).toBe('routes.servers.title');

    resolvePage({ template: '<div />' });
    await navigation;
    expect(routeNavigation.pending.value).toBe(false);
  });

  it('clears feedback when a guard cancels navigation', async () => {
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/', component: { template: '<div />' } },
        { path: '/blocked', component: { template: '<div />' } },
      ],
    });
    installRouteNavigationFeedback(router);
    router.beforeEach((to) => to.path === '/blocked' ? false : true);
    await router.push('/');

    await router.push('/blocked');
    expect(routeNavigation.pending.value).toBe(false);
  });

  it('does not let an older completion clear a newer attempt', () => {
    const older = beginRouteNavigation('/servers', 'routes.servers.title');
    const newer = beginRouteNavigation('/credentials', 'routes.credentials.title');

    finishRouteNavigation(older);
    expect(routeNavigation.pending.value).toBe(true);
    expect(routeNavigation.targetPath.value).toBe('/credentials');

    finishRouteNavigation(newer);
    expect(routeNavigation.pending.value).toBe(false);
  });
});
