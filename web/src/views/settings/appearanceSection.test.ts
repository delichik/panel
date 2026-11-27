// @vitest-environment jsdom
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils';
import { createPinia } from 'pinia';
import { computed, nextTick, ref, type ComputedRef, type Ref } from 'vue';
import { createMemoryHistory, createRouter, type Router } from 'vue-router';
import { afterEach, beforeEach, describe, expect, it, type Mock, vi } from 'vitest';
import SettingsView from './index.vue';
import { keyAssetsApi } from '@/api/keyAssets';
import { settingsApi } from '@/api/settings';
import { systemApi } from '@/api/system';
import Select from '@/components/ui/Select.vue';
import { errorDialogKey } from '@/components/ui/errorDialog';
import { toastKey } from '@/components/ui/toast';
import { useI18n } from '@/i18n';
import type {
  AccentId,
  Appearance,
  PresetBackground,
} from '@/types/appearance';
import type { RuntimeSettings } from '@/types/settings';

interface MockAppearanceStore {
  appearance: Ref<Appearance | null>;
  ready: Ref<boolean>;
  resolvedTheme: ComputedRef<'light' | 'dark'>;
  previewUrl: Ref<string | null>;
  accentPresets: readonly { id: AccentId; seed: string }[];
  presetBackgrounds: readonly PresetBackground[];
  load: Mock;
  save: Mock;
  setThemeMode: Mock;
  setMica: Mock;
  setAccent: Mock;
  setBackground: Mock;
  enableImageBackground: Mock;
  uploadBackground: Mock;
  removeBackground: Mock;
  reset: Mock;
  loadPreview: Mock;
  clearPreview: Mock;
}

/**
 * `@/design/appearance` is mocked so the settings appearance section can be
 * exercised without network/cache/DOM side effects. The async component imports
 * this module only when the appearance section actually renders.
 */
const storeMock = vi.hoisted(() => ({ current: null as unknown as MockAppearanceStore }));
const validateBackgroundFileMock = vi.hoisted(() => (file: { type: string; size: number }) => {
  if (file.type !== 'image/jpeg' && file.type !== 'image/png') return 'type';
  if (file.size > 8 * 1024 * 1024) return 'size';
  return null;
});

vi.mock('@/design/appearance', () => ({
  useAppearance: () => storeMock.current,
  validateBackgroundFile: validateBackgroundFileMock,
}));

const ACCENT_PRESETS: readonly { id: AccentId; seed: string }[] = [
  { id: 'blue', seed: '#0078D4' },
  { id: 'teal', seed: '#038387' },
  { id: 'green', seed: '#107C10' },
  { id: 'gold', seed: '#986F0B' },
  { id: 'orange', seed: '#CA5010' },
  { id: 'red', seed: '#C50F1F' },
  { id: 'pink', seed: '#C239B3' },
  { id: 'purple', seed: '#8764B8' },
  { id: 'gray', seed: '#69797E' },
];

const PRESET_BACKGROUNDS: readonly PresetBackground[] = [
  { id: 'aurora', gradient: 'linear-gradient(120deg, #111111, #222222)' },
  { id: 'dusk', gradient: 'linear-gradient(120deg, #222222, #333333)' },
  { id: 'mist', gradient: 'linear-gradient(120deg, #333333, #444444)' },
  { id: 'slate', gradient: 'linear-gradient(120deg, #444444, #555555)' },
];

function fakeAppearance(overrides: Partial<Appearance> = {}): Appearance {
  return {
    themeMode: 'system',
    mica: true,
    accent: 'blue',
    background: { kind: 'none', presetId: null, assetId: null, revision: 0, hasImage: false },
    thumbDataUrl: null,
    initialized: true,
    updatedAt: '2026-01-01T00:00:00.000Z',
    ...overrides,
  };
}

