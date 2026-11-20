// @vitest-environment jsdom
import { mount } from '@vue/test-utils';
import { nextTick } from 'vue';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import DateTimeRangePicker from './DateTimeRangePicker.vue';
import { useI18n } from '@/i18n';

beforeEach(() => {
  useI18n().setLocale('en');
});

afterEach(() => {
  document.body.innerHTML = '';
});

function controlClassPresent(element: HTMLElement): boolean {
  return ['h-control', 'h-control-sm', 'size-control', 'size-control-sm'].some((name) => element.classList.contains(name));
}

describe('DateTimeRangePicker control sizing', () => {
  it('keeps the field min width and every button on a control height token', async () => {
    const wrapper = mount(DateTimeRangePicker, {
      attachTo: document.body,
      props: { modelValue: {} },
    });
    expect(wrapper.classes()).toContain('min-w-field');

    const trigger = wrapper.get('button');
    expect(trigger.classes()).toContain('h-control');

    await trigger.trigger('click');
    await nextTick();
    const panel = document.querySelector<HTMLElement>('[role="dialog"]');
    expect(panel).not.toBeNull();

    const buttons = [...panel!.querySelectorAll<HTMLButtonElement>('button')];
    expect(buttons.length).toBeGreaterThan(0);
    for (const button of buttons) {
      expect(controlClassPresent(button), `${button.textContent?.trim() || button.getAttribute('aria-label')} needs a control-height token`).toBe(true);
    }

    // 日格提升到 32px（h-control），仍保持 7 列网格布局。
    const dayCells = buttons.filter((button) => /^\d{1,2}$/.test(button.textContent?.trim() ?? ''));
    expect(dayCells.length).toBeGreaterThan(0);
    for (const cell of dayCells) expect(cell.classList.contains('h-control')).toBe(true);

    // 月导航 32px。
    const navButtons = buttons.filter((button) => (button.getAttribute('aria-label') ?? '').toLowerCase().includes('month'));
    expect(navButtons.length).toBeGreaterThan(0);
    for (const button of navButtons) expect(button.classList.contains('size-control')).toBe(true);

    wrapper.unmount();
  });
});
