// @vitest-environment jsdom
import { mount } from '@vue/test-utils';
import { createMemoryHistory, createRouter, type Router } from 'vue-router';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { useI18n } from '@/i18n';
import PageHeader from './PageHeader.vue';

function makeRouter(): Router {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { template: '<div />' } },
      { path: '/servers', component: { template: '<div />' } },
    ],
  });
}

function mountHeader(options: { router?: Router; props?: Record<string, unknown>; slots?: Record<string, string> } = {}) {
  const router = options.router ?? makeRouter();
  return mount(PageHeader, {
    props: { title: 'Servers', ...options.props },
    slots: options.slots,
    global: { plugins: [router] },
  });
}

beforeEach(() => {
  useI18n().setLocale('en');
});

afterEach(() => {
  useI18n().setLocale('zh-CN');
});

describe('PageHeader breadcrumbs', () => {
  it('renders the shared BreadcrumbBar above the title with default accessible name', async () => {
    const wrapper = mountHeader({
      router: undefined,
      props: {
        breadcrumbs: [
          { label: 'Assets', to: '/' },
          { label: 'Certificates', to: '/servers' },
          { label: 'example.com' },
        ],
      },
    });
    await wrapper.vm.$nextTick();

    const nav = wrapper.get('nav[aria-label="Breadcrumb"]');
    expect(nav.text()).toContain('Assets');
    expect(nav.text()).toContain('example.com');
    // Ancestor entries are links; the last item is the current page.
    expect(wrapper.get('nav a[href="/"]').text()).toBe('Assets');
    const current = nav.findAll('span').at(-1)!;
    expect(current.text()).toBe('example.com');
    expect(current.attributes('aria-current')).toBe('page');
    // Breadcrumb lives in the structural row above the h1, not beside it.
    const title = wrapper.get('header h1');
    expect((title.element.previousElementSibling as HTMLElement).contains(nav.element)).toBe(true);
  });

  it('uses the provided breadcrumbLabel for the nav accessible name', () => {
    const wrapper = mountHeader({
      props: {
        breadcrumbLabel: 'You are here',
        breadcrumbs: [{ label: 'Assets' }, { label: 'Servers' }],
      },
    });

    expect(wrapper.get('nav').attributes('aria-label')).toBe('You are here');
  });

  it('lets the #breadcrumb slot override the breadcrumb prop', () => {
    const wrapper = mountHeader({
      props: { breadcrumbs: [{ label: 'Assets' }, { label: 'Servers' }] },
      slots: { breadcrumb: '<div data-custom-trail>Custom trail</div>' },
    });

    expect(wrapper.get('[data-custom-trail]').text()).toBe('Custom trail');
    expect(wrapper.find('nav[aria-label="Breadcrumb"]').exists()).toBe(false);
  });

  it('places back and breadcrumb in the same row above the title and keeps the h1 left edge', async () => {
    const wrapper = mountHeader({
      props: {
        title: 'Server detail',
        backLabel: 'Back to list',
        breadcrumbs: [{ label: 'Servers', to: '/' }, { label: 'web-01' }],
      },
    });
    await wrapper.vm.$nextTick();

    const back = wrapper.get('header button[aria-label="Back to list"]');
    const nav = wrapper.get('nav[aria-label="Breadcrumb"]');
    const row = back.element.parentElement as HTMLElement;
    expect(row.contains(nav.element)).toBe(true);
    expect(row.className).toContain('mb-2');
    const title = wrapper.get('header h1');
    expect(title.element.previousElementSibling).toBe(row);
    expect(row.contains(title.element)).toBe(false);

    await back.trigger('click');
    expect(wrapper.emitted('back')).toHaveLength(1);
  });

  it('renders no breadcrumb region when neither prop nor slot is provided', () => {
    const wrapper = mountHeader();

    expect(wrapper.find('nav').exists()).toBe(false);
    expect(wrapper.get('header h1').element.previousElementSibling).toBeNull();
  });
});

describe('PageHeader mobile layout (360–430px)', () => {
  it('balances/wraps the title and description without leaving the type ramp', () => {
    const wrapper = mountHeader({
      props: {
        title: 'A very long server name that would otherwise overflow the phone viewport',
        description: 'A long description that must wrap inside the body column instead of forcing a horizontal scrollbar.',
      },
    });

    const title = wrapper.get('header h1');
    expect(title.classes()).toContain('text-title');
    expect(title.classes()).toContain('text-balance');
    expect(title.classes()).toContain('break-words');
    const description = wrapper.get('header p');
    expect(description.classes()).toContain('text-body');
    expect(description.classes()).toContain('break-words');
  });

  it('keeps back + trail in a wrapping row with a truncating breadcrumb column', () => {
    const wrapper = mountHeader({
      props: {
        title: 'web-01',
        backLabel: 'Back to list',
        breadcrumbs: [
          { label: 'A very long assets breadcrumb label that must truncate', to: '/' },
          { label: 'web-01' },
        ],
      },
    });

    const back = wrapper.get('header button[aria-label="Back to list"]');
    const row = back.element.parentElement as HTMLElement;
    expect(row.className).toContain('flex-wrap');
    expect(row.className).toContain('max-sm:gap-1.5');
    const nav = wrapper.get('nav[aria-label="Breadcrumb"]');
    expect(nav.classes()).toContain('min-w-0');
    expect(nav.classes()).toContain('flex-1');
    // The breadcrumb items themselves truncate (ui/BreadcrumbBar contract).
    expect(nav.classes()).toContain('flex-wrap');
  });

  it('stacks actions full width below sm so buttons cannot be compressed', () => {
    const wrapper = mountHeader({
      props: { title: 'Server detail' },
      slots: { actions: '<button type="button">Refresh</button><button type="button">Restart</button><button type="button">Delete</button>' },
    });

    const actions = wrapper.get('header > div:last-child');
    // Existing desktop/tablet contract stays intact.
    expect(actions.classes()).toContain('flex-wrap');
    expect(actions.classes()).toContain('max-lg:w-full');
    // Phone: vertical column, every slotted control full width.
    expect(actions.classes()).toContain('max-sm:flex-col');
    expect(actions.classes()).toContain('max-sm:items-stretch');
    expect(actions.classes()).toContain('max-sm:gap-2');
    expect(actions.classes()).toContain('max-sm:[&>*]:w-full');
    expect(wrapper.findAll('header > div:last-child > button')).toHaveLength(3);
  });
});
