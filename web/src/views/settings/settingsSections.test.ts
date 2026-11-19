// @vitest-environment jsdom
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils';
import { createPinia } from 'pinia';
import { createMemoryHistory, createRouter, type Router } from 'vue-router';
import { nextTick } from 'vue';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import SettingsView from './index.vue';
import SettingsCard from '@/components/patterns/SettingsCard.vue';
import SettingsSection from '@/components/patterns/SettingsSection.vue';
import Select from '@/components/ui/Select.vue';
import { keyAssetsApi } from '@/api/keyAssets';
import { settingsApi } from '@/api/settings';
import { systemApi } from '@/api/system';
import { toastKey } from '@/components/ui/toast';
import { useI18n } from '@/i18n';
import type { RuntimeSettings } from '@/types/settings';

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
    autoUpdate: {
      appsEnabled: true,
      packagesEnabled: false,
      packagesMode: 'security',
      scheduleTime: '04:00',
      lastRunAt: '2026-08-01T00:00:00.000Z',
      lastRunStatus: 'completed',
      lastRunTaskId: 'task-auto-update-1',
    },
    imageRegistry: { endpoint: '', scheme: 'https' },
    jwtSecretConfigured: true,
  };
}

const toastPush = vi.fn();
let wrapper: VueWrapper | undefined;
let router: Router;

// jsdom in this vitest environment has no localStorage; stub it like the other
// settings/shell tests so i18n and the session store can read their keys.
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

async function render(path: string) {
  router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/settings/:section?', name: 'settings', component: SettingsView }],
  });
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
  vi.spyOn(settingsApi, 'updateAutoUpdate').mockImplementation(async () => runtimeSettings());
  vi.spyOn(settingsApi, 'updateImageRegistry').mockImplementation(async () => runtimeSettings());
  vi.spyOn(settingsApi, 'runAutoUpdate').mockResolvedValue({ taskId: 'task-auto-update-manual-1' });
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

describe('WinUI settings section primitives', () => {
  it('renders a settings section as a title/description header plus one surface card with divider rows', () => {
    const view = mount(SettingsSection, {
      props: { title: 'Section title', description: 'Section description' },
      slots: {
        default: '<div data-row>Row A</div><div data-row>Row B</div>',
        actions: '<button type="button">Section action</button>',
      },
    });

    expect(view.find('[data-settings-section]').exists()).toBe(true);
    expect(view.find('h2').text()).toBe('Section title');
    expect(view.text()).toContain('Section description');
    expect(view.find('.surface-card').exists()).toBe(true);
    expect(view.find('.divide-y').exists()).toBe(true);
    expect(view.findAll('[data-row]')).toHaveLength(2);
    expect(view.findAll('button')).toHaveLength(1);
    // 分区容器内不出现第二层卡片。
    expect(view.find('.surface-card .surface-card').exists()).toBe(false);
  });

  it('omits the section header when no title/description/actions are provided', () => {
    const view = mount(SettingsSection, {
      props: { description: 'Only context copy' },
      slots: { default: '<div data-row>Row A</div>' },
    });

    expect(view.find('h2').exists()).toBe(false);
    expect(view.text()).toContain('Only context copy');
    expect(view.find('[data-row]').exists()).toBe(true);

    const bare = mount(SettingsSection, { slots: { default: '<div data-row>Row B</div>' } });
    // 无 header 时不渲染标题行（卡片行仍存在）。
    expect(bare.find('h2').exists()).toBe(false);
    expect(bare.text()).not.toContain('Section');
  });

  it('renders a SettingsCard row with title, description, control, hint and full-width content', () => {
    const view = mount(SettingsCard, {
      props: { title: 'Row title', description: 'Row description' },
      slots: {
        control: '<button type="button">Row control</button>',
        hint: '<span data-hint>Row hint</span>',
        default: '<p data-body>Row body</p>',
      },
    });

    const card = view.find('[data-settings-card]');
    expect(card.exists()).toBe(true);
    expect(view.text()).toContain('Row title');
    expect(view.text()).toContain('Row description');
    expect(view.find('button').text()).toBe('Row control');
    expect(view.find('[data-hint]').text()).toBe('Row hint');
    expect(view.find('[data-body]').text()).toBe('Row body');
    expect(card.attributes('aria-disabled')).toBeUndefined();
    expect(card.attributes('aria-invalid')).toBeUndefined();
  });

  it('marks SettingsCard disabled and invalid states for semantics and visual styling', () => {
    const disabled = mount(SettingsCard, { props: { title: 'Disabled row', disabled: true } });
    expect(disabled.find('[data-settings-card]').classes()).toContain('opacity-45');
    expect(disabled.find('[data-settings-card]').attributes('aria-disabled')).toBe('true');

    const invalid = mount(SettingsCard, {
      props: { title: 'Invalid row', invalid: true },
      slots: { hint: '<span class="text-danger">Row error</span>' },
    });
    expect(invalid.find('[data-settings-card]').attributes('aria-invalid')).toBe('true');
    expect(invalid.find('[data-settings-card]').text()).toContain('Row error');
    expect(invalid.find('p').classes()).toContain('text-danger');
  });
});