function runtimeSettings(): RuntimeSettings {
  return {
    listenAddress: '127.0.0.1:8080',
    appDatabase: 'app.db',
    metricsDatabase: 'metrics.db',
    dataRoot: 'data',
    containerReportIntervalSeconds: 30,
    tokenExpiration: '1d',
    language: 'en',
    logLevel: 'info',
    remoteCommandTimeoutSeconds: 45,
    reconcileTraceEnabled: false,
    branding: { loginTitle: 'Seamark', loginSubtitle: '' },
    certificates: { email: '', dnsPropagationDelaySeconds: 30 },
    panel: { domain: 'localhost', tlsCertificateId: '' },
    agent: { downloadBaseUrl: '', downloadVerifyTls: false, transferTimeoutSeconds: 120 },
    jwtSecretConfigured: true,
  };
}

const toastPush = vi.fn();
const errorDialogPush = vi.fn();
let wrapper: VueWrapper | undefined;
let router: Router;

// jsdom in this vitest environment has no localStorage; stub it like the other
// settings/shell tests so i18n and stores can read their keys.
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

function buildStore(overrides: Partial<Appearance> = {}): MockAppearanceStore {
  const mocks = {
    load: vi.fn(async () => undefined),
    save: vi.fn(async () => undefined),
    setThemeMode: vi.fn(async () => undefined),
    setMica: vi.fn(async () => undefined),
    setAccent: vi.fn(async () => undefined),
    setBackground: vi.fn(async () => undefined),
    enableImageBackground: vi.fn(async () => undefined),
    uploadBackground: vi.fn(async () => undefined),
    removeBackground: vi.fn(async () => undefined),
    reset: vi.fn(async () => undefined),
    loadPreview: vi.fn(async () => null),
    clearPreview: vi.fn(() => undefined),
  };
  return {
    appearance: ref<Appearance | null>(fakeAppearance(overrides)),
    ready: ref(true),
    resolvedTheme: computed(() => 'light' as const),
    previewUrl: ref<string | null>(null),
    accentPresets: ACCENT_PRESETS,
    presetBackgrounds: PRESET_BACKGROUNDS,
    ...mocks,
  };
}

async function render() {
  router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/settings/:section', component: SettingsView }] });
  await router.push('/settings/appearance');
  await router.isReady();
  wrapper = mount(SettingsView, {
    global: {
      plugins: [createPinia(), router],
      provide: {
        [toastKey as symbol]: { push: toastPush, remove: vi.fn() },
        [errorDialogKey as symbol]: { push: errorDialogPush, dismiss: vi.fn() },
      },
    },
  });
  // AppearanceSection is an async component: wait until its dynamic import and
  // setup have settled (the transform may need more than one macrotask).
  for (let attempt = 0; attempt < 100; attempt += 1) {
    await flushPromises();
    if (wrapper.find('[data-appearance-background-option]').exists() || wrapper.text().includes(useI18n().t('appearance.errors.load'))) break;
    await new Promise((resolve) => setTimeout(resolve, 10));
  }
  await flushPromises();
  await nextTick();
  return wrapper;
}

function radioByLabel(label: string) {
  const radio = wrapper!.findAll('[role="radio"]').find((item) => item.attributes('aria-label') === label);
  expect(radio, `radio "${label}"`).toBeTruthy();
  return radio!;
}

function buttonByText(text: string) {
  const button = wrapper!.findAll('button').find((item) => item.text() === text);
  expect(button, `button "${text}"`).toBeTruthy();
  return button!;
}

beforeEach(() => {
  toastPush.mockReset();
  errorDialogPush.mockReset();
  useI18n().setLocale('en');
  storeMock.current = buildStore();
  vi.spyOn(settingsApi, 'runtime').mockResolvedValue(runtimeSettings());
  vi.spyOn(settingsApi, 'serverVariables').mockResolvedValue([]);
  vi.spyOn(settingsApi, 'updateGeneral').mockImplementation(async () => runtimeSettings());
  vi.spyOn(systemApi, 'version').mockResolvedValue({ version: '0.2.0-dev', channel: 'dev', latestVersion: '', updateAvailable: false });
  vi.spyOn(keyAssetsApi, 'systemCertificates').mockResolvedValue([]);
  vi.spyOn(keyAssetsApi, 'tlsCertificates').mockResolvedValue([]);
});

