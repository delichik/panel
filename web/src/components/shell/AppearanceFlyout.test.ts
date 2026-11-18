// @vitest-environment jsdom
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils';
import { createMemoryHistory, createRouter, type Router } from 'vue-router';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { appearanceApi } from '@/api/appearance';
import { toastKey } from '@/components/ui/toast';
import { useAppearance } from '@/design/appearance';
import { useI18n } from '@/i18n';
import type { Appearance } from '@/types/appearance';
import AppearanceFlyout from './AppearanceFlyout.vue';

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

const mounts: VueWrapper[] = [];
const toastPush = vi.fn();

function appearanceFixture(overrides: Partial<Appearance> = {}): Appearance {
  return {
    themeMode: 'system',
    mica: true,
    accent: 'blue',
    background: { kind: 'none', presetId: null, assetId: null, revision: 0, hasImage: false },
    thumbDataUrl: null,
    initialized: true,
    updatedAt: '2026-01-01T00:00:00Z',
    ...overrides,
  };
}

function makeRouter(): Router {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { template: '<div />' } },
      { path: '/settings/appearance', component: { template: '<div />' } },
    ],
  });
}

async function mountFlyout(router: Router) {
  await router.push('/');
  await router.isReady();
  const wrapper = mount(AppearanceFlyout, {
    attachTo: document.body,
    global: {
      plugins: [router],
      provide: { [toastKey as symbol]: { push: toastPush, remove: vi.fn() } },
    },
  });
  mounts.push(wrapper);
  return wrapper;
}

function triggerButton(wrapper: VueWrapper) {
  return wrapper.get<HTMLButtonElement>('button[aria-label="Appearance"]');
}

async function openFlyout(wrapper: VueWrapper) {
  const trigger = triggerButton(wrapper);
  trigger.element.focus();
  await trigger.trigger('click');
  await flushPromises();
  const panel = document.querySelector<HTMLElement>('[role="dialog"][aria-label="Appearance"]');
  expect(panel).not.toBeNull();
  return { trigger, panel: panel! };
}

beforeEach(() => {
  toastPush.mockReset();
  useI18n().setLocale('en');
  // Seed the shared store with a loaded server appearance so the flyout has
  // concrete values without racing the authenticated load.
  useAppearance().appearance.value = appearanceFixture();
  vi.spyOn(appearanceApi, 'update').mockImplementation(async (input) => {
    const current = useAppearance().appearance.value ?? appearanceFixture();
    return {
      ...current,
      themeMode: input.themeMode,
      mica: input.mica,
      accent: input.accent,
    };
  });
});

afterEach(() => {
  while (mounts.length) mounts.pop()!.unmount();
  document.body.innerHTML = '';
  localStorage.clear();
  useI18n().setLocale('zh-CN');
  vi.restoreAllMocks();
});

