// @vitest-environment jsdom
import { mount } from '@vue/test-utils';
import { afterEach, describe, expect, it } from 'vitest';
import AccentPalette from './AccentPalette.vue';
import { useI18n } from '@/i18n';
import type { AccentId } from '@/types/appearance';

const presets: Array<{ id: AccentId; seed: string }> = [
  { id: 'blue', seed: '#0078D4' },
  { id: 'teal', seed: '#038387' },
  { id: 'red', seed: '#C50F1F' },
];

function createStorage(): Storage {
  let store = new Map<string, string>();
  return {
    get length() {
      return store.size;
    },
    clear() {
      store = new Map();
    },
    getItem(key: string) {
      return store.has(key) ? store.get(key)! : null;
    },
    key(index: number) {
      return Array.from(store.keys())[index] ?? null;
    },
    removeItem(key: string) {
      store.delete(key);
    },
    setItem(key: string, value: string) {
      store.set(key, String(value));
    },
  };
}
vi.stubGlobal('localStorage', createStorage());

afterEach(() => {
  document.body.innerHTML = '';
});

describe('AccentPalette', () => {
  it('uses a 2px ring-ring focus ring without offset and marks the selected preset', () => {
    useI18n().setLocale('en');
    const wrapper = mount(AccentPalette, {
      props: { modelValue: 'teal', label: 'Accent color', presets },
    });

    const radios = wrapper.findAll('[role="radio"]');
    expect(radios).toHaveLength(3);
    for (const radio of radios) {
      expect(radio.classes()).toContain('focus-visible:ring-2');
      expect(radio.classes()).toContain('focus-visible:ring-ring');
      expect(radio.classes()).not.toContain('focus-visible:ring-offset-2');
      expect(radio.classes()).not.toContain('focus-visible:ring-offset-card');
    }

    const selected = wrapper.find('[role="radio"][aria-checked="true"]');
    expect(selected.attributes('aria-label')).toBe(useI18n().t('appearance.accent.teal'));
    expect(selected.classes()).toContain('ring-2');
    expect(selected.classes()).toContain('ring-ring');
    expect(selected.classes()).not.toContain('ring-offset-2');
    expect(selected.classes()).not.toContain('ring-foreground');
    expect(selected.attributes('tabindex')).toBe('0');
    expect(wrapper.findAll('[role="radio"][tabindex="-1"]')).toHaveLength(2);
  });

  it('moves selection with arrow keys inside the radiogroup', async () => {
    useI18n().setLocale('en');
    const wrapper = mount(AccentPalette, {
      props: { modelValue: 'blue', label: 'Accent color', presets },
    });

    const first = wrapper.findAll('[role="radio"]')[0]!;
    await first.trigger('keydown', { key: 'ArrowRight' });

    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual(['teal']);
  });
});
