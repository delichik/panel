// @vitest-environment jsdom
import { mount } from '@vue/test-utils';
import { describe, expect, it } from 'vitest';
import ConsolePage from './ConsolePage.vue';

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
});