describe('settings page sections use the shared WinUI SettingsCard shell', () => {
  const cases: Array<{ path: string; heading: string; rows: number }> = [
    { path: '/settings/general', heading: 'settingsPage.section.runtime', rows: 5 },
    { path: '/settings/security', heading: 'settingsPage.section.security', rows: 5 },
    { path: '/settings/certificates', heading: 'settingsPage.section.certificates', rows: 4 },
    { path: '/settings/agent', heading: 'settingsPage.section.agent', rows: 3 },
    { path: '/settings/auto-update', heading: 'settingsPage.section.autoUpdate', rows: 5 },
    { path: '/settings/system-certificates', heading: 'settingsPage.section.systemCertificates', rows: 0 },
    { path: '/settings/system', heading: 'settingsPage.section.system', rows: 4 },
    { path: '/settings/image-registry', heading: 'settingsPage.section.imageRegistry', rows: 2 },
  ];

  it('renders every non-appearance section with a heading and a single surface card of rows', async () => {
    const { t } = useI18n();
    for (const testCase of cases) {
      const view = await render(testCase.path);
      const section = view.find('[data-settings-section]');
      expect(section.exists(), `${testCase.path} settings section`).toBe(true);
      // 顶层分区标题由 PageHeader 承担，SettingsSection 不再重复标题。
      expect(view.find('h1').text()).toBe(t(testCase.heading));
      expect(view.find('[data-width]').attributes('data-width')).toBe('content');
      expect(section.find('h2').exists()).toBe(false);
      expect(section.find('.surface-card').exists()).toBe(true);
      expect(section.find('.surface-card .surface-card').exists()).toBe(false);
      expect(section.findAll('[data-settings-card]'), `${testCase.path} card rows`).toHaveLength(testCase.rows);

      // 自动保存替代了分区保存按钮：不再有任何保存分区/品牌/变量入口。
      const saveButtons = view.findAll('button').filter((item) => (
        item.text() === 'Save section' || item.text() === 'Save branding' || item.text() === 'Save variables'
      ));
      expect(saveButtons, `${testCase.path} save actions`).toHaveLength(0);
      view.unmount();
      wrapper = undefined;
    }
  });

  it('renders backup export and restore as two settings sections with card rows', async () => {
    const { t } = useI18n();
    const view = await render('/settings/backups');

    const sections = view.findAll('[data-settings-section]');
    expect(sections).toHaveLength(2);
    expect(sections[0]!.find('h2').text()).toBe(t('settingsPage.backupExport'));
    expect(sections[1]!.find('h2').text()).toBe(t('settingsPage.restore'));
    expect(view.findAll('[data-settings-card]')).toHaveLength(5);

    const restoreButton = view.findAll('button').find((item) => item.text() === t('settingsPage.confirmRestore'));
    expect(restoreButton).toBeTruthy();
    expect(restoreButton!.attributes('disabled')).toBeDefined();
    // 移动端分区 footer 按钮 full width。
    const exportButton = view.findAll('button').find((item) => item.text() === t('settingsPage.startExport'));
    expect(exportButton?.classes()).toContain('max-sm:w-full');
    expect(restoreButton!.classes()).toContain('max-sm:w-full');
  });

  it('keeps the danger confirmation wired from the runtime card row', async () => {
    const { t } = useI18n();
    const view = await render('/settings/general');

    const clearButton = view.findAll('button').find((item) => item.text() === t('settingsPage.clearEventsLogs'));
    expect(clearButton).toBeTruthy();
    await clearButton!.trigger('click');
    await flushPromises();

    expect(document.querySelector('[role="dialog"]')).not.toBeNull();
  });

  it('keeps field hints and validation errors inside the row control column', async () => {
    const view = await render('/settings/general');
    const cards = view.findAll('[data-settings-card]');
    // 默认合法值下不显示错误 hint。
    expect(cards[0]!.text()).not.toContain(useI18n().t('settingsPage.validationPositiveNumber'));

    const interval = view.find('[role="spinbutton"]');
    await interval.setValue('');
    await interval.trigger('change');
    await flushPromises();

    const invalidCard = view.findAll('[data-settings-card]')[0]!;
    expect(invalidCard.attributes('aria-invalid')).toBe('true');
    expect(invalidCard.text()).toContain(useI18n().t('settingsPage.validationPositiveNumber'));
  });
});

