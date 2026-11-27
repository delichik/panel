// @vitest-environment jsdom
import { flushPromises, mount } from '@vue/test-utils';
import { createMemoryHistory, createRouter, type Router } from 'vue-router';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { useI18n } from '@/i18n';
import RoutePageFallback from './RoutePageFallback.vue';

async function makeRouter(path = '/'): Promise<Router> {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { template: '<div />' } },
      { path: '/settings/:section', component: { template: '<div />' }, meta: { pageKey: 'settings' } },
    ],
  });
  await router.push(path);
  await router.isReady();
  return router;
}

async function mountFallback(path = '/') {
  const wrapper = mount(RoutePageFallback, { global: { plugins: [await makeRouter(path)] } });
  await flushPromises();
  return wrapper;
}

beforeEach(() => {
  useI18n().setLocale('en');
});

afterEach(() => {
  useI18n().setLocale('zh-CN');
});

describe('RoutePageFallback mobile geometry', () => {
  it('keeps the same content column and page padding as ConsolePage', async () => {
    const wrapper = await mountFallback('/');

    expect(wrapper.classes()).toContain('max-w-content');
    expect(wrapper.classes()).toContain('mx-auto');
    expect(wrapper.classes()).toContain('p-4');
    expect(wrapper.classes()).toContain('max-lg:p-3');
    expect(wrapper.classes()).toContain('max-sm:gap-3');
    expect(wrapper.attributes('aria-busy')).toBe('true');
    expect(wrapper.attributes('aria-label')).toBe('Loading…');
  });

  it('uses the narrow column when the lazy target is a settings page', async () => {
    const wrapper = await mountFallback('/settings/general');

    expect(wrapper.classes()).toContain('max-w-content-narrow');
    expect(wrapper.classes()).not.toContain('max-w-content');
  });

  it('lets the skeleton lines shrink on phones instead of overflowing', async () => {
    const wrapper = await mountFallback('/');

    // The description line carries the phone full-width fallback.
    expect(wrapper.element.innerHTML).toContain('max-sm:w-full');
  });

  it('renders the real route title and description immediately when route meta provides titleKey', async () => {
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/servers', component: { template: '<div />' }, meta: { titleKey: 'routes.servers.title' } },
      ],
    });
    await router.push('/servers');
    await router.isReady();
    const wrapper = mount(RoutePageFallback, { global: { plugins: [router] } });
    await flushPromises();

    expect(wrapper.get('h1').text()).toBe(useI18n().t('routes.servers.title'));
    expect(wrapper.text()).toContain(useI18n().t('routes.servers.description'));
  });
});
