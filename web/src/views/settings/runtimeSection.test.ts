// @vitest-environment jsdom
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils';
import { createPinia } from 'pinia';
import { createMemoryHistory, createRouter, type Router } from 'vue-router';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import SettingsView from './index.vue';
import Select from '@/components/ui/Select.vue';
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

function runtimeSettings(overrides: Partial<RuntimeSettings> = {}): RuntimeSettings {
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
    ...overrides,
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

/**
 * 只 fake 计时器与 Date，保留 setImmediate（@vue/test-utils 的 flushPromises 依赖它）。
 */
function enableAutoSaveTimers() {
  vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout', 'setInterval', 'clearInterval', 'Date'] });
}

async function advanceAutoSave(ms = 500) {
  await vi.advanceTimersByTimeAsync(ms);
  await flushPromises();
}

async function render(path = '/settings/general') {
  router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/settings/:section?', name: 'settings', component: SettingsView }] });
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
  vi.spyOn(settingsApi, 'updateGeneral').mockImplementation(async () => runtimeSettings());
  vi.spyOn(settingsApi, 'updateSecurity').mockImplementation(async () => runtimeSettings());
  vi.spyOn(settingsApi, 'updateCertificates').mockImplementation(async () => runtimeSettings());
  vi.spyOn(settingsApi, 'updateAgent').mockImplementation(async () => runtimeSettings());
  vi.spyOn(settingsApi, 'updateBranding').mockImplementation(async () => runtimeSettings());
  vi.spyOn(settingsApi, 'updateTailscale').mockImplementation(async () => runtimeSettings());
  vi.spyOn(settingsApi, 'updateServerVariables').mockImplementation(async () => []);
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

describe('settings runtime section', () => {
  it('keeps only the container report interval as a numeric runtime setting', async () => {
    const view = await render();

    const numberInputs = view.findAll<HTMLInputElement>('[role="spinbutton"]');
    expect(numberInputs).toHaveLength(1);
    expect(numberInputs[0]!.element.value).toBe('30');
    expect(numberInputs[0]!.attributes('aria-valuemin')).toBe('1');
    // NumberBox 在窄屏占满行（width token 由页面 class 控制）。
    expect(numberInputs[0]!.element.parentElement?.classList.contains('w-full')).toBe(true);
    expect(view.text()).toContain('Container report interval seconds');
    // 指标保留/采集间隔/清理计划已移出运行时设置，由指标设施管理。
    expect(view.text()).not.toContain('Metrics retention days');
    expect(view.text()).not.toContain('Metrics collection interval seconds');
    expect(view.text()).not.toContain('Cleanup schedule');
  });

  it('no longer renders a save-section button and indicates idle autosave silently', async () => {
    const view = await render();

    expect(view.findAll('button').some((button) => button.text() === 'Save section')).toBe(false);
    expect(view.find('[data-autosave-status]').exists()).toBe(false);
  });

  it('autosaves the general partition after 500ms with only general fields', async () => {
    const view = await render();
    enableAutoSaveTimers();

    const interval = view.find<HTMLInputElement>('[role="spinbutton"]');
    await interval.setValue('45');
    await interval.trigger('change');
    await flushPromises();

    // 防抖期间不发请求，状态为 pending。
    expect(settingsApi.updateGeneral).not.toHaveBeenCalled();
    expect(view.find('[data-autosave-status]').attributes('data-autosave-state')).toBe('pending');

    await advanceAutoSave(499);
    expect(settingsApi.updateGeneral).not.toHaveBeenCalled();
    await advanceAutoSave(1);

    const payload = vi.mocked(settingsApi.updateGeneral).mock.calls[0][0];
    expect(payload).toEqual({
      containerReportIntervalSeconds: 45,
      language: 'en',
      logLevel: 'info',
      reconcileTraceEnabled: false,
    });
    // 已移除的指标字段不得重新出现在负载里。
    expect(payload).not.toHaveProperty('metricsRetentionDays');
    expect(payload).not.toHaveProperty('metricsCollectionIntervalSeconds');
    expect(payload).not.toHaveProperty('cleanupSchedule');
    // 自动保存成功不弹 success toast，只用状态指示。
    expect(toastPush).not.toHaveBeenCalledWith(expect.objectContaining({ tone: 'success' }));
    expect(view.find('[data-autosave-status]').attributes('data-autosave-state')).toBe('saved');
    expect(view.text()).toContain('All changes saved');
  });

  it('cancels the pending save while the user keeps typing and saves only after 500ms of quiet', async () => {
    const view = await render();
    enableAutoSaveTimers();

    const interval = view.find<HTMLInputElement>('[role="spinbutton"]');
    await interval.setValue('45');
    await interval.trigger('change');
    await flushPromises();

    // 未满 500ms 继续输入：必须取消上一次待保存动作并重新计时。
    await advanceAutoSave(300);
    await interval.setValue('50');
    await interval.trigger('change');
    await flushPromises();

    await advanceAutoSave(499);
    expect(settingsApi.updateGeneral).not.toHaveBeenCalled();

    await advanceAutoSave(1);
    expect(settingsApi.updateGeneral).toHaveBeenCalledTimes(1);
    expect(vi.mocked(settingsApi.updateGeneral).mock.calls[0][0]).toEqual({
      containerReportIntervalSeconds: 50,
      language: 'en',
      logLevel: 'info',
      reconcileTraceEnabled: false,
    });
  });

  it('blocks invalid numeric input locally without sending a request', async () => {
    const view = await render();
    enableAutoSaveTimers();

    const interval = view.find<HTMLInputElement>('[role="spinbutton"]');
    await interval.setValue('');
    await interval.trigger('change');
    await advanceAutoSave();

    expect(settingsApi.updateGeneral).not.toHaveBeenCalled();
    expect(view.find('[data-autosave-status]').attributes('data-autosave-state')).toBe('invalid');
    expect(view.findAll('[data-settings-card]')[0]!.attributes('aria-invalid')).toBe('true');
    expect(view.text()).toContain(useI18n().t('settingsPage.validationPositiveNumber'));
  });

  it('switches the UI language immediately, then saves it through the general partition', async () => {
    const view = await render();
    enableAutoSaveTimers();
    const { locale } = useI18n();
    expect(locale.value).toBe('en');

    const languageSelect = view.findAllComponents(Select)[0]!;
    languageSelect.vm.$emit('update:modelValue', 'zh-CN');
    await flushPromises();

    // 立即切换界面语言，不等保存。
    expect(locale.value).toBe('zh-CN');
    expect(settingsApi.updateGeneral).not.toHaveBeenCalled();

    await advanceAutoSave();
    expect(vi.mocked(settingsApi.updateGeneral).mock.calls[0][0]).toMatchObject({ language: 'zh-CN' });
  });

  it('rolls the UI language back to the persisted value when the language save fails', async () => {
    const view = await render();
    enableAutoSaveTimers();
    const { locale } = useI18n();
    vi.mocked(settingsApi.updateGeneral).mockRejectedValueOnce(new Error('language save failed'));

    const languageSelect = view.findAllComponents(Select)[0]!;
    languageSelect.vm.$emit('update:modelValue', 'zh-CN');
    await flushPromises();
    expect(locale.value).toBe('zh-CN');

    await advanceAutoSave();

    expect(locale.value).toBe('en');
    expect(view.find('[data-autosave-status]').attributes('data-autosave-state')).toBe('error');
    expect(toastPush).toHaveBeenCalledWith(expect.objectContaining({ tone: 'danger' }));
  });

  it('shows a danger toast on failure and retries with the same payload', async () => {
    const view = await render();
    enableAutoSaveTimers();
    vi.mocked(settingsApi.updateGeneral)
      .mockRejectedValueOnce(new Error('conflict'))
      .mockResolvedValueOnce(runtimeSettings({ containerReportIntervalSeconds: 45 }));

    const interval = view.find<HTMLInputElement>('[role="spinbutton"]');
    await interval.setValue('45');
    await interval.trigger('change');
    await advanceAutoSave();

    expect(toastPush).toHaveBeenCalledWith(expect.objectContaining({ tone: 'danger', title: 'conflict' }));
    const status = view.find('[data-autosave-status]');
    expect(status.attributes('data-autosave-state')).toBe('error');
    // 表单保留用户输入。
    expect(view.find<HTMLInputElement>('[role="spinbutton"]').element.value).toBe('45');

    await status.find('[data-autosave-retry]').trigger('click');
    await advanceAutoSave(0);

    expect(vi.mocked(settingsApi.updateGeneral)).toHaveBeenCalledTimes(2);
    expect(vi.mocked(settingsApi.updateGeneral).mock.calls[1][0]).toMatchObject({ containerReportIntervalSeconds: 45 });
    expect(view.find('[data-autosave-status]').attributes('data-autosave-state')).toBe('saved');
  });

  it('keeps a pending auto-save alive when switching tabs before the debounce fires', async () => {
    const view = await render('/settings/general');
    enableAutoSaveTimers();

    const interval = view.find<HTMLInputElement>('[role="spinbutton"]');
    await interval.setValue('45');
    await interval.trigger('change');
    expect(settingsApi.updateGeneral).not.toHaveBeenCalled();

    const securityTab = view.findAll('[role="tab"]').find((item) => item.text() === useI18n().t('settingsPage.section.security'))!;
    await securityTab.trigger('click');
    await flushPromises();
    expect(router.currentRoute.value.path).toBe('/settings/security');

    // 切 tab 不卸载页面，待发自动保存继续完成。
    await advanceAutoSave();
    expect(vi.mocked(settingsApi.updateGeneral).mock.calls[0][0]).toMatchObject({ containerReportIntervalSeconds: 45 });
  });

  it('cancels the pending timer and reloads on refresh', async () => {
    const view = await render();
    enableAutoSaveTimers();

    const interval = view.find<HTMLInputElement>('[role="spinbutton"]');
    await interval.setValue('45');
    await interval.trigger('change');
    vi.mocked(settingsApi.runtime).mockClear();

    const refresh = view.findAll('button').find((button) => button.text() === useI18n().t('common.refresh'))!;
    await refresh.trigger('click');
    await flushPromises();
    await advanceAutoSave(500);

    expect(settingsApi.runtime).toHaveBeenCalledTimes(1);
    expect(settingsApi.updateGeneral).not.toHaveBeenCalled();
    expect(view.find<HTMLInputElement>('[role="spinbutton"]').element.value).toBe('30');
  });
});