describe('settings partition auto-save', () => {
  it('autosaves only the security partition after editing the remote timeout', async () => {
    const view = await render('/settings/security');
    enableAutoSaveTimers();

    const timeout = view.find<HTMLInputElement>('[role="spinbutton"]');
    await timeout.setValue('90');
    await timeout.trigger('change');
    await advanceAutoSave();

    expect(vi.mocked(settingsApi.updateSecurity).mock.calls[0][0]).toEqual({
      tokenExpiration: '1d',
      remoteCommandTimeoutSeconds: 90,
    });
    expect(settingsApi.updateGeneral).not.toHaveBeenCalled();
    expect(view.find('[data-autosave-status]').attributes('data-autosave-state')).toBe('saved');
  });

  it('autosaves the certificates partition with a normalized panel domain', async () => {
    const view = await render('/settings/certificates');
    enableAutoSaveTimers();

    await view.find('input[aria-label="Panel domain"]').setValue('Panel.Example.COM');
    await advanceAutoSave();

    expect(vi.mocked(settingsApi.updateCertificates).mock.calls[0][0]).toEqual({
      panel: { domain: 'panel.example.com', tlsCertificateId: '' },
      certificates: { email: '', dnsPropagationDelaySeconds: 30 },
    });
    expect(settingsApi.updateAgent).not.toHaveBeenCalled();
  });

  it('normalizes the agent download origin and blocks a URL with a path locally', async () => {
    const view = await render('/settings/agent');
    enableAutoSaveTimers();

    const baseUrl = view.find('input[aria-label="Agent download base URL"]');
    await baseUrl.setValue('https://cdn.example.com/');
    await advanceAutoSave();
    expect(vi.mocked(settingsApi.updateAgent).mock.calls[0][0]).toEqual({
      downloadBaseUrl: 'https://cdn.example.com',
      downloadVerifyTls: false,
      transferTimeoutSeconds: 120,
    });

    vi.mocked(settingsApi.updateAgent).mockClear();
    await baseUrl.setValue('https://cdn.example.com/dist');
    await advanceAutoSave();
    expect(settingsApi.updateAgent).not.toHaveBeenCalled();
    expect(view.find('[data-autosave-status]').attributes('data-autosave-state')).toBe('invalid');
    expect(view.text()).toContain(useI18n().t('settingsPage.validationAgentDownloadBaseUrl'));
  });

  it('autosaves branding and server variables independently in the system section', async () => {
    const view = await render('/settings/system');
    enableAutoSaveTimers();

    await view.find('input[aria-label="Login title"]').setValue('Ops');
    await advanceAutoSave();
    expect(vi.mocked(settingsApi.updateBranding).mock.calls[0][0]).toEqual({ loginTitle: 'Ops', loginSubtitle: '' });
    expect(settingsApi.updateServerVariables).not.toHaveBeenCalled();

    await view.find('textarea').setValue('*PUBLIC_ADDRESS=Public address\nREGION_CODE=Region code');
    await advanceAutoSave();
    expect(vi.mocked(settingsApi.updateServerVariables).mock.calls[0][0]).toEqual([
      { key: 'PUBLIC_ADDRESS', name: 'Public address', required: true },
      { key: 'REGION_CODE', name: 'Region code', required: false },
    ]);
    expect(settingsApi.updateBranding).toHaveBeenCalledTimes(1);
  });

  it('blocks malformed server variable lines locally and explains the expected format', async () => {
    const view = await render('/settings/system');
    enableAutoSaveTimers();

    await view.find('textarea').setValue('PUBLIC ADDRESS=Public address');
    await advanceAutoSave();

    expect(settingsApi.updateServerVariables).not.toHaveBeenCalled();
    expect(view.find('[data-autosave-status]').attributes('data-autosave-state')).toBe('invalid');
    expect(view.text()).toContain(useI18n().t('settingsPage.serverVariablesInvalid'));
  });
});

