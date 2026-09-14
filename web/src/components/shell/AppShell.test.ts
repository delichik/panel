// @vitest-environment jsdom
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils';
import { createPinia } from 'pinia';
import { createMemoryHistory, createRouter, type Router } from 'vue-router';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { settingsApi } from '@/api/settings';
import { toastKey } from '@/components/ui/toast';
import { useI18n } from '@/i18n';
import { beginRouteNavigation, finishRouteNavigation } from '@/router/navigationState';
import type { RuntimeSettings } from '@/types/settings';

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
const toastPush = vi.fn();

function runtimeSettings(language = 'en'): RuntimeSettings {
  return {
    listenAddress: '127.0.0.1:8080',
    appDatabase: 'app.db',
    metricsDatabase: 'metrics.db',
    dataRoot: 'data',
    metricsRetentionDays: 14,
    metricsCollectionIntervalSeconds: 60,
    containerReportIntervalSeconds: 30,
    cleanupSchedule: 'daily',
    tokenExpiration: '1d',
    language,
    logLevel: 'info',
    remoteCommandTimeoutSeconds: 45,
    reconcileTraceEnabled: false,
    branding: { loginTitle: 'Seamark', loginSubtitle: '' },
    certificates: { email: '', dnsPropagationDelaySeconds: 30 },
    panel: { domain: 'localhost', tlsCertificateId: '' },
    jwtSecretConfigured: true,
  };
}

async function mountShell() {
  const { default: AppShell } = await import('./AppShell.vue');
  const router = makeRouter();
  await router.push('/');
  await router.isReady();
  const wrapper = mount(AppShell, {
    attachTo: document.body,
    global: {
      plugins: [createPinia(), router],
      provide: { [toastKey as symbol]: { push: toastPush, remove: vi.fn() } },
    },
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
  toastPush.mockReset();
  vi.spyOn(settingsApi, 'runtime').mockResolvedValue(runtimeSettings());
  vi.spyOn(settingsApi, 'updateLanguage').mockImplementation(async (language) => runtimeSettings(language));
  useI18n().setLocale('en');
});

afterEach(() => {
  finishRouteNavigation();
  while (mountedWrappers.length) mountedWrappers.pop()!.unmount();
  document.body.innerHTML = '';
  document.body.style.overflow = '';
  localStorage.clear();
  useI18n().setLocale('zh-CN');
  vi.restoreAllMocks();
});

describe('AppShell mobile nav drawer', () => {
  it('persists a language switch and exposes its pending state', async () => {
    let resolveSave!: (settings: RuntimeSettings) => void;
    vi.mocked(settingsApi.updateLanguage).mockImplementation(() => new Promise((resolve) => { resolveSave = resolve; }));
    const { wrapper } = await mountShell();
    const button = wrapper.get('button[aria-label="Language"]');

    await button.trigger('click');

    expect(settingsApi.updateLanguage).toHaveBeenCalledWith('zh-CN');
    expect(useI18n().locale.value).toBe('zh-CN');
    expect(button.attributes('disabled')).toBeDefined();
    expect(button.attributes('aria-busy')).toBe('true');
    expect(button.attributes('aria-label')).toBe('正在保存语言');

    resolveSave(runtimeSettings('zh-CN'));
    await flushPromises();

    expect(button.attributes('disabled')).toBeUndefined();
    expect(button.attributes('aria-busy')).toBeUndefined();
    expect(localStorage.getItem('panel.locale')).toBe('zh-CN');
  });

  it('rolls back the language and reports an error when persistence fails', async () => {
    vi.mocked(settingsApi.updateLanguage).mockRejectedValue(new Error('offline'));
    const { wrapper } = await mountShell();
    const button = wrapper.get('button[aria-label="Language"]');

    await button.trigger('click');
    await flushPromises();

    expect(useI18n().locale.value).toBe('en');
    expect(document.documentElement.lang).toBe('en');
    expect(localStorage.getItem('panel.locale')).toBe('en');
    expect(toastPush).toHaveBeenCalledWith(expect.objectContaining({
      title: 'Unable to save language. Your previous language has been restored.',
      tone: 'danger',
    }));
  });

  it('shows immediate target feedback while a route is still resolving', async () => {
    const { wrapper } = await mountShell();

    beginRouteNavigation('/servers', 'routes.servers.title');
    await flushPromises();

    const pendingLink = wrapper.get('aside nav a[href="/servers"]');
    expect(pendingLink.attributes('aria-busy')).toBe('true');
    expect(pendingLink.classes()).toContain('text-brand');
    expect(wrapper.get('header h1').text()).toBe('Opening Servers…');
    expect(wrapper.get('header [role="progressbar"]').attributes('aria-label')).toBe('Opening Servers…');

    finishRouteNavigation();
    await flushPromises();

    expect(pendingLink.attributes('aria-busy')).toBeUndefined();
    expect(wrapper.find('header [role="progressbar"]').exists()).toBe(false);
  });

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
