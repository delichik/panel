// @vitest-environment jsdom
import { mount } from '@vue/test-utils';
import { afterEach, describe, expect, it } from 'vitest';
import SettingsCard from './SettingsCard.vue';

afterEach(() => {
  document.body.innerHTML = '';
});

function mountCard(overrides: Record<string, unknown> = {}) {
  return mount(SettingsCard, {
    props: {
      title: 'Language',
      description: 'Panel interface language',
      ...overrides,
    },
    slots: {
      control: '<button id="control" type="button">English</button>',
      hint: '<span id="hint">Saved to server</span>',
    },
  });
}

describe('SettingsCard mobile stacking', () => {
  it('stacks title and control vertically on narrow screens', () => {
    const wrapper = mountCard();
    const card = wrapper.get('[data-settings-card]');
    expect(card.classes()).toContain('max-sm:flex-col');
    expect(card.classes()).toContain('max-sm:items-stretch');

    const controlColumn = wrapper.get('#control').element.parentElement!;
    expect(controlColumn.className).toContain('max-sm:w-full');
    expect(controlColumn.className).toContain('max-sm:flex-col');
    expect(controlColumn.className).toContain('max-sm:items-stretch');
    expect(controlColumn.className).toContain('max-sm:justify-start');
    wrapper.unmount();
  });

  it('keeps long titles/descriptions wrapping instead of overflowing', () => {
    const wrapper = mountCard({ title: 'A-very-long-unbroken-setting-title', description: 'A-long-hint-that-must-wrap' });
    const texts = wrapper.findAll('p');
    expect(texts[0]!.classes()).toContain('break-words');
    expect(texts[1]!.classes()).toContain('break-words');
    wrapper.unmount();
  });

  it('keeps the control and hint on their own full-width row', () => {
    const wrapper = mountCard();
    const column = wrapper.get('#control').element.parentElement!.parentElement!;
    expect(column.className).toContain('max-sm:w-full');
    expect(column.className).toContain('max-sm:items-stretch');
    expect(wrapper.get('#hint')).toBeTruthy();
    wrapper.unmount();
  });
});