describe('settings auto-update partition', () => {
  it('autosaves only the four editable auto-update fields after toggling application updates', async () => {
    const view = await render('/settings/auto-update');
    enableAutoSaveTimers();

    const appSwitch = view.findAll('[role="switch"]')[0]!;
    expect(appSwitch.attributes('aria-checked')).toBe('true');
    await appSwitch.trigger('click');
    await advanceAutoSave();

    expect(vi.mocked(settingsApi.updateAutoUpdate).mock.calls[0][0]).toEqual({
      appsEnabled: false,
      packagesEnabled: false,
      packagesMode: 'security',
      scheduleTime: '04:00',
    });
    expect(settingsApi.updateGeneral).not.toHaveBeenCalled();
    // 只读的 lastRun 投影不得进入自动保存载荷。
    expect(vi.mocked(settingsApi.updateAutoUpdate).mock.calls[0][0]).not.toHaveProperty('lastRunAt');
    expect(view.find('[data-autosave-status]').attributes('data-autosave-state')).toBe('saved');
  });

  it('blocks an invalid HH:MM schedule time locally without sending a request', async () => {
    const { t } = useI18n();
    const view = await render('/settings/auto-update');
    enableAutoSaveTimers();

    await view.find('input[aria-label="Hour"]').setValue('25');
    await advanceAutoSave();

    expect(settingsApi.updateAutoUpdate).not.toHaveBeenCalled();
    expect(view.find('[data-autosave-status]').attributes('data-autosave-state')).toBe('invalid');
    expect(view.text()).toContain(t('settingsPage.validationAutoUpdateScheduleTime'));
  });

  it('runs the coordinator only after explicit confirmation without changing autosave fields', async () => {
    const { t } = useI18n();
    const view = await render('/settings/auto-update');
    const runButton = view.findAll('button').find((item) => item.text() === t('settingsPage.autoUpdate.runNow'));
    expect(runButton).toBeTruthy();
    expect(runButton!.attributes('disabled')).toBeUndefined();

    await runButton!.trigger('click');
    await flushPromises();
    expect(settingsApi.runAutoUpdate).not.toHaveBeenCalled();
    expect(document.querySelector('[role="dialog"]')).not.toBeNull();

    const confirmButton = Array.from(document.querySelectorAll<HTMLButtonElement>('[role="dialog"] button'))
      .find((button) => button.textContent?.trim() === t('settingsPage.autoUpdate.runNow'));
    expect(confirmButton).toBeTruthy();
    confirmButton!.click();
    await flushPromises();

    expect(settingsApi.runAutoUpdate).toHaveBeenCalledTimes(1);
    expect(settingsApi.updateAutoUpdate).not.toHaveBeenCalled();
    expect(toastPush).toHaveBeenCalledWith(expect.objectContaining({
      title: expect.stringContaining('task-auto-update-manual-1'),
      tone: 'success',
    }));
  });

  it('disables the manual run button when both switches are off', async () => {
    const { t } = useI18n();
    vi.mocked(settingsApi.runtime).mockResolvedValue({
      ...runtimeSettings(),
      autoUpdate: { appsEnabled: false, packagesEnabled: false, packagesMode: 'security', scheduleTime: '04:00' },
    });
    const view = await render('/settings/auto-update');
    const runButton = view.findAll('button').find((item) => item.text() === t('settingsPage.autoUpdate.runNow'));
    expect(runButton).toBeTruthy();
    expect(runButton!.attributes('disabled')).toBeDefined();
  });

  it('disables the manual run button while a run is already active', async () => {
    const { t } = useI18n();
    vi.mocked(settingsApi.runtime).mockResolvedValue({
      ...runtimeSettings(),
      autoUpdate: {
        appsEnabled: true,
        packagesEnabled: false,
        packagesMode: 'security',
        scheduleTime: '04:00',
        lastRunAt: '2026-08-01T04:00:00.000Z',
        lastRunStatus: 'running',
        lastRunTaskId: 'task-auto-update-active',
      },
    });
    const view = await render('/settings/auto-update');
    const runButton = view.findAll('button').find((item) => item.text() === t('settingsPage.autoUpdate.runNow'));
    expect(runButton).toBeTruthy();
    expect(runButton!.attributes('disabled')).toBeDefined();
  });
});

