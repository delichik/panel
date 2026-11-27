// @vitest-environment jsdom
import { mount } from '@vue/test-utils';
import { nextTick } from 'vue';
import { afterEach, describe, expect, it, vi } from 'vitest';
import Tooltip from './Tooltip.vue';

afterEach(() => {
  document.body.innerHTML = '';
  vi.useRealTimers();
});

function mountTooltip() {
  return mount(Tooltip, {
    attachTo: document.body,
    props: { text: 'Helpful hint' },
    slots: { default: '<button id="tip-trigger">Trigger</button>' },
  });
}

describe('Tooltip', () => {
  it('shows after the WinUI 500ms hover delay and teleports the panel to body', async () => {
    vi.useFakeTimers();
    const wrapper = mountTooltip();

    // mouseenter does not bubble, so hover the wrapper (the element that owns the listener).
    await wrapper.trigger('mouseenter');
    await vi.advanceTimersByTimeAsync(499);
    await nextTick();
    expect(document.body.querySelector('[role="tooltip"]')).toBeNull();

    await vi.advanceTimersByTimeAsync(1);
    await nextTick();
    const tip = document.body.querySelector<HTMLElement>('[role="tooltip"]');
    expect(tip).not.toBeNull();
    expect(tip!.textContent).toContain('Helpful hint');
    // Teleported + fixed positioning so overflow containers cannot clip it.
    expect(tip!.className).toContain('fixed');
    expect(tip!.className).toContain('max-w-80');
    expect(tip!.className).toContain('z-40');
    expect(tip!.style.position).toBe('fixed');
    wrapper.unmount();
  });

  it('shows immediately on keyboard focus and hides when focus leaves', async () => {
    const wrapper = mountTooltip();
    const trigger = wrapper.get('#tip-trigger');

    await trigger.trigger('focusin');
    await nextTick();
    expect(document.body.querySelector('[role="tooltip"]')).not.toBeNull();

    await trigger.trigger('focusout');
    await nextTick();
    expect(document.body.querySelector('[role="tooltip"]')).toBeNull();
    wrapper.unmount();
  });

  it('hides when the pointer leaves before the hover delay elapses', async () => {
    vi.useFakeTimers();
    const wrapper = mountTooltip();

    await wrapper.trigger('mouseenter');
    await wrapper.trigger('mouseleave');
    await vi.advanceTimersByTimeAsync(600);
    await nextTick();
    expect(document.body.querySelector('[role="tooltip"]')).toBeNull();
    wrapper.unmount();
  });
});
