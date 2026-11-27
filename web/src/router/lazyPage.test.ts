// @vitest-environment jsdom
import { createMemoryHistory, createRouter } from 'vue-router';
import { describe, expect, it, vi } from 'vitest';
import { lazyPage, prefetchPage } from './lazyPage';

describe('lazyPage', () => {
  it('lets vue-router finish before the page chunk resolves', async () => {
    let resolvePage!: (component: { default: { template: string } }) => void;
    const page = new Promise<{ default: { template: string } }>((resolve) => {
      resolvePage = resolve;
    });
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/', component: { template: '<div />' } },
        { path: '/slow', component: lazyPage(() => page), meta: { titleKey: 'routes.servers.title' } },
      ],
    });
    await router.push('/');

    // With () => import(), this await would hang until resolvePage.
    // lazyPage must not block navigation on the chunk.
    await router.push('/slow');
    expect(router.currentRoute.value.path).toBe('/slow');
    expect(warn.mock.calls.some((args) => String(args[0]).includes('defineAsyncComponent()'))).toBe(false);

    resolvePage({ default: { template: '<div />' } });
    warn.mockRestore();
  });

  it('warms the shared loader without awaiting it', async () => {
    const loader = vi.fn(async () => ({ default: { template: '<div />' } }));
    prefetchPage(loader);
    expect(loader).toHaveBeenCalledTimes(1);
    await expect(loader.mock.results[0]?.value).resolves.toBeTruthy();
  });
});
