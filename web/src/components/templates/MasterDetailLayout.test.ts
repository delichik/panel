// @vitest-environment jsdom
import { mount } from '@vue/test-utils';
import { describe, expect, it, vi } from 'vitest';
import MasterDetailLayout from './MasterDetailLayout.vue';

describe('MasterDetailLayout', () => {
  it('owns the shared geometry and height contract', () => {
    const wrapper = mount(MasterDetailLayout, {
      slots: {
        master: '<aside id="master">master</aside>',
        detail: '<main id="detail">detail</main>',
      },
    });

    expect(wrapper.classes()).toContain('h-full');
    expect(wrapper.classes()).toContain('min-h-0');
    expect(wrapper.classes()).toContain('overflow-x-hidden');
    expect(wrapper.classes()).toContain('xl:grid-cols-[360px_minmax(0,1fr)]');
    expect(wrapper.get('#master').text()).toBe('master');
    expect(wrapper.get('#detail').text()).toBe('detail');
  });

  it('keeps the detail pane mounted across a selected-object change', async () => {
    const wrapper = mount(MasterDetailLayout, {
      props: { detailKey: 'first' },
      slots: {
        master: '<aside id="master">master</aside>',
        detail: '<main id="detail">detail body</main>',
      },
    });

    await wrapper.setProps({ detailKey: 'second' });
    // The swap is enter-only: the outgoing pane leaves immediately and the new
    // pane must always come back with the same structure.
    await vi.waitFor(() => expect(wrapper.find('#detail').exists()).toBe(true));

    expect(wrapper.get('#detail').text()).toBe('detail body');
  });

  it('keeps both panes rendered when the page does not opt into single view', () => {
    const wrapper = mount(MasterDetailLayout, {
      slots: {
        master: '<aside id="master">master</aside>',
        detail: '<main id="detail">detail body</main>',
      },
    });

    const [masterPane, detailPane] = Array.from(wrapper.element.children) as HTMLElement[];
    expect(masterPane!.className).not.toContain('max-xl:hidden');
    expect(detailPane!.className).not.toContain('max-xl:hidden');
    expect(wrapper.find('button').exists()).toBe(false);
  });

  it('shows only the list or only the detail below xl and exposes a back action', async () => {
    const wrapper = mount(MasterDetailLayout, {
      props: { hasDetail: false, backLabel: 'Back to list' },
      slots: {
        master: '<aside id="master">master</aside>',
        detail: '<main id="detail">detail body</main>',
      },
    });
    const panes = () => Array.from(wrapper.element.children) as HTMLElement[];

    // Nothing selected: narrow screens stay on the list.
    expect(panes()[0]!.className).not.toContain('max-xl:hidden');
    expect(panes()[1]!.className).toContain('max-xl:hidden');
    expect(wrapper.find('button').exists()).toBe(false);

    // Selection: narrow screens show the detail with a localized back action.
    await wrapper.setProps({ hasDetail: true });
    expect(panes()[0]!.className).toContain('max-xl:hidden');
    expect(panes()[1]!.className).not.toContain('max-xl:hidden');
    const back = wrapper.get('button');
    expect(back.text()).toBe('Back to list');
    await back.trigger('click');
    expect(wrapper.emitted('back')).toHaveLength(1);
  });
});
