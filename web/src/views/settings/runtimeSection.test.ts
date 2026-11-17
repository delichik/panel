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
    tailscale: { authKeyConfigured: true, tags: ['tag:server'], container },
    jwtSecretConfigured: true,
  };
}

const toastPush = vi.fn();
let wrapper: VueWrapper | undefined;
let router: Router;

// jsdom in this vitest environment has no localStorage; stub it like AppShell's test.
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

async function render(path = '/settings/general') {
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

beforeEach(() => {
  toastPush.mockReset();
  useI18n().setLocale('en');
  vi.spyOn(settingsApi, 'runtime').mockResolvedValue(runtimeSettings());
  vi.spyOn(settingsApi, 'serverVariables').mockResolvedValue([]);
  vi.spyOn(settingsApi, 'updateRuntime').mockImplementation(async () => runtimeSettings());
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

describe('settings runtime section', () => {
  it('keeps only the container report interval as a numeric runtime setting', async () => {
    const view = await render();

    const numberInputs = view.findAll<HTMLInputElement>('input[type="number"]');
    expect(numberInputs).toHaveLength(1);
    expect(numberInputs[0]!.element.value).toBe('30');
    expect(view.text()).toContain('Container report interval seconds');
    // 指标保留/采集间隔/清理计划已移出运行时设置，由指标设施管理。
    expect(view.text()).not.toContain('Metrics retention days');
    expect(view.text()).not.toContain('Metrics collection interval seconds');
    expect(view.text()).not.toContain('Cleanup schedule');
  });

  it('saves the runtime section with the container interval but without removed metrics fields', async () => {
    const view = await render();
    const interval = view.find<HTMLInputElement>('input[type="number"]');
    interval.element.value = '45';
    await interval.trigger('input');
    await flushPromises();

    const save = view.findAll('button').find((button) => button.text() === 'Save section')!;
    await save.trigger('click');
    await flushPromises();

    const payload = vi.mocked(settingsApi.updateRuntime).mock.calls[0][0];
    expect(payload.containerReportIntervalSeconds).toBe(45);
    expect(payload).not.toHaveProperty('metricsRetentionDays');
    expect(payload).not.toHaveProperty('metricsCollectionIntervalSeconds');
    expect(payload).not.toHaveProperty('cleanupSchedule');
    expect(toastPush).toHaveBeenCalledWith(expect.objectContaining({ tone: 'success' }));
  });
});
