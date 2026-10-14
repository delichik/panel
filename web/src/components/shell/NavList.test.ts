// @vitest-environment jsdom
import { mount } from '@vue/test-utils';
import { createMemoryHistory, createRouter } from 'vue-router';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { useI18n } from '@/i18n';
import NavList from './NavList.vue';

async function mountNav(options: { path?: string; activeKey?: string; pendingKey?: string; collapsed?: boolean } = {}) {
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

  it('reports a pending route on the target item instead of the current one', async () => {
    // AppShell passes the pending key as the displayed active key so the target
    // reads as current while its chunk is still loading.
    const wrapper = await mountNav({ path: '/overview', activeKey: 'servers', pendingKey: 'servers' });
    const pending = wrapper.get('a[href="/servers"]');

    expect(pending.attributes('aria-busy')).toBe('true');
    expect(pending.classes()).toContain('text-brand');
    expect(wrapper.get('a[href="/overview"]').classes()).not.toContain('text-brand');
  });

  it('keeps collapsed labels in the DOM so the accessible name survives', async () => {
    const wrapper = await mountNav({ activeKey: 'servers', collapsed: true });
    const label = wrapper.get('a[href="/servers"] .shell-label');

    expect(label.text()).toBe('Servers');
    expect(label.classes()).toContain('max-w-0');
    expect(label.classes()).toContain('opacity-0');
  });

  it('emits navigate so the mobile drawer can close on selection', async () => {
    const wrapper = await mountNav({ activeKey: 'overview' });
    await wrapper.get('a[href="/servers"]').trigger('click');

    expect(wrapper.emitted('navigate')).toHaveLength(1);
  });
});
