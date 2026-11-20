// @vitest-environment jsdom
import { mount } from '@vue/test-utils';
import { describe, expect, it } from 'vitest';
import ListPage from './ListPage.vue';

describe('ListPage', () => {
  it('owns the toolbar / table scroll cell / pagination geometry', () => {
    const wrapper = mount(ListPage, {
      slots: {
        toolbar: '<div data-toolbar>toolbar</div>',
        default: '<div data-table>table</div>',
        pagination: '<div data-pagination>pagination</div>',
      },
    });

    expect(wrapper.classes()).toContain('h-full');
    expect(wrapper.classes()).toContain('min-h-0');
    expect(wrapper.classes()).toContain('grid-rows-page-footer');
    const [, body, pagination] = Array.from(wrapper.element.children) as HTMLElement[];
    expect(body!.className).toContain('overflow-hidden');
    expect(pagination!.className).toContain('shrink-0');
    expect(wrapper.find('[data-table]').exists()).toBe(true);
  });

  it('renders page-level states through the shared slots instead of page-local branches', async () => {
    const wrapper = mount(ListPage, {
      slots: {
        default: '<div data-ready>table</div>',
        loading: '<div data-loading>skeleton</div>',
        error: '<div data-error>failed</div>',
        empty: '<div data-empty>nothing</div>',
      },
    });

    expect(wrapper.find('[data-ready]').exists()).toBe(true);

    await wrapper.setProps({ state: 'loading' });
    expect(wrapper.find('[data-loading]').exists()).toBe(true);
    expect(wrapper.find('[data-ready]').exists()).toBe(false);

    await wrapper.setProps({ state: 'error' });
    expect(wrapper.find('[data-error]').exists()).toBe(true);
    expect(wrapper.find('[data-loading]').exists()).toBe(false);

    await wrapper.setProps({ state: 'empty' });
    expect(wrapper.find('[data-empty]').exists()).toBe(true);
    expect(wrapper.find('[data-error]').exists()).toBe(false);

    await wrapper.setProps({ state: 'ready' });
    expect(wrapper.find('[data-ready]').exists()).toBe(true);
  });

  it('does not reserve the state region when no state slot is provided', () => {
    const wrapper = mount(ListPage, { slots: { default: '<div data-ready>table</div>' } });
    expect(wrapper.text()).toContain('table');
    expect(wrapper.find('[data-loading]').exists()).toBe(false);
  });
});