describe('settings image registry partition', () => {
  it('backfills the registry endpoint and scheme from the runtime GET payload', async () => {
    const { t } = useI18n();
    vi.mocked(settingsApi.runtime).mockResolvedValue({
      ...runtimeSettings(),
      imageRegistry: { endpoint: 'registry.example.com:5000', scheme: 'http' },
    });
    const view = await render('/settings/image-registry');

    const endpoint = view.find<HTMLInputElement>(`input[aria-label="${t('settingsPage.imageRegistry.endpoint')}"]`);
    expect(endpoint.element.value).toBe('registry.example.com:5000');
    expect(view.findComponent(Select).props('modelValue')).toBe('http');
  });

  it('autosaves only the image registry payload after normalizing a bare host:port', async () => {
    const { t } = useI18n();
    const view = await render('/settings/image-registry');
    enableAutoSaveTimers();

    const endpoint = view.find<HTMLInputElement>(`input[aria-label="${t('settingsPage.imageRegistry.endpoint')}"]`);
    await endpoint.setValue(' Registry.Example.COM:5000 ');
    view.findComponent(Select).vm.$emit('update:modelValue', 'http');
    await advanceAutoSave();

    expect(vi.mocked(settingsApi.updateImageRegistry).mock.calls[0]![0]).toEqual({
      endpoint: 'registry.example.com:5000',
      scheme: 'http',
    });
    expect(settingsApi.updateGeneral).not.toHaveBeenCalled();
    expect(view.find('[data-autosave-status]').attributes('data-autosave-state')).toBe('saved');
  });

  it('blocks an endpoint with a scheme, path, query, userinfo, whitespace, quote, backslash or invalid port locally', async () => {
    const { t } = useI18n();
    const view = await render('/settings/image-registry');
    enableAutoSaveTimers();

    const endpoint = view.find<HTMLInputElement>(`input[aria-label="${t('settingsPage.imageRegistry.endpoint')}"]`);
    const invalidEndpoints = [
      'https://registry.example.com',
      'registry.example.com/v2',
      'registry.example.com?x=1',
      'user@registry.example.com',
      'registry example.com',
      '"registry.example.com"',
      'registry\\example.com',
      'registry.example.com:0',
      'registry.example.com:65536',
    ];
    for (const invalid of invalidEndpoints) {
      await endpoint.setValue(invalid);
      await advanceAutoSave();
      expect(settingsApi.updateImageRegistry, invalid).not.toHaveBeenCalled();
      expect(view.find('[data-autosave-status]').attributes('data-autosave-state')).toBe('invalid');
    }
    expect(view.text()).toContain(t('settingsPage.validationImageRegistryEndpoint'));
  });
});

