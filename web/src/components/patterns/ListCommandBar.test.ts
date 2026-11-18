// @vitest-environment jsdom
import { enableAutoUnmount, mount } from '@vue/test-utils';
import { afterEach, describe, expect, it } from 'vitest';
import ListCommandBar from './ListCommandBar.vue';

enableAutoUnmount(afterEach);

describe('ListCommandBar', () => {
  it('renders the three command areas plus a full-width secondary row', () => {
    const wrapper = mount(ListCommandBar, {
      slots: {
        search: '<input id="search" />',
        filters: '<div id="filters">Filters</div>',
        actions: '<button id="batch" type="button">Batch</button>',
        default: '<p id="summary">42 items</p>',
      },
    });

    const bar = wrapper.get('[data-list-command-bar]');
    expect(bar.find('#search').exists()).toBe(true);
    expect(bar.find('#filters').exists()).toBe(true);
    expect(bar.find('#batch').exists()).toBe(true);
    expect(bar.find('#summary').exists()).toBe(true);

    // 窄屏纵向折叠、宽屏单行；主操作在宽屏右对齐。
    expect(bar.classes()).toContain('flex-col');
    expect(bar.classes()).toContain('sm:flex-row');
    expect(bar.get('#search').element.parentElement?.className).toContain('sm:w-72');
    expect(bar.get('#batch').element.parentElement?.className).toContain('sm:ml-auto');

    // 手机宽度：搜索占满行，filters/actions 单列 full width（按钮不被压缩）。
    expect(bar.get('#search').element.parentElement?.className).toContain('max-sm:w-full');
    const filters = bar.get('#filters').element.parentElement!;
    expect(filters.className).toContain('max-sm:grid');
    expect(filters.className).toContain('max-sm:grid-cols-1');
    expect(filters.className).toContain('max-sm:items-stretch');
    const actions = bar.get('#batch').element.parentElement!;
    expect(actions.className).toContain('max-sm:grid');
    expect(actions.className).toContain('max-sm:grid-cols-1');
    expect(actions.className).toContain('max-sm:w-full');
    expect(actions.className).toContain('max-sm:items-stretch');
  });

  it('only renders the areas that have content', () => {
    const wrapper = mount(ListCommandBar, { slots: { search: '<input id="search" />' } });
    expect(wrapper.find('#search').exists()).toBe(true);
    expect(wrapper.findAll('[data-list-command-bar] > div')).toHaveLength(1);
  });

  it('exposes an accessible group name when a label is provided and never fakes selection state', () => {
    const wrapper = mount(ListCommandBar, {
      props: { label: 'Server filters' },
      slots: { filters: '<button id="filter" type="button">Agent</button>' },
    });

    const bar = wrapper.get('[data-list-command-bar]');
    expect(bar.attributes('role')).toBe('group');
    expect(bar.attributes('aria-label')).toBe('Server filters');

    // 选中态属于 Tabs/ToggleButton 语义；命令栏自身不提供 aria-pressed/role=toolbar。
    expect(bar.attributes('aria-pressed')).toBeUndefined();
    expect(bar.attributes('role')).not.toBe('toolbar');
    expect(wrapper.find('[aria-pressed]').exists()).toBe(false);
  });

  it('keeps semantic layout classes instead of arbitrary sizing', () => {
    const wrapper = mount(ListCommandBar);
    const bar = wrapper.get('[data-list-command-bar]');
    for (const className of bar.classes()) {
      expect(className).not.toMatch(/-\[.*\]/);
    }
  });
});
