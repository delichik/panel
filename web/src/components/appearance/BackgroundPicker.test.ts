// @vitest-environment jsdom
import { flushPromises, mount } from '@vue/test-utils';
import { afterEach, describe, expect, it, vi } from 'vitest';
import BackgroundPicker from './BackgroundPicker.vue';
import { useI18n } from '@/i18n';
import type { AppearanceBackground, PresetBackground } from '@/types/appearance';

const presets: readonly PresetBackground[] = [
  { id: 'aurora', gradient: 'linear-gradient(120deg, #111111, #222222)' },
];

const noBackground: AppearanceBackground = { kind: 'none', presetId: null, assetId: null, revision: 0, hasImage: false };
const imageBackground: AppearanceBackground = { kind: 'image', presetId: null, assetId: 'asset-1', revision: 1, hasImage: true };

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

// BackgroundPicker only needs the shared file validator from the appearance store.
vi.mock('@/design/appearance', () => ({
  validateBackgroundFile: (file: { type: string; size: number }) => {
    if (file.type !== 'image/jpeg' && file.type !== 'image/png') return 'type';
    if (file.size > 8 * 1024 * 1024) return 'size';
    return null;
  },
}));

afterEach(() => {
  document.body.innerHTML = '';
  vi.clearAllMocks();
});

function mountPicker(overrides: Record<string, unknown> = {}) {
  useI18n().setLocale('en');
  return mount(BackgroundPicker, {
    props: {
      background: noBackground,
      previewUrl: null,
      presets,
      ...overrides,
    },
  });
}

describe('BackgroundPicker', () => {
  it('uses 2px ring-ring focus/selected states without offset on background options', () => {
    const wrapper = mountPicker();
    const options = wrapper.findAll('[data-appearance-background-option]');
    expect(options).toHaveLength(2);

    for (const option of options) {
      expect(option.classes()).toContain('focus-visible:ring-2');
      expect(option.classes()).toContain('focus-visible:ring-ring');
      expect(option.classes()).not.toContain('focus-visible:ring-offset-2');
    }

    const selected = wrapper.find('[data-appearance-background-option][aria-checked="true"]');
    expect(selected.classes()).toContain('ring-2');
    expect(selected.classes()).toContain('ring-ring');
    expect(selected.classes()).not.toContain('ring-offset-2');
    expect(selected.classes()).not.toContain('ring-foreground');
  });

  it('reports a local validation error through the shared danger InfoBar', async () => {
    const wrapper = mountPicker();
    const input = wrapper.find('input[type="file"]');
    const invalid = new File(['hello'], 'notes.txt', { type: 'text/plain' });
    Object.defineProperty(input.element, 'files', { value: [invalid], configurable: true });
    await input.trigger('change');
    await flushPromises();

    const bar = wrapper.find('[data-info-bar]');
    expect(bar.exists()).toBe(true);
    expect(bar.attributes('data-tone')).toBe('danger');
    expect(bar.text()).toContain(useI18n().t('appearance.errors.type'));
    expect(wrapper.find('p[role="alert"]').exists()).toBe(false);
    expect(wrapper.emitted('upload')).toBeUndefined();
  });

  it('emits a valid image upload and shows upload loading through the shared overlay', async () => {
    const uploading = mountPicker({ uploading: true });
    expect(uploading.find('[role="status"]').text()).toContain(useI18n().t('appearance.background.uploading'));

    const wrapper = mountPicker();
    const input = wrapper.find('input[type="file"]');
    const image = new File([new Uint8Array([1, 2, 3])], 'background.png', { type: 'image/png' });
    Object.defineProperty(input.element, 'files', { value: [image], configurable: true });
    await input.trigger('change');
    await flushPromises();

    expect(wrapper.emitted('upload')?.at(-1)).toEqual([image]);
    expect(wrapper.find('[data-info-bar]').exists()).toBe(false);
  });

  it('renders the store-provided preview blob for an uploaded image', () => {
    const wrapper = mountPicker({ background: imageBackground, previewUrl: 'blob:preview' });
    const image = wrapper.find('img[src="blob:preview"]');
    expect(image.exists()).toBe(true);
    expect(image.attributes('alt')).toBe(useI18n().t('appearance.background.preview'));
  });
});
