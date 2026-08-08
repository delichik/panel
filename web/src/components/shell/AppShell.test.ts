// @vitest-environment jsdom
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils';
import { createPinia } from 'pinia';
import { createMemoryHistory, createRouter, type Router } from 'vue-router';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { useI18n } from '@/i18n';

// This vitest jsdom environment does not provide localStorage; stub it like
// other tests stub fetch so AppShell / theme can read persistence keys.
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

function makeRouter(): Router {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { template: '<div />' } },
      { path: '/servers', component: { template: '<div />' } },
      { path: '/fragment', component: { template: '<div /><section />' } },
      { path: '/:pathMatch(.*)*', component: { template: '<div />' } },
    ],
  });
}

// AppShell teleports the mobile drawer to <body>; unmount every mounted shell
// after each test so no stale teleported DOM leaks into the next test.
const mountedWrappers: VueWrapper[] = [];

async function mountShell() {
  const { default: AppShell } = await import('./AppShell.vue');
  const router = makeRouter();
  await router.push('/');
  await router.isReady();
  const wrapper = mount(AppShell, {
    attachTo: document.body,
    global: { plugins: [createPinia(), router] },
  });
  mountedWrappers.push(wrapper);
  await flushPromises();
  return { wrapper, router };
}

function stubMatchMedia() {
  const listeners: Array<(event: { matches: boolean }) => void> = [];
  const mql = {
    matches: false,
    media: '(min-width: 1024px)',
    onchange: null,
    addEventListener: (_type: string, listener: (event: { matches: boolean }) => void) => listeners.push(listener),
    removeEventListener: () => undefined,
    addListener: () => undefined,
    removeListener: () => undefined,
    dispatchEvent: () => false,
  };
  (window as unknown as { matchMedia: (query: string) => typeof mql }).matchMedia = () => mql;
  return {
    emit(matches: boolean) {
      mql.matches = matches;
      listeners.forEach((listener) => listener({ matches }));
    },
  };
}

beforeEach(() => {
  useI18n().setLocale('en');
});

afterEach(() => {
  while (mountedWrappers.length) mountedWrappers.pop()!.unmount();
  document.body.innerHTML = '';
  document.body.style.overflow = '';
  localStorage.clear();
  useI18n().setLocale('zh-CN');
});

describe('AppShell mobile nav drawer', () => {
  it('opens as a modal dialog with aria wiring, focus and background lock', async () => {
    const { wrapper } = await mountShell();
    const trigger = wrapper.get('button[aria-label="Open navigation"]');
    expect(trigger.attributes('aria-haspopup')).toBe('dialog');
    expect(trigger.attributes('aria-expanded')).toBe('false');
    const controlsId = trigger.attributes('aria-controls');
    expect(controlsId).toBeTruthy();

    await trigger.trigger('click');
    await flushPromises();

    const dialog = document.querySelector<HTMLElement>('[role="dialog"]');
    expect(dialog).not.toBeNull();
    expect(dialog?.getAttribute('aria-modal')).toBe('true');
    expect(dialog?.getAttribute('aria-label')).toBe('Main navigation');
    expect(dialog?.id).toBe(controlsId);
    expect(trigger.attributes('aria-expanded')).toBe('true');
    expect(document.body.style.overflow).toBe('hidden');
    expect(wrapper.element.hasAttribute('inert')).toBe(true);
    expect(document.activeElement).not.toBeNull();
    expect(dialog?.contains(document.activeElement)).toBe(true);
    expect(document.activeElement?.getAttribute('aria-label')).toBe('Close');
  });

  it('closes with Escape, restores focus and unlocks background scroll', async () => {
    const { wrapper } = await mountShell();
    const trigger = wrapper.get('button[aria-label="Open navigation"]');
    (trigger.element as HTMLElement).focus();
    await trigger.trigger('click');
    await flushPromises();
    const dialog = document.querySelector<HTMLElement>('[role="dialog"]')!;
    expect(document.body.style.overflow).toBe('hidden');

    dialog.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }));
    await flushPromises();

    expect(document.querySelector('[role="dialog"]')).toBeNull();
    expect(document.body.style.overflow).toBe('');
    expect(wrapper.element.hasAttribute('inert')).toBe(false);
    expect(document.activeElement).toBe(trigger.element);
  });

  it('closes through the visible close button and the overlay', async () => {
    const { wrapper } = await mountShell();
    const trigger = wrapper.get('button[aria-label="Open navigation"]');

    await trigger.trigger('click');
    await flushPromises();
    const closeButton = document.querySelector<HTMLButtonElement>('[role="dialog"] button[aria-label="Close"]');
    expect(closeButton).not.toBeNull();
    closeButton!.click();
    await flushPromises();
    expect(document.querySelector('[role="dialog"]')).toBeNull();

    await trigger.trigger('click');
    await flushPromises();
    const dialog = document.querySelector<HTMLElement>('[role="dialog"]')!;
    dialog.parentElement!.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    await flushPromises();
    expect(document.querySelector('[role="dialog"]')).toBeNull();
  });

  it('closes the drawer after navigating to a nav target', async () => {
    const { wrapper, router } = await mountShell();
    const trigger = wrapper.get('button[aria-label="Open navigation"]');
    await trigger.trigger('click');
    await flushPromises();

    const link = document.querySelector<HTMLAnchorElement>('[role="dialog"] a[href="/servers"]');
    expect(link).not.toBeNull();
    link!.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }));
    await flushPromises();

    expect(router.currentRoute.value.path).toBe('/servers');
    expect(document.querySelector('[role="dialog"]')).toBeNull();
  });

  it('does not warn when a multi-root page enters the route transition', async () => {
    const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});
    try {
      const { router } = await mountShell();
      await router.push('/fragment');
      await flushPromises();
      const warned = warnSpy.mock.calls.some((args) => String(args[0]).includes('non-element root node'));
      expect(warned).toBe(false);
    } finally {
      warnSpy.mockRestore();
    }
  });

  it('closes automatically when crossing into the desktop breakpoint', async () => {
    const mql = stubMatchMedia();
    const { wrapper } = await mountShell();
    const trigger = wrapper.get('button[aria-label="Open navigation"]');
    await trigger.trigger('click');
    await flushPromises();
    expect(document.querySelector('[role="dialog"]')).not.toBeNull();

    mql.emit(true);
    await flushPromises();

    expect(document.querySelector('[role="dialog"]')).toBeNull();
    expect(document.body.style.overflow).toBe('');
  });
});
