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
    expect(wrapper.get('#list').text()).toBe('list');
    // No detail selected: the list keeps the cell and is fully interactive.
    expect(panes(wrapper)[0]!.className).toContain('col-start-1');
    expect(panes(wrapper)[0]!.hasAttribute('inert')).toBe(false);
    expect(panes(wrapper)[0]!.getAttribute('aria-hidden')).toBeNull();
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