describe('settings page section navigation', () => {
  it('uses one horizontal WinUI tab strip instead of a second sidebar', async () => {
    const { t } = useI18n();
    const view = await render('/settings/security');

    // AppShell 的全局导航之外，设置页不再渲染自己的左侧 aside。
    expect(view.find('aside').exists()).toBe(false);
    const tablist = view.find('[role="tablist"]');
    expect(tablist.exists()).toBe(true);
    expect(tablist.classes()).toContain('overflow-x-auto');
    expect(tablist.classes()).toContain('overflow-y-hidden');

    const tabs = view.findAll('[role="tab"]');
    expect(tabs).toHaveLength(10);
    expect(tabs.map((tab) => tab.text())).toContain(t('settingsPage.section.imageRegistry'));
    // 镜像仓库是第 10 个分区。
    expect(tabs[9]!.text()).toBe(t('settingsPage.section.imageRegistry'));
    const selected = view.find('[role="tab"][aria-selected="true"]');
    expect(selected.text()).toBe(t('settingsPage.section.security'));
  });

  it('switches sections through the tab strip while keeping the URL authoritative', async () => {
    const { t } = useI18n();
    const view = await render('/settings/security');

    const generalTab = view.findAll('[role="tab"]').find((item) => item.text() === t('settingsPage.section.runtime'));
    expect(generalTab).toBeTruthy();
    await generalTab!.trigger('click');
    await flushPromises();

    expect(router.currentRoute.value.path).toBe('/settings/general');
    expect(view.find('[role="tab"][aria-selected="true"]').text()).toBe(t('settingsPage.section.runtime'));
    // 内容随 URL 切换，且不整页刷新。
    expect(view.find('[role="spinbutton"]').exists()).toBe(true);
    expect(view.find('[data-settings-section]').exists()).toBe(true);
  });

  it('restores the selected tab from a direct section URL', async () => {
    const { t } = useI18n();
    const view = await render('/settings/backups');

    expect(view.find('[role="tab"][aria-selected="true"]').text()).toBe(t('settingsPage.section.backups'));
    expect(view.findAll('[data-settings-section]')).toHaveLength(2);
  });

  it('keeps the tab header fixed while only the section content scrolls', async () => {
    const view = await render('/settings/general');

    const tablist = view.find('[role="tablist"]');
    const scroll = view.find('[data-settings-scroll]');
    expect(tablist.exists()).toBe(true);
    expect(scroll.exists()).toBe(true);
    expect(scroll.classes()).toContain('overflow-y-auto');
    // Tab header 在滚动容器之外，不随分区内容滚走；Tabs 根有高度约束。
    expect(scroll.find('[role="tablist"]').exists()).toBe(false);
    const tabsRoot = tablist.element.parentElement;
    expect(tabsRoot?.classList.contains('h-full')).toBe(true);
    expect(tabsRoot?.classList.contains('min-h-0')).toBe(true);
    // 移动端 Tabs 不挤压：tab 按钮 shrink-0 + whitespace-nowrap（横向滚动）。
    expect(tabsRoot?.className).toContain('[&_[role=tab]]:shrink-0');
    expect(tabsRoot?.className).toContain('[&_[role=tab]]:whitespace-nowrap');
  });

  it('resets the section scroll position when switching tabs', async () => {
    const { t } = useI18n();
    const view = await render('/settings/general');

    const scroll = view.find('[data-settings-scroll]').element as HTMLElement;
    scroll.scrollTop = 120;

    const securityTab = view.findAll('[role="tab"]').find((item) => item.text() === t('settingsPage.section.security'))!;
    await securityTab.trigger('click');
    await flushPromises();
    await nextTick();

    expect(scroll.scrollTop).toBe(0);
  });

  it('keeps the page mounted when switching sections so unsaved field values survive', async () => {
    const { t } = useI18n();
    const view = await render('/settings/general');

    const interval = view.find('[role="spinbutton"]');
    await interval.setValue('45');
    await interval.trigger('change');

    const backupsTab = view.findAll('[role="tab"]').find((item) => item.text() === t('settingsPage.section.backups'))!;
    await backupsTab.trigger('click');
    await flushPromises();
    expect(view.find('[role="spinbutton"]').exists()).toBe(false);

    const runtimeTab = view.findAll('[role="tab"]').find((item) => item.text() === t('settingsPage.section.runtime'))!;
    await runtimeTab.trigger('click');
    await flushPromises();

    // 同一页面实例：切走再切回不会重置未保存的输入。
    expect(view.find<HTMLInputElement>('[role="spinbutton"]').element.value).toBe('45');
  });

  it('redirects an invalid section to general instead of rendering another section', async () => {
    const { t } = useI18n();
    const view = await render('/settings/not-a-section');
    await flushPromises();

    expect(router.currentRoute.value.path).toBe('/settings/general');
    expect(view.find('[role="tab"][aria-selected="true"]').text()).toBe(t('settingsPage.section.runtime'));
    expect(view.findAll('[data-settings-section]')).toHaveLength(1);
  });

  it('refreshes the active section from the page header', async () => {
    const { t } = useI18n();
    const view = await render('/settings/general');
    vi.mocked(settingsApi.runtime).mockClear();

    const refresh = view.findAll('button').find((item) => item.text() === t('common.refresh'))!;
    await refresh.trigger('click');
    await flushPromises();

    expect(settingsApi.runtime).toHaveBeenCalledTimes(1);
  });

  it('renders the fixed tab header before section data finishes loading', async () => {
    let resolveRuntime: ((value: RuntimeSettings) => void) | undefined;
    vi.mocked(settingsApi.runtime).mockReturnValueOnce(new Promise((resolve) => {
      resolveRuntime = resolve;
    }));

    const view = await render('/settings/general');
    expect(view.find('[role="tablist"]').exists()).toBe(true);
    expect(view.find('[data-settings-scroll] [aria-busy="true"]').exists()).toBe(true);

    resolveRuntime?.(runtimeSettings());
    await flushPromises();
    expect(view.find('[role="spinbutton"]').exists()).toBe(true);
  });
});
