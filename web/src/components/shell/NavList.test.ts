// @vitest-environment jsdom
import { mount } from '@vue/test-utils';
import { createMemoryHistory, createRouter } from 'vue-router';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { useI18n } from '@/i18n';
import NavList from './NavList.vue';

async function mountNav(options: { path?: string; activeKey?: string; collapsed?: boolean } = {}) {
  const { path = '/overview', ...props } = options;
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/:pathMatch(.*)*', component: { template: '<div />' } }],
  });
  await router.push(path);
  await router.isReady();
  return mount(NavList, {
    props: { label: 'Main navigation', ...props },
    global: { plugins: [router] },
  });
}

beforeEach(() => {
  useI18n().setLocale('en');
});

afterEach(() => {
  useI18n().setLocale('zh-CN');
});

describe('NavList', () => {
  it('marks the owning family item as the current page on deep links', async () => {
    // `/certificates/keys` owns no nav item of its own: the family entry must
    // still carry aria-current instead of relying on vue-router's exact match.
    const wrapper = await mountNav({ path: '/certificates/keys', activeKey: 'certificates' });

    expect(wrapper.get('a[href="/certificates/domains"]').attributes('aria-current')).toBe('page');
    expect(wrapper.get('a[href="/overview"]').attributes('aria-current')).toBeUndefined();
  });

  it('paints the WinUI NavigationView selected state: neutral fill, accent pill, foreground text', async () => {
    const wrapper = await mountNav({ path: '/overview', activeKey: 'servers' });
    const active = wrapper.get('a[href="/servers"]');

    expect(active.attributes('aria-busy')).toBeUndefined();
    expect(active.attributes('aria-current')).toBe('page');
    // WinUI selected item: ControlFillColorSecondary family fill, primary text.
    expect(active.classes()).toContain('bg-control');
    expect(active.classes()).toContain('text-foreground');
    // Accent never tints the label/fill of a nav item; it is the 3x16 pill only.
    expect(active.classes()).not.toContain('bg-accent-selected');
    expect(active.classes()).not.toContain('text-accent-text');
    expect(active.classes()).not.toContain('font-semibold');
    const indicator = active.get('.nav-indicator');
    expect(indicator.classes()).toContain('bg-primary');
    expect(indicator.classes()).toContain('h-4');
    expect(indicator.classes()).toContain('w-indicator');
    expect(wrapper.get('a[href="/overview"]').classes()).not.toContain('bg-control');
    expect(wrapper.get('a[href="/overview"]').find('.nav-indicator').exists()).toBe(false);
  });

  it('uses the WinUI 32px row height with fill-only hover/press feedback', async () => {
    const wrapper = await mountNav({ activeKey: 'servers' });
    const inactive = wrapper.get('a[href="/overview"]');

    expect(inactive.classes()).toContain('h-8');
    expect(inactive.classes()).toContain('rounded-control');
    // Narrow screens enlarge nav rows to a touch-friendly height.
    expect(inactive.classes()).toContain('max-lg:h-11');
    expect(inactive.classes()).toContain('hover:bg-hover');
    expect(inactive.classes()).toContain('active:bg-pressed');
    // No hover lift / press scale: WinUI nav feedback is fill-only.
    expect(inactive.classes().join(' ')).not.toMatch(/hover:-translate|active:scale/);
  });

  it('uses WinUI NavigationView compact mode: 40x40 targets, sr-only labels and the same indicator', async () => {
    const wrapper = await mountNav({ activeKey: 'servers', collapsed: true });
    const link = wrapper.get('a[href="/servers"]');

    // Compact pane: 40x40 icon-only targets centered in the 48px pane.
    expect(link.classes()).toContain('w-10');
    expect(link.classes()).toContain('h-10');
    expect(link.classes()).toContain('justify-center');
    // Labels stay in the accessibility tree without the old clipped fade.
    expect(link.find('.sr-only').text()).toBe('Servers');
    // Compact keeps the same neutral selected fill + accent indicator.
    expect(link.classes()).toContain('bg-control');
    expect(link.classes()).toContain('text-foreground');
    expect(link.find('.nav-indicator').exists()).toBe(true);
    // Collapsed groups are separated by 1px dividers instead of captions.
    expect(wrapper.findAll('div.bg-divider').length).toBeGreaterThan(0);
  });

  it('emits navigate so the mobile drawer can close on selection', async () => {
    const wrapper = await mountNav({ activeKey: 'overview' });
    await wrapper.get('a[href="/servers"]').trigger('click');

    expect(wrapper.emitted('navigate')).toHaveLength(1);
  });

  it('keeps mobile rows at the 44px touch height with truncating labels and no horizontal scroll', async () => {
    const wrapper = await mountNav({ activeKey: 'servers' });
    const nav = wrapper.get('nav');
    expect(nav.classes()).toContain('overflow-y-auto');
    expect(nav.classes()).toContain('overflow-x-hidden');

    const link = wrapper.get('a[href="/servers"]');
    expect(link.classes()).toContain('max-lg:h-11');
    expect(link.classes()).toContain('min-w-0');
    const label = link.findAll('span').at(-1)!;
    expect(label.classes()).toContain('min-w-0');
    expect(label.classes()).toContain('flex-1');
    expect(label.classes()).toContain('overflow-hidden');
    expect(label.classes()).toContain('text-ellipsis');
    expect(label.classes()).toContain('whitespace-nowrap');
  });
});