describe('AppearanceFlyout', () => {
  it('is a dialog-styled light-dismiss flyout with focus management', async () => {
    const wrapper = await mountFlyout(makeRouter());
    const { trigger, panel } = await openFlyout(wrapper);

    expect(trigger.attributes('aria-haspopup')).toBe('dialog');
    expect(trigger.attributes('aria-expanded')).toBe('true');
    expect(panel.id).toBe(trigger.attributes('aria-controls'));
    // Non-modal popup (does not trap the whole page behind a modal overlay).
    expect(panel.getAttribute('aria-modal')).toBe('false');
    expect(panel.contains(document.activeElement)).toBe(true);
    expect(document.activeElement?.getAttribute('role')).toBe('radio');
    // WinUI flyout surface is centralized in the shared `surface-flyout` utility
    // (8px radius + 1px stroke + flyout fill + shadow) plus acrylic blur.
    expect(panel.className).toContain('surface-flyout');
    expect(panel.className).toContain('backdrop-blur-flyout');

    panel.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }));
    await flushPromises();

    expect(document.querySelector('[role="dialog"][aria-label="Appearance"]')).toBeNull();
    expect(trigger.attributes('aria-expanded')).toBe('false');
    expect(document.activeElement).toBe(trigger.element);
  });

  it('saves the theme mode with the store and never drops the background', async () => {
    const wrapper = await mountFlyout(makeRouter());
    const { panel } = await openFlyout(wrapper);

    const dark = Array.from(panel.querySelectorAll<HTMLButtonElement>('[role="radio"]'))
      .find((option) => option.textContent?.trim() === 'Dark');
    expect(dark).toBeTruthy();
    dark!.click();
    await flushPromises();

    expect(appearanceApi.update).toHaveBeenCalledTimes(1);
    const payload = vi.mocked(appearanceApi.update).mock.calls[0]![0];
    expect(payload).toMatchObject({ themeMode: 'dark', mica: true, accent: 'blue' });
    // Omitting `background` keeps an uploaded image / preset untouched.
    expect(payload).not.toHaveProperty('background');
    expect(useAppearance().appearance.value?.themeMode).toBe('dark');
    expect(dark!.getAttribute('aria-checked')).toBe('true');
  });

  it('supports radio-group keyboard navigation for the theme row', async () => {
    const wrapper = await mountFlyout(makeRouter());
    const { panel } = await openFlyout(wrapper);

    const system = panel.querySelector<HTMLButtonElement>('[role="radio"][aria-checked="true"]')!;
    expect(system.textContent?.trim()).toBe('Automatic');
    system.focus();
    system.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true, cancelable: true }));
    await flushPromises();

    expect(useAppearance().appearance.value?.themeMode).toBe('light');
    expect(appearanceApi.update).toHaveBeenCalledWith(expect.objectContaining({ themeMode: 'light' }));
    expect(document.activeElement?.getAttribute('aria-checked')).toBe('true');
  });

  it('toggles Mica through the store', async () => {
    const wrapper = await mountFlyout(makeRouter());
    const { panel } = await openFlyout(wrapper);

    const toggle = panel.querySelector<HTMLButtonElement>('[role="switch"]');
    expect(toggle).not.toBeNull();
    expect(toggle!.getAttribute('aria-checked')).toBe('true');
    toggle!.click();
    await flushPromises();

    expect(appearanceApi.update).toHaveBeenCalledWith(expect.objectContaining({ mica: false }));
    expect(useAppearance().appearance.value?.mica).toBe(false);
  });

  it('renders the 9 Windows accent presets and saves the selected accent', async () => {
    const wrapper = await mountFlyout(makeRouter());
    const { panel } = await openFlyout(wrapper);

    const swatches = panel.querySelectorAll('[role="radiogroup"] [role="radio"]');
    // 3 theme radios + 9 accent swatches.
    expect(swatches).toHaveLength(12);
    const red = panel.querySelector<HTMLButtonElement>('button[aria-label="Red"]')!;
    expect(red.getAttribute('aria-checked')).toBe('false');
    red.click();
    await flushPromises();

    expect(appearanceApi.update).toHaveBeenCalledWith(expect.objectContaining({ accent: 'red' }));
    expect(useAppearance().appearance.value?.accent).toBe('red');
    expect(red.getAttribute('aria-checked')).toBe('true');
  });

  it('links to the settings background manager and closes itself', async () => {
    const router = makeRouter();
    const wrapper = await mountFlyout(router);
    const { panel } = await openFlyout(wrapper);

    const manage = Array.from(panel.querySelectorAll<HTMLButtonElement>('button'))
      .find((button) => button.textContent?.includes('Manage background'));
    expect(manage).toBeTruthy();
    manage!.click();
    await flushPromises();

    expect(router.currentRoute.value.path).toBe('/settings/appearance');
    expect(document.querySelector('[role="dialog"][aria-label="Appearance"]')).toBeNull();
  });

  it('rolls back and toasts when the save fails', async () => {
    vi.mocked(appearanceApi.update).mockRejectedValue(new Error('offline'));
    const wrapper = await mountFlyout(makeRouter());
    const { panel } = await openFlyout(wrapper);

    const dark = Array.from(panel.querySelectorAll<HTMLButtonElement>('[role="radio"]'))
      .find((option) => option.textContent?.trim() === 'Dark');
    dark!.click();
    await flushPromises();

    // The store rolled the optimistic theme back; the flyout reports the failure.
    expect(useAppearance().appearance.value?.themeMode).toBe('system');
    expect(toastPush).toHaveBeenCalledWith(expect.objectContaining({
      title: 'Unable to save appearance settings.',
      tone: 'danger',
    }));
  });

  it('closes on an outside pointer press without stealing focus', async () => {
    const wrapper = await mountFlyout(makeRouter());
    await openFlyout(wrapper);

    document.body.dispatchEvent(new MouseEvent('pointerdown', { bubbles: true }));
    await flushPromises();

    expect(document.querySelector('[role="dialog"][aria-label="Appearance"]')).toBeNull();
  });
});
