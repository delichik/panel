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

/** 只 fake 计时器与 Date，保留 setImmediate（flushPromises 依赖）。 */
function enableAutoSaveTimers() {
  vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout', 'setInterval', 'clearInterval', 'Date'] });
}

async function advanceAutoSave(ms = 500) {
  await vi.advanceTimersByTimeAsync(ms);
  await flushPromises();
}

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

function tagsInput() {
  return wrapper!.find('input[placeholder="tag:server, tag:panel"]');
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
  vi.spyOn(settingsApi, 'updateGeneral').mockImplementation(async () => runtimeSettings());
  vi.spyOn(settingsApi, 'updateSecurity').mockImplementation(async () => runtimeSettings());
  vi.spyOn(settingsApi, 'updateCertificates').mockImplementation(async () => runtimeSettings());
  vi.spyOn(settingsApi, 'updateAgent').mockImplementation(async () => runtimeSettings());
  vi.spyOn(settingsApi, 'updateBranding').mockImplementation(async () => runtimeSettings());
  vi.spyOn(settingsApi, 'updateTailscale').mockImplementation(async () => runtimeSettings());
  vi.spyOn(settingsApi, 'updateServerVariables').mockImplementation(async () => []);
  vi.spyOn(settingsApi, 'applyTailscale').mockResolvedValue(container);
  vi.spyOn(systemApi, 'version').mockResolvedValue({ version: '0.2.0-dev', channel: 'dev', latestVersion: '', updateAvailable: false });
  vi.spyOn(keyAssetsApi, 'systemCertificates').mockResolvedValue([]);
  vi.spyOn(keyAssetsApi, 'tlsCertificates').mockResolvedValue([]);
});

afterEach(() => {
  vi.useRealTimers();
  wrapper?.unmount();
  wrapper = undefined;
  document.body.innerHTML = '';
  vi.restoreAllMocks();
});

describe('settings tailscale section', () => {
  it('is reachable from the settings tabs without a second sidebar', async () => {
    await render();
    // 设置页内部不再有第二条左侧分区导航，只有顶部共享 Tabs。
    expect(wrapper!.find('aside').exists()).toBe(false);
    const tabs = wrapper!.findAll('[role="tab"]');
    expect(tabs).toHaveLength(9);
    const tailscaleTab = tabs.find((item) => item.text() === 'Tailscale');
    expect(tailscaleTab).toBeTruthy();
    expect(tailscaleTab!.attributes('aria-selected')).toBe('true');
  });

  it('never hydrates the stored auth key from the server response', async () => {
    await render();
    expect((authKeyInput().element as HTMLInputElement).value).toBe('');
    expect(wrapper!.text()).toContain('Auth key configured');
  });

  it('autosaves a new auth key with the parsed tags after 500ms and clears the write-only input', async () => {
    vi.mocked(settingsApi.updateTailscale).mockResolvedValue(runtimeSettings({ tags: ['tag:server', 'tag:edge-1'] }));
    await render();
    enableAutoSaveTimers();

    await authKeyInput().setValue('tskey-auth-new');
    await tagsInput().setValue('tag:server, tag:edge-1');
    await flushPromises();

    expect(settingsApi.updateTailscale).not.toHaveBeenCalled();
    await advanceAutoSave();

    expect(vi.mocked(settingsApi.updateTailscale).mock.calls[0][0]).toEqual({
      tags: ['tag:server', 'tag:edge-1'],
      authKey: 'tskey-auth-new',
    });
    // 保存成功后清空只写密钥输入，并保持已配置状态。
    expect((authKeyInput().element as HTMLInputElement).value).toBe('');
    expect(wrapper!.text()).toContain('Auth key configured');
    expect(wrapper!.find('[data-autosave-status]').attributes('data-autosave-state')).toBe('saved');
  });

  it('blocks autosave and reports the offending entries when a tag is invalid', async () => {
    await render();
    enableAutoSaveTimers();

    await tagsInput().setValue('Tag:Panel');
    await advanceAutoSave();

    expect(settingsApi.updateTailscale).not.toHaveBeenCalled();
    expect(wrapper!.text()).toContain('Invalid ACL tags: Tag:Panel');
    expect(wrapper!.find('[data-autosave-status]').attributes('data-autosave-state')).toBe('invalid');
  });

  it('blocks autosave when the auth key does not look like a Tailscale key', async () => {
    await render();
    enableAutoSaveTimers();

    await authKeyInput().setValue('not-a-tskey');
    await advanceAutoSave();

    expect(settingsApi.updateTailscale).not.toHaveBeenCalled();
    expect(wrapper!.text()).toContain('Enter a Tailscale auth key');
    expect(wrapper!.find('[data-autosave-status]').attributes('data-autosave-state')).toBe('invalid');
  });

  it('sends clearAuthKey only after the danger confirm is accepted', async () => {
    await render();

    await sectionButton('Clear auth key').trigger('click');
    await flushPromises();

    // 对话框已打开但尚未确认：此时不得发出任何写请求。
    expect(settingsApi.updateTailscale).not.toHaveBeenCalled();

    const dialog = document.querySelector('[role="dialog"]');
    expect(dialog).not.toBeNull();
    const checkbox = dialog!.querySelector<HTMLInputElement>('input[type="checkbox"]');
    expect(checkbox).not.toBeNull();
    checkbox!.click();
    await flushPromises();

    confirmDialogButton('Apply').click();
    await flushPromises();

    const payload = vi.mocked(settingsApi.updateTailscale).mock.calls[0][0];
    expect(payload).toEqual({ clearAuthKey: true });
    expect(payload).not.toHaveProperty('authKey');
    expect(payload).not.toHaveProperty('tags');
    expect(toastPush).toHaveBeenCalledWith(expect.objectContaining({ tone: 'success' }));
  });

  it('drops a pending tag autosave after clearing the auth key', async () => {
    await render();
    enableAutoSaveTimers();

    await tagsInput().setValue('tag:server, tag:panel');
    await flushPromises();
    expect(settingsApi.updateTailscale).not.toHaveBeenCalled();

    // 取消待发自动保存，清除密钥请求只带 clearAuthKey。
    vi.mocked(settingsApi.updateTailscale).mockClear();
    await sectionButton('Clear auth key').trigger('click');
    await flushPromises();
    const dialog = document.querySelector('[role="dialog"]');
    const checkbox = dialog!.querySelector<HTMLInputElement>('input[type="checkbox"]');
    checkbox!.click();
    await flushPromises();
    confirmDialogButton('Apply').click();
    await flushPromises();
    await advanceAutoSave();

    const payloads = vi.mocked(settingsApi.updateTailscale).mock.calls.map((call) => call[0]);
    expect(payloads).toHaveLength(1);
    expect(payloads[0]).toEqual({ clearAuthKey: true });
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
