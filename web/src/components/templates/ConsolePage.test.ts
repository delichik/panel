// @vitest-environment jsdom
import { mount } from '@vue/test-utils';
import { createMemoryHistory, createRouter } from 'vue-router';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { useI18n } from '@/i18n';
import ConsolePage from './ConsolePage.vue';

function makeRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { template: '<div />' } },
      { path: '/servers', component: { template: '<div />' } },
    ],
  });
}

beforeEach(() => {
  useI18n().setLocale('en');
});

afterEach(() => {
  useI18n().setLocale('zh-CN');
});

describe('ConsolePage', () => {
  it('scrolls its body internally by default', () => {
    const wrapper = mount(ConsolePage, {
      props: { title: 'Servers' },
      slots: { default: '<div id="body">body</div>' },
    });

    const body = wrapper.element.children[1] as HTMLElement;
    expect(body.className).toContain('overflow-y-auto');
    expect(body.className).toContain('overflow-x-hidden');
    expect(body.className).toContain('max-lg:overflow-visible');
  });

  it('hands region scrolling to the page when body-scroll is false', () => {
    const wrapper = mount(ConsolePage, {
      props: { title: 'Servers', bodyScroll: false },
    });

    const body = wrapper.element.children[1] as HTMLElement;
    expect(body.className).toContain('overflow-hidden');
    expect(body.className).not.toContain('overflow-y-auto');
    // Narrow screens still restore page-level scrolling.
    expect(body.className).toContain('max-lg:overflow-visible');
  });

  it('defaults to the WinUI data/list content column', () => {
    const wrapper = mount(ConsolePage, { props: { title: 'Servers' } });

    expect(wrapper.classes()).toContain('max-w-content');
    expect(wrapper.classes()).not.toContain('max-w-content-narrow');
    expect(wrapper.attributes('data-width')).toBe('content');
  });

  it('switches to the narrow content column for settings/form pages', () => {
    const wrapper = mount(ConsolePage, { props: { title: 'Settings', width: 'narrow' } });

    expect(wrapper.classes()).toContain('max-w-content-narrow');
    expect(wrapper.classes()).not.toContain('max-w-content');
    expect(wrapper.attributes('data-width')).toBe('narrow');
    // Header and body live in the same (root) content column.
    const header = wrapper.get('header');
    const body = wrapper.element.children[1] as HTMLElement;
    expect(wrapper.element.contains(header.element)).toBe(true);
    expect(wrapper.element.contains(body)).toBe(true);
  });

  it('owns the single page padding; header and body stay padding-free', () => {
    const wrapper = mount(ConsolePage, {
      props: { title: 'Servers' },
      slots: { default: '<div data-body>body</div>' },
    });

    // Desktop 16px, narrow (<1024) 12px — both owned by ConsolePage.
    expect(wrapper.classes()).toContain('p-4');
    expect(wrapper.classes()).toContain('max-lg:p-3');
    // PageHeader is a structural block and the body adds no second p-* layer.
    expect(wrapper.get('header').classes()).not.toContain('px-4');
    expect(wrapper.get('header').classes()).not.toContain('pt-4');
    expect(wrapper.get('header').classes()).not.toContain('pb-3');
    const body = wrapper.element.children[1] as HTMLElement;
    expect(body.className).not.toContain('p-4');
  });

  it('keeps the back action behavior and emits back', async () => {
    const wrapper = mount(ConsolePage, {
      props: { title: 'Server detail', backLabel: 'Back to list' },
    });

    await wrapper.get('header button[aria-label="Back to list"]').trigger('click');
    expect(wrapper.emitted('back')).toHaveLength(1);
  });

  it('passes breadcrumbs through to the shared BreadcrumbBar above the title', () => {
    const router = makeRouter();
    const wrapper = mount(ConsolePage, {
      props: {
        title: 'Servers',
        breadcrumbs: [
          { label: 'Assets', to: '/' },
          { label: 'Servers' },
        ],
        breadcrumbLabel: 'Trail',
      },
      global: { plugins: [router] },
    });

    const nav = wrapper.get('nav[aria-label="Trail"]');
    expect(nav.text()).toContain('Assets');
    expect(nav.findAll('span').at(-1)!.attributes('aria-current')).toBe('page');
    // Header and body stay in one content column; the breadcrumb is above the h1.
    expect(wrapper.get('header h1').element.previousElementSibling?.contains(nav.element)).toBe(true);
  });

  it('keeps the #breadcrumb slot as an override for custom trails', () => {
    const router = makeRouter();
    const wrapper = mount(ConsolePage, {
      props: {
        title: 'Servers',
        breadcrumbs: [{ label: 'Assets' }, { label: 'Servers' }],
      },
      slots: { breadcrumb: '<div data-custom-trail>Custom trail</div>' },
      global: { plugins: [router] },
    });

    expect(wrapper.get('[data-custom-trail]').text()).toBe('Custom trail');
    expect(wrapper.find('nav[aria-label="Breadcrumb"]').exists()).toBe(false);
  });
});