afterEach(() => {
  wrapper?.unmount();
  wrapper = undefined;
  document.body.innerHTML = '';
  vi.restoreAllMocks();
});

describe('settings appearance section', () => {
  it('renders the appearance section with theme, mica, accent and background controls, without a page-level save button', async () => {
    const view = await render();

    expect(view.text()).toContain(useI18n().t('appearance.title'));
    expect(view.text()).toContain(useI18n().t('appearance.description'));
    expect(view.text()).toContain(useI18n().t('appearance.theme.label'));
    expect(view.text()).toContain(useI18n().t('appearance.mica.label'));
    expect(view.text()).toContain(useI18n().t('appearance.accent.label'));
    expect(view.text()).toContain(useI18n().t('appearance.background.label'));

    // 9 accent presets + 5 background choices (solid + 4 presets) are radios.
    const accentGroup = view.find(`[role="radiogroup"][aria-label="${useI18n().t('appearance.accent.label')}"]`);
    expect(accentGroup.exists()).toBe(true);
    expect(accentGroup.findAll('[role="radio"]')).toHaveLength(9);
    const backgroundGroup = view.find(`[role="radiogroup"][aria-label="${useI18n().t('appearance.background.label')}"]`);
    expect(backgroundGroup.exists()).toBe(true);
    expect(backgroundGroup.findAll('[role="radio"]')).toHaveLength(5);

    // 外观已迁入 SettingsSection + SettingsCard：卡外分区标题、卡内行、无手写分割/嵌套卡。
    const section = view.find('[data-settings-section]');
    expect(section.exists()).toBe(true);
    expect(view.find('h1').text()).toBe(useI18n().t('settingsPage.section.appearance'));
    expect(section.find('h2').exists()).toBe(false);
    expect(section.findAll('[data-settings-card]').length).toBeGreaterThanOrEqual(4);
    expect(section.find('.surface-card .surface-card').exists()).toBe(false);
    expect(view.findAll('h3')).toHaveLength(0);

    // 分区独立即时保存：没有「保存分区」按钮语义。
    const saveButtons = view.findAll('button').filter((item) => (
      item.text() === 'Save section' || item.text() === 'Save branding' || item.text() === 'Save variables'
    ));
    expect(saveButtons).toHaveLength(0);
  });

  it('refreshes appearance through the shared store from the page header', async () => {
    const view = await render();
    storeMock.current.load.mockClear();

    const refresh = view.findAll('button').find((item) => item.text() === useI18n().t('common.refresh'));
    expect(refresh).toBeTruthy();
    await refresh!.trigger('click');
    await flushPromises();

    expect(storeMock.current.load).toHaveBeenCalledTimes(1);
  });

  it('persists theme changes through the shared store without touching the background payload', async () => {
    const view = await render();
    const themeSelect = view.findComponent(Select);
    expect(themeSelect.exists()).toBe(true);

    themeSelect.vm.$emit('update:modelValue', 'dark');
    await flushPromises();

    expect(storeMock.current.setThemeMode).toHaveBeenCalledWith('dark');
    expect(storeMock.current.save).not.toHaveBeenCalled();
  });

  it('toggles Mica and selects accents immediately', async () => {
    const view = await render();

    await view.find('#appearance-mica').trigger('click');
    expect(storeMock.current.setMica).toHaveBeenCalledWith(false);

    await radioByLabel(useI18n().t('appearance.accent.green')).trigger('click');
    expect(storeMock.current.setAccent).toHaveBeenCalledWith('green');
  });

  it('supports keyboard navigation across the accent swatches', async () => {
    const view = await render();

    const blue = radioByLabel(useI18n().t('appearance.accent.blue'));
    expect(blue.attributes('tabindex')).toBe('0');
    expect(blue.attributes('aria-checked')).toBe('true');
    // 未选中的色板不进 Tab 序列，方向键在单选组内移动。
    expect(radioByLabel(useI18n().t('appearance.accent.red')).attributes('tabindex')).toBe('-1');

    await blue.trigger('keydown', { key: 'ArrowRight' });
    expect(storeMock.current.setAccent).toHaveBeenCalledWith('teal');
  });

  it('switches between solid and preset backgrounds and clears the image preview', async () => {
    const view = await render();

    await radioByLabel(useI18n().t('appearance.background.none')).trigger('click');
    expect(storeMock.current.setBackground).toHaveBeenCalledWith('none', null);

    await radioByLabel(useI18n().t('appearance.preset.dusk')).trigger('click');
    expect(storeMock.current.setBackground).toHaveBeenCalledWith('preset', 'dusk');
    expect(storeMock.current.clearPreview).toHaveBeenCalled();
  });

  it('uploads a validated JPEG/PNG and shows the stored preview', async () => {
    const view = await render();
    const input = view.find('input[type="file"]');
    expect(input.exists()).toBe(true);

    storeMock.current.uploadBackground.mockImplementation(async () => {
      storeMock.current.appearance.value = fakeAppearance({
        background: { kind: 'image', presetId: null, assetId: 'asset-1', revision: 1, hasImage: true },
        thumbDataUrl: 'data:image/jpeg;base64,thumb',
      });
    });
    storeMock.current.loadPreview.mockImplementation(async () => {
      storeMock.current.previewUrl.value = 'blob:preview';
      return 'blob:preview';
    });

    const file = new File([new Uint8Array([1, 2, 3])], 'background.png', { type: 'image/png' });
    Object.defineProperty(input.element, 'files', { value: [file], configurable: true });
    await input.trigger('change');
    await flushPromises();

    expect(storeMock.current.uploadBackground).toHaveBeenCalledWith(file);
    expect(view.find('img[src="blob:preview"]').exists()).toBe(true);
    expect(toastPush).toHaveBeenCalledWith(expect.objectContaining({ tone: 'success', title: useI18n().t('appearance.saved') }));
  });

  it('rejects a non JPEG/PNG file locally before calling the store', async () => {
    const view = await render();
    const input = view.find('input[type="file"]');
    const file = new File(['hello'], 'notes.txt', { type: 'text/plain' });
    Object.defineProperty(input.element, 'files', { value: [file], configurable: true });
    await input.trigger('change');
    await flushPromises();

    expect(storeMock.current.uploadBackground).not.toHaveBeenCalled();
    expect(view.text()).toContain(useI18n().t('appearance.errors.type'));
  });

  it('rejects oversized images locally before calling the store', async () => {
    const view = await render();
    const input = view.find('input[type="file"]');
    const file = new File([new Uint8Array(1)], 'huge.png', { type: 'image/png' });
    Object.defineProperty(file, 'size', { value: 8 * 1024 * 1024 + 1, configurable: true });
    Object.defineProperty(input.element, 'files', { value: [file], configurable: true });
    await input.trigger('change');
    await flushPromises();

    expect(storeMock.current.uploadBackground).not.toHaveBeenCalled();
    expect(view.text()).toContain(useI18n().t('appearance.errors.size'));
  });

  it('removes an uploaded image and restores defaults through the store', async () => {
    storeMock.current = buildStore({
      background: { kind: 'image', presetId: null, assetId: 'asset-1', revision: 2, hasImage: true },
    });
    const view = await render();
    storeMock.current.loadPreview.mockImplementation(async () => {
      storeMock.current.previewUrl.value = 'blob:preview';
      return 'blob:preview';
    });
    await flushPromises();

    await buttonByText(useI18n().t('appearance.background.remove')).trigger('click');
    await flushPromises();
    expect(storeMock.current.removeBackground).toHaveBeenCalledTimes(1);

    await buttonByText(useI18n().t('appearance.reset')).trigger('click');
    await flushPromises();
    expect(storeMock.current.reset).toHaveBeenCalledTimes(1);
  });

  it('re-enables an uploaded image after a solid/preset selection', async () => {
    storeMock.current = buildStore({
      background: { kind: 'preset', presetId: 'aurora', assetId: 'asset-1', revision: 2, hasImage: true },
    });
    const view = await render();

    await radioByLabel(useI18n().t('appearance.background.image')).trigger('click');
    expect(storeMock.current.enableImageBackground).toHaveBeenCalledTimes(1);
  });

  it('loads the original image preview when an image background is active', async () => {
    storeMock.current = buildStore({
      background: { kind: 'image', presetId: null, assetId: 'asset-1', revision: 2, hasImage: true },
    });
    storeMock.current.loadPreview.mockImplementation(async () => {
      storeMock.current.previewUrl.value = 'blob:preview';
      return 'blob:preview';
    });

    const view = await render();
    await flushPromises();

    expect(storeMock.current.loadPreview).toHaveBeenCalledTimes(1);
    expect(view.find('img[src="blob:preview"]').exists()).toBe(true);
  });

  it('renders the four fixed appearance card labels while appearance is loading', async () => {
    storeMock.current = buildStore();
    storeMock.current.appearance.value = null;
    storeMock.current.ready.value = false;
    let resolveLoad: (() => void) | undefined;
    storeMock.current.load.mockImplementation(() => new Promise<void>((resolve) => {
      resolveLoad = () => {
        storeMock.current.appearance.value = fakeAppearance();
        storeMock.current.ready.value = true;
        resolve();
      };
    }));

    router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/settings/:section', component: SettingsView }] });
    await router.push('/settings/appearance');
    await router.isReady();
    wrapper = mount(SettingsView, {
      global: {
        plugins: [createPinia(), router],
        provide: {
          [toastKey as symbol]: { push: toastPush, remove: vi.fn() },
          [errorDialogKey as symbol]: { push: errorDialogPush, dismiss: vi.fn() },
        },
      },
    });
    await flushPromises();
    await nextTick();

    const section = wrapper.get('[data-settings-section]');
    expect(section.attributes('aria-busy')).toBe('true');
    expect(section.find('h2').exists()).toBe(false);
    expect(section.findAll('[data-settings-card]')).toHaveLength(4);
    expect(section.text()).toContain(useI18n().t('appearance.theme.label'));
    expect(section.text()).toContain(useI18n().t('appearance.mica.label'));
    expect(section.text()).toContain(useI18n().t('appearance.accent.label'));
    expect(section.text()).toContain(useI18n().t('appearance.background.label'));

    resolveLoad?.();
    await flushPromises();
  });

  it('loads appearance when the shared store is not ready yet', async () => {
    storeMock.current = buildStore();
    storeMock.current.appearance.value = null;
    storeMock.current.ready.value = false;
    storeMock.current.load.mockImplementation(async () => {
      storeMock.current.appearance.value = fakeAppearance();
      storeMock.current.ready.value = true;
    });

    const view = await render();

    expect(storeMock.current.load).toHaveBeenCalledTimes(1);
    expect(view.text()).toContain(useI18n().t('appearance.theme.label'));
  });

  it('shows the inline load failure state with a retry action', async () => {
    storeMock.current = buildStore();
    storeMock.current.appearance.value = null;
    storeMock.current.ready.value = true;

    const view = await render();
    expect(view.text()).toContain(useI18n().t('appearance.errors.load'));

    await buttonByText(useI18n().t('common.retry')).trigger('click');
    await flushPromises();
    expect(storeMock.current.load).toHaveBeenCalledTimes(1);
  });

  it('uses the blocking error dialog for a save failure instead of a toast', async () => {
    const view = await render();
    const failure = new Error('appearance save failed');
    storeMock.current.setThemeMode.mockRejectedValueOnce(failure);

    view.findComponent(Select).vm.$emit('update:modelValue', 'light');
    await flushPromises();

    // 外观保存是显式用户动作：失败走全局错误弹窗，不进右上角 toast。
    expect(errorDialogPush).toHaveBeenCalledWith(expect.objectContaining({
      title: useI18n().t('appearance.errors.save'),
      reference: failure,
    }));
    expect(toastPush).not.toHaveBeenCalledWith(expect.objectContaining({ tone: 'danger' }));
  });
});
