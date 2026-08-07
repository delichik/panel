// @vitest-environment jsdom
import { flushPromises, mount } from '@vue/test-utils';
import { defineComponent, nextTick, ref } from 'vue';
import { afterEach, describe, expect, it } from 'vitest';
import { useOverlayBehavior } from './useOverlayBehavior';

afterEach(() => {
  document.body.innerHTML = '';
  document.body.style.overflow = '';
});

describe('useOverlayBehavior', () => {
  const Host = defineComponent({
    setup() {
      const open = ref(false);
      const panel = ref<HTMLElement | null>(null);
      const { onKeydown } = useOverlayBehavior({
        open: () => open.value,
        containerRef: panel,
        onClose: () => {
          open.value = false;
        },
        lockScroll: true,
      });
      return { open, panel, onKeydown };
    },
    template: `
      <button id="trigger" @click="open = true">Open</button>
      <div v-if="open" id="panel" ref="panel" tabindex="-1" @keydown="onKeydown">
        <a id="first" href="#">First</a>
        <a id="last" href="#">Last</a>
      </div>
    `,
  });

  it('focuses the first focusable, traps Tab, closes on Escape, restores focus and locks scroll', async () => {
    const wrapper = mount(Host, { attachTo: document.body });
    const trigger = wrapper.get('#trigger').element as HTMLButtonElement;
    trigger.focus();
    await wrapper.get('#trigger').trigger('click');
    await nextTick();

    expect(document.getElementById('panel')).not.toBeNull();
    expect(document.activeElement).toBe(document.getElementById('first'));
    expect(document.body.style.overflow).toBe('hidden');

    const last = document.getElementById('last') as HTMLElement;
    last.focus();
    last.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', bubbles: true, cancelable: true }));
    await nextTick();
    expect(document.activeElement).toBe(document.getElementById('first'));

    const first = document.getElementById('first') as HTMLElement;
    first.focus();
    first.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', bubbles: true, cancelable: true, shiftKey: true }));
    await nextTick();
    expect(document.activeElement).toBe(last);

    document.getElementById('panel')!.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }));
    await flushPromises();
    expect(document.getElementById('panel')).toBeNull();
    expect(document.body.style.overflow).toBe('');
    expect(document.activeElement).toBe(trigger);

    wrapper.unmount();
  });

  it('restores the previous body overflow when an overlay with lockScroll is unmounted', async () => {
    const wrapper = mount(Host, { attachTo: document.body });
    document.body.style.overflow = 'auto';
    await wrapper.get('#trigger').trigger('click');
    await nextTick();
    expect(document.body.style.overflow).toBe('hidden');

    wrapper.unmount();
    expect(document.body.style.overflow).toBe('auto');
  });
});