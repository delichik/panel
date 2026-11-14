// @vitest-environment jsdom
import { mount } from '@vue/test-utils';
import { describe, expect, it, vi } from 'vitest';
import TableDetailLayout from './TableDetailLayout.vue';

function panes(wrapper: ReturnType<typeof mount>) {
  return Array.from(wrapper.element.children) as HTMLElement[];
}

describe('TableDetailLayout', () => {
  it('owns the height contract and stacks the list and detail in one cell', () => {
    const wrapper = mount(TableDetailLayout, {
      slots: {
        list: '<div id="list">list</div>',
        detail: '<div id="detail">detail</div>',
      },
    });

    expect(wrapper.classes()).toContain('h-full');
    expect(wrapper.classes()).toContain('min-h-0');
    // 根建立独立层叠上下文，列表内的 z-index（sticky 表头/Tooltip）不得刺穿详情或外壳顶栏。
    expect(wrapper.classes()).toContain('isolate');
    expect(wrapper.get('#list').text()).toBe('list');
    // No detail selected: the list keeps the cell and is fully interactive.
    const listPane = panes(wrapper)[0]!;
    expect(listPane.className).toContain('col-start-1');
    expect(listPane.className).toContain('relative');
    expect(listPane.className).toContain('z-0');
    expect(listPane.hasAttribute('inert')).toBe(false);
    expect(listPane.getAttribute('aria-hidden')).toBeNull();
    expect(wrapper.find('#detail').exists()).toBe(false);
  });

  it('covers the list with the detail and hides the list from assistive tech', async () => {
    const wrapper = mount(TableDetailLayout, {
      props: { hasDetail: true },
      global: { stubs: { transition: false } },
      slots: {
        list: '<div id="list">list</div>',
        detail: '<div id="detail">detail</div>',
      },
    });

    const [listPane, detailPane] = panes(wrapper);
    expect(listPane!.hasAttribute('inert')).toBe(true);
    expect(listPane!.getAttribute('aria-hidden')).toBe('true');
    expect(detailPane!.className).toContain('col-start-1');
    expect(detailPane!.className).toContain('relative');
    expect(detailPane!.className).toContain('z-10');
    expect(detailPane!.className).toContain('bg-background');
    expect(wrapper.get('#detail').text()).toBe('detail');
    // The list stays mounted so scroll position and filters survive the detour.
    expect(wrapper.find('#list').exists()).toBe(true);

    await wrapper.setProps({ hasDetail: false });
    await vi.waitFor(() => expect(wrapper.find('#detail').exists()).toBe(false));
    expect(panes(wrapper)[0]!.hasAttribute('inert')).toBe(false);
    expect(wrapper.find('#list').exists()).toBe(true);
  });
});
