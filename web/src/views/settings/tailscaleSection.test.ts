// @vitest-environment jsdom
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils';
import { createPinia } from 'pinia';
import { createMemoryHistory, createRouter, type Router } from 'vue-router';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import SettingsView from './index.vue';
import { keyAssetsApi } from '@/api/keyAssets';
import { settingsApi } from '@/api/settings';
import { systemApi } from '@/api/system';
import { toastKey } from '@/components/ui/toast';
import { useI18n } from '@/i18n';
import type { RuntimeSettings, RuntimeTailscaleContainerState } from '@/types/settings';

const container: RuntimeTailscaleContainerState = {
  available: true,
  running: true,
  loggedIn: true,
  hostname: 'seamark-panel',
  ipv4: '100.101.102.103',
  ipv6: 'fd7a:115c:a1e0::1',
  version: '1.78.1',
  backendState: 'Running',
  lastError: '',
  updatedAt: '2026-08-01T07:55:00.000Z',
};

function runtimeSettings(tailscale: Partial<RuntimeSettings['tailscale']> = {}): RuntimeSettings {
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
    language: 'en',
    logLevel: 'info',
    remoteCommandTimeoutSeconds: 45,
    reconcileTraceEnabled: false,
    branding: { loginTitle: 'Seamark', loginSubtitle: '' },
    certificates: { email: '', dnsPropagationDelaySeconds: 30 },
    panel: { domain: 'localhost', tlsCertificateId: '' },
    agent: { downloadBaseUrl: '', downloadVerifyTls: false, transferTimeoutSeconds: 120 },
    tailscale: { authKeyConfigured: true, tags: ['tag:server'], container, ...tailscale },
    jwtSecretConfigured: true,
  };
}

const toastPush = vi.fn();
let wrapper: VueWrapper | undefined;
let router: Router;

// This vitest jsdom environment does not provide localStorage; stub it like
// AppShell's test does so the session store and i18n can read their keys.
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

async function render(path = '/settings/tailscale') {
  router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/settings/:section', component: SettingsView }] });
  await router.push(path);
  await router.isReady();
  wrapper = mount(SettingsView, {
    global: {
      plugins: [createPinia(), router],
      provide: { [toastKey as symbol]: { push: toastPush, remove: vi.fn() } },
    },
  });
  await flushPromises();
  return wrapper;
}

function sectionButton(text: string) {
  const button = wrapper!.findAll('button').find((item) => item.text() === text);
  expect(button, `button "${text}"`).toBeTruthy();
  return button!;
}

function authKeyInput() {
  return wrapper!.find('input[type="password"]');
}

function confirmDialogButton(text: string) {
  const dialog = document.querySelector('[role="dialog"]');
  expect(dialog).not.toBeNull();
  const button = Array.from(dialog!.querySelectorAll('button')).find((item) => item.textContent?.trim() === text);
  expect(button, `dialog button "${text}"`).toBeTruthy();
  return button!;
}

beforeEach(() => {
  toastPush.mockReset();
  useI18n().setLocale('en');
  vi.spyOn(settingsApi, 'runtime').mockResolvedValue(runtimeSettings());
  vi.spyOn(settingsApi, 'serverVariables').mockResolvedValue([]);
  vi.spyOn(settingsApi, 'updateRuntime').mockImplementation(async () => runtimeSettings());
  vi.spyOn(settingsApi, 'applyTailscale').mockResolvedValue(container);
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

describe('settings tailscale section', () => {
  it('is reachable from the settings sidebar', async () => {
    await render();
    const link = wrapper!.findAll('a').find((item) => item.text() === 'Tailscale');
    expect(link?.attributes('href')).toBe('/settings/tailscale');
    expect(link?.attributes('aria-current')).toBe('page');
  });

  it('never hydrates the stored auth key from the server response', async () => {
    await render();
    expect((authKeyInput().element as HTMLInputElement).value).toBe('');
    expect(wrapper!.text()).toContain('Auth key configured');
  });

  it('sends a new auth key with the parsed tags when the section is saved', async () => {
    await render();
    await authKeyInput().setValue('tskey-auth-new');
    await wrapper!.find('input[placeholder="tag:server, tag:panel"]').setValue('tag:server, tag:edge-1');
    await sectionButton('Save section').trigger('click');
    await flushPromises();

    const payload = vi.mocked(settingsApi.updateRuntime).mock.calls[0][0];
    expect(payload.tailscale).toEqual({ authKey: 'tskey-auth-new', tags: ['tag:server', 'tag:edge-1'] });
  });

  it('blocks saving and shows the offending entries when a tag is invalid', async () => {
    await render();
    await wrapper!.find('input[placeholder="tag:server, tag:panel"]').setValue('Tag:Panel');
    await flushPromises();

    expect(wrapper!.text()).toContain('Invalid ACL tags: Tag:Panel');
    expect(sectionButton('Save section').attributes('disabled')).toBeDefined();
  });

  it('sends clearAuthKey only after the danger confirm is accepted', async () => {
    await render();
    await sectionButton('Clear auth key').trigger('click');
    await flushPromises();

    // 对话框已打开但尚未确认：此时不得发出任何写请求。
    expect(settingsApi.updateRuntime).not.toHaveBeenCalled();

    confirmDialogButton('Apply').click();
    await flushPromises();

    const payload = vi.mocked(settingsApi.updateRuntime).mock.calls[0][0];
    expect(payload.tailscale).toEqual({ clearAuthKey: true });
    expect(payload.tailscale).not.toHaveProperty('authKey');
    expect(toastPush).toHaveBeenCalledWith(expect.objectContaining({ tone: 'success' }));
  });

  it('refreshes the reported container state after requesting reconciliation', async () => {
    await render();
    await sectionButton('Apply / reconnect').trigger('click');
    await flushPromises();

    expect(settingsApi.applyTailscale).toHaveBeenCalledTimes(1);
    expect(toastPush).toHaveBeenCalledWith(expect.objectContaining({ title: 'Tailscale reconciliation requested.' }));
  });

  it('states that the container cannot be managed instead of pretending it worked', async () => {
    vi.mocked(settingsApi.runtime).mockResolvedValue(runtimeSettings({ container: { ...container, available: false } }));
    await render();

    expect(wrapper!.text()).toContain('Container tailscale cannot be managed here');
    expect(sectionButton('Apply / reconnect').attributes('disabled')).toBeDefined();
  });
});
