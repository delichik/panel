// @vitest-environment jsdom
import { flushPromises, mount, enableAutoUnmount } from '@vue/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { createMemoryHistory, createRouter } from 'vue-router';
import Badge from '@/components/ui/Badge.vue';
import { toastKey } from '@/components/ui/toast';
import { useI18n } from '@/i18n';
import ServersPage from './index.vue';

enableAutoUnmount(afterEach);

const listPage = vi.fn();
const get = vi.fn();
const metrics = vi.fn();
const metricsBucketed = vi.fn();
const containers = vi.fn();
const list = vi.fn();

// jsdom 无 canvas：图表本体只验证挂载契约，具体选项由 metricsModel 单测覆盖。
vi.mock('@/components/patterns/MetricChart.vue', () => ({
  default: {
    name: 'MetricChart',
    props: ['series', 'valueKind', 'thresholds', 'label'],
    template: '<div data-testid="metric-chart" />',
  },
}));

vi.mock('@/api/servers', () => ({
  serversApi: {
    listPage: (...args: unknown[]) => listPage(...args),
    list: (...args: unknown[]) => list(...args),
    get: (...args: unknown[]) => get(...args),
    test: vi.fn(),
    delete: vi.fn(),
    metrics: (...args: unknown[]) => metrics(...args),
    metricsBucketed: (...args: unknown[]) => metricsBucketed(...args),
    networkProbes: vi.fn().mockResolvedValue({ serverId: 'srv-1', enabled: false, nodes: [], results: [], updatedAt: null }),
    deployAgent: vi.fn(),
    restart: vi.fn(),
    trustHostKey: vi.fn(),
    applyTailscale: vi.fn(),
  },
}));
vi.mock('@/api/credentials', () => ({
  credentialsApi: {
    list: vi.fn().mockResolvedValue([]),
    listPage: vi.fn().mockResolvedValue({ items: [], total: 0, page: 1, pageSize: 50 }),
    get: vi.fn(),
    create: vi.fn(),
    update: vi.fn(),
    delete: vi.fn(),
  },
}));
vi.mock('@/api/tasks', () => ({
  tasksApi: {
    list: vi.fn().mockResolvedValue({ items: [], total: 0, page: 1, pageSize: 1 }),
    get: vi.fn(),
    logs: vi.fn().mockResolvedValue({ nextCursor: 0, logs: [] }),
  },
}));
vi.mock('@/api/containers', () => ({
  containersApi: {
    containers: (...args: unknown[]) => containers(...args),
    images: vi.fn().mockResolvedValue({ items: [], observedAt: null, stale: false, refreshing: false }),
    networks: vi.fn().mockResolvedValue({ items: [], observedAt: null, stale: false, refreshing: false }),
    volumes: vi.fn().mockResolvedValue({ items: [], observedAt: null, stale: false, refreshing: false }),
    containerLogs: vi.fn(),
    containerAction: vi.fn(),
    deleteContainer: vi.fn(),
    pullImage: vi.fn(),
    refreshImages: vi.fn(),
    deleteUnusedImages: vi.fn(),
    deleteImage: vi.fn(),
    upgradeSelectedImages: vi.fn(),
    upgradeAllImages: vi.fn(),
    refreshNetworks: vi.fn(),
    refreshVolumes: vi.fn(),
    deleteUnusedVolumes: vi.fn(),
    deleteVolume: vi.fn(),
  },
}));
vi.mock('@/api/packages', () => ({
  packagesApi: {
    updates: vi.fn().mockResolvedValue({ lastRefreshedAt: null, refreshing: false, updates: [] }),
    refresh: vi.fn(),
    upgradeSelected: vi.fn(),
    upgradeAll: vi.fn(),
  },
}));
vi.mock('@/api/security', () => ({
  securityApi: {
    ufwState: vi.fn().mockResolvedValue({ serverId: 'srv-1', supported: true, installed: true, active: true, status: 'active', defaultPolicy: 'deny', rules: [] }),
    addUfwRule: vi.fn(),
    deleteUfwRule: vi.fn(),
    fail2BanState: vi.fn(),
    saveFail2Ban: vi.fn(),
    enableFail2Ban: vi.fn(),
    releaseFail2Ban: vi.fn(),
    installFail2Ban: vi.fn(),
  },
}));

const server = {
  id: 'srv-1',
  name: 'edge-1',
  host: '10.0.0.1',
  port: 22,
  credentialId: 'cred-1',
  reachable: true,
  tailscaleEnabled: false,
  tailscalePreferAgent: false,
  tailscalePreferInterconnect: false,
  proxyMode: 'direct',
  tunnelServerId: '',
  tunnelServerName: '',
  socks5Address: '',
  socks5Username: '',
  proxyInvalid: false,
  proxyError: '',
  os: { prettyName: 'Debian 13', supported: true },
  sudo: { passwordless: true },
  privilege: { privileged: true, mode: 'passwordless_sudo' },
  traits: { 'agent.status': 'compatible', 'agent.url': 'https://10.0.0.1:9786' },
};

async function mountPage() {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/servers', component: ServersPage }] });
  await router.push('/servers');
  await router.isReady();
  const wrapper = mount(ServersPage, {
    global: {
      plugins: [router],
      provide: { [toastKey as symbol]: { push: vi.fn(), remove: vi.fn() } },
    },
    attachTo: document.body,
  });
  await flushPromises();
  return { wrapper, router };
}

async function mountAtQuery(query: Record<string, string>) {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/servers', component: ServersPage }] });
  await router.push({ path: '/servers', query });
  await router.isReady();
  const wrapper = mount(ServersPage, {
    global: {
      plugins: [router],
      provide: { [toastKey as symbol]: { push: vi.fn(), remove: vi.fn() } },
    },
    attachTo: document.body,
  });
  await flushPromises();
  return { wrapper, router };
}

beforeEach(() => {
  useI18n().setLocale('en');
  vi.clearAllMocks();
  listPage.mockResolvedValue({ items: [server], total: 1, page: 1, pageSize: 50 });
  list.mockResolvedValue([]);
  get.mockResolvedValue(server);
  metrics.mockResolvedValue({ cpu: [], memory: [], diskIo: [], diskCurrent: null, network: [], load: [] });
  metricsBucketed.mockResolvedValue({ cpu: [], memory: [], diskIo: [], diskCurrent: null, network: [], load: [] });
  containers.mockResolvedValue({ items: [], observedAt: null, stale: false, refreshing: false });
});

/** 打开自定义 Select 并点击指定文案的选项（选项 Teleport 到 body）。 */
async function chooseSelectOption(selector: string, label: string) {
  const combobox = document.querySelector<HTMLElement>(selector);
  expect(combobox).not.toBeNull();
  combobox!.click();
  await flushPromises();
  const option = Array.from(document.querySelectorAll<HTMLElement>('[role="option"]')).find((element) => element.textContent?.trim() === label);
  expect(option, `option ${label}`).toBeTruthy();
  option!.click();
  await flushPromises();
}

function tunnelOptionLabels() {
  return Array.from(document.querySelectorAll<HTMLElement>('[role="option"]')).map((element) => element.textContent?.trim() ?? '');
}

describe('servers page table and covered detail', () => {
  it('renders the inventory as a table with row actions', async () => {
    const { wrapper } = await mountPage();

    expect(listPage).toHaveBeenCalledWith(expect.objectContaining({ page: 1, pageSize: 50 }));
    // 列精简：名称合并 host:port，地址不再单独占列；窄屏隐藏最近检查。
    expect(wrapper.findAll('thead th').map((cell) => cell.text())).toEqual(['Name', 'Status', 'Last checked', 'Actions']);
    expect(wrapper.findAll('thead th')[2]!.classes()).toContain('max-sm:hidden');
    const rows = wrapper.findAll('tbody tr');
    expect(rows).toHaveLength(1);
    expect(rows[0]!.text()).toContain('edge-1');
    expect(rows[0]!.text()).toContain('10.0.0.1:22');
    // 状态列只有一枚综合徽标（稳定性/代理信息进 tooltip）。
    expect(rows[0]!.findAll('td')[1]!.findAllComponents(Badge)).toHaveLength(1);
    // 名称列与操作列「详情」均可打开覆盖式详情；整行不可点。
    const buttons = rows[0]!.findAll('button');
    expect(buttons.find((button) => button.text() === 'edge-1')?.attributes('aria-label')).toBe('Open details for edge-1');
    expect(buttons.some((button) => button.text().includes('Details'))).toBe(true);
    expect(wrapper.find('[data-testid="server-detail"]').exists()).toBe(false);
  });

  it('covers the table with the detail view and syncs the URL', async () => {
    const { wrapper, router } = await mountPage();

    // 列表页本身没有面包屑；只有覆盖详情从列表入口建立 trail。
    expect(wrapper.find('nav[aria-label="Breadcrumb"]').exists()).toBe(false);

    const nameButton = wrapper.findAll('tbody tr').at(0)!.findAll('button').find((button) => button.text() === 'edge-1')!;
    await nameButton.trigger('click');
    await flushPromises();

    expect(router.currentRoute.value.query.server).toBe('srv-1');
    const breadcrumbs = wrapper.get('nav[aria-label="Breadcrumb"]');
    const trailLink = breadcrumbs.findAll('a').find((link) => link.text() === 'Servers')!;
    expect(trailLink.attributes('href')).toBe('/servers');
    // 末项（当前服务器）由 BreadcrumbBar 渲染 aria-current="page"；祖先链接的 aria-current 由 RouterLink 精确匹配补充。
    const currentCrumbs = breadcrumbs.findAll('[aria-current="page"]');
    expect(currentCrumbs.at(-1)!.text()).toBe('edge-1');
    // back 与面包屑由 PageHeader 自动同行，back 行为保留。
    expect(wrapper.find('button[aria-label="Back to list"]').exists()).toBe(true);

    const detail = wrapper.get('[data-testid="server-detail"]');
    expect(detail.text()).toContain('Overview');
    expect(detail.text()).toContain('Connection');
    // 宽屏概览：摘要卡后为两列属性/指标（PropertyList 2 列），图表长内容在全宽区域。
    expect(detail.html()).toContain('wide:grid-cols-2');
    expect(detail.find('[data-property-list]').exists()).toBe(true);
    expect(detail.text()).toContain('Metrics');
    expect(detail.text()).toContain('Probe');
    // 详情覆盖时列表保持挂载（用于保留滚动和筛选），但不可交互。
    const listPane = wrapper.element.querySelector('[inert]');
    expect(listPane).not.toBeNull();
  });

  it('lazily mounts a resource tab only after it is opened', async () => {
    const { wrapper } = await mountPage();
    const detailButton = wrapper.findAll('tbody tr').at(0)!.findAll('button').find((button) => button.text().includes('Details'))!;
    await detailButton.trigger('click');
    await flushPromises();

    expect(containers).not.toHaveBeenCalled();
    const resourcesTab = wrapper.findAll('[role="tab"]').find((tab) => tab.text() === 'Resources')!;
    await resourcesTab.trigger('click');
    await flushPromises();

    expect(containers).toHaveBeenCalledWith('srv-1', expect.anything());
    expect(wrapper.text()).toContain('No containers');
    // 子视图改成单层 tab 下的下拉选择器，不再出现第二排 tab。
    expect(wrapper.find('[role="combobox"][aria-label="Resources"]').text()).toContain('Containers');
    expect(wrapper.findAll('[role="tab"]').some((tab) => tab.text() === 'Images')).toBe(false);
  });

  it('uses a bounded NumberBox for the UFW rule port', async () => {
    const { wrapper } = await mountPage();
    const detailButton = wrapper.findAll('tbody tr').at(0)!.findAll('button').find((button) => button.text().includes('Details'))!;
    await detailButton.trigger('click');
    await flushPromises();

    const securityTab = wrapper.findAll('[role="tab"]').find((tab) => tab.text() === 'Network & security')!;
    await securityTab.trigger('click');
    await flushPromises();

    const addRule = wrapper.findAll('button').find((button) => button.text() === 'Add rule');
    expect(addRule).toBeTruthy();
    await addRule!.trigger('click');
    await flushPromises();

    const portBox = document.querySelector<HTMLInputElement>('#ufw-rule-port');
    expect(portBox).not.toBeNull();
    expect(portBox!.getAttribute('role')).toBe('spinbutton');
    expect(portBox!.getAttribute('aria-valuemin')).toBe('1');
    expect(portBox!.getAttribute('aria-valuemax')).toBe('65535');
  });

  it('maps legacy resource tab query aliases onto resources + subtab', async () => {
    const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/servers', component: ServersPage }] });
    await router.push({ path: '/servers', query: { server: 'srv-1', tab: 'images' } });
    await router.isReady();
    const wrapper = mount(ServersPage, {
      global: {
        plugins: [router],
        provide: { [toastKey as symbol]: { push: vi.fn(), remove: vi.fn() } },
      },
      attachTo: document.body,
    });
    await flushPromises();

    expect(wrapper.findAll('[role="tab"]').find((tab) => tab.text() === 'Resources')?.attributes('aria-selected')).toBe('true');
    expect(wrapper.find('[role="combobox"][aria-label="Resources"]').text()).toContain('Images');
  });

  it('maps the legacy metrics deep link onto the merged overview tab', async () => {
    const { wrapper } = await mountAtQuery({ server: 'srv-1', tab: 'metrics' });
    expect(wrapper.findAll('[role="tab"]').find((tab) => tab.text() === 'Overview')?.attributes('aria-selected')).toBe('true');
    expect(wrapper.findAll('[role="tab"]').some((tab) => tab.text() === 'Metrics')).toBe(false);
    expect(metricsBucketed).toHaveBeenCalledWith('srv-1', '1h', 60, expect.anything());
  });

  it('maps legacy agent and probes deep links onto system and network-security subtabs', async () => {
    const agentPage = await mountAtQuery({ server: 'srv-1', tab: 'agent' });
    expect(agentPage.wrapper.findAll('[role="tab"]').find((tab) => tab.text() === 'System')?.attributes('aria-selected')).toBe('true');
    expect(agentPage.wrapper.find('[role="combobox"][aria-label="System"]').text()).toContain('Agent');
  });

  it('maps the legacy probes deep link onto the firewall subview without a probes tab', async () => {
    const { wrapper } = await mountAtQuery({ server: 'srv-1', tab: 'probes' });
    expect(wrapper.findAll('[role="tab"]').find((tab) => tab.text() === 'Network & security')?.attributes('aria-selected')).toBe('true');
    expect(wrapper.find('[role="combobox"][aria-label="Network & security"]').text()).toContain('Firewall');
    expect(wrapper.findAll('[role="tab"]').some((tab) => tab.text() === 'Network probes')).toBe(false);
  });

  it('loads every server as an SSH tunnel candidate instead of only the current page', async () => {
    list.mockResolvedValue([server, { ...server, id: 'srv-60', name: 'beyond-page', host: '10.0.0.60' }]);
    const { wrapper } = await mountPage();
    await wrapper.findAll('button').find((button) => button.text() === 'Add')!.trigger('click');
    await flushPromises();

    expect(document.querySelector('[role="dialog"]')).not.toBeNull();
    expect(list).toHaveBeenCalled();
    // 端口使用 WinUI NumberBox（spinbutton）并带 1..65535 边界；draft 仍为字符串。
    const portBox = document.querySelector<HTMLInputElement>('#server-form-port');
    expect(portBox).not.toBeNull();
    expect(portBox!.getAttribute('role')).toBe('spinbutton');
    expect(portBox!.getAttribute('aria-valuemin')).toBe('1');
    expect(portBox!.getAttribute('aria-valuemax')).toBe('65535');
    await chooseSelectOption('#server-form-proxy-mode', 'SSH tunnel');
    // 打开隧道下拉后，当前页之外的服务器也必须可选。
    const combobox = document.querySelector<HTMLElement>('#server-form-tunnel-server')!;
    combobox.click();
    await flushPromises();
    expect(tunnelOptionLabels()).toContain('beyond-page · 10.0.0.60');
    expect(tunnelOptionLabels()).toContain('edge-1 · 10.0.0.1');
  });

  it('falls back to the page list with a retry hint when full candidates fail to load', async () => {
    list.mockRejectedValueOnce(new Error('network down'));
    const { wrapper } = await mountPage();
    await wrapper.findAll('button').find((button) => button.text() === 'Add')!.trigger('click');
    await flushPromises();

    await chooseSelectOption('#server-form-proxy-mode', 'SSH tunnel');
    expect(document.body.textContent).toContain('Unable to load the full server list');
    // 失败时仍用父页面列表兜底，直连已有候选不受阻塞。
    const combobox = document.querySelector<HTMLElement>('#server-form-tunnel-server')!;
    combobox.click();
    await flushPromises();
    expect(tunnelOptionLabels()).toContain('edge-1 · 10.0.0.1');

    // 重试成功后补全全量候选。
    list.mockResolvedValue([{ ...server, id: 'srv-60', name: 'beyond-page', host: '10.0.0.60' }]);
    const retry = Array.from(document.querySelectorAll<HTMLButtonElement>('[role="dialog"] button')).find((button) => button.textContent?.trim() === 'Retry')!;
    retry.click();
    await flushPromises();
    expect(document.body.textContent).not.toContain('Unable to load the full server list');
    expect(document.querySelector<HTMLElement>('#server-form-tunnel-server')).not.toBeNull();
  });

  it('searches servers beyond the preloaded list when picking an SSH tunnel server', async () => {
    const remote = { ...server, id: 'srv-500', name: 'far-away', host: '10.9.9.9' };
    listPage.mockImplementation(async (params: { q?: string } = {}) => (
      params?.q === 'far'
        ? { items: [remote], total: 1, page: 1, pageSize: 50 }
        : { items: [server], total: 1, page: 1, pageSize: 50 }
    ));
    const { wrapper } = await mountPage();
    await wrapper.findAll('button').find((button) => button.text() === 'Add')!.trigger('click');
    await flushPromises();
    await chooseSelectOption('#server-form-proxy-mode', 'SSH tunnel');

    document.querySelector<HTMLElement>('#server-form-tunnel-server')!.click();
    await flushPromises();
    const search = document.querySelector<HTMLInputElement>('[role="searchbox"]');
    expect(search, 'tunnel server search input').not.toBeNull();
    search!.value = 'far';
    search!.dispatchEvent(new Event('input'));
    await vi.waitFor(() => expect(listPage).toHaveBeenCalledWith(expect.objectContaining({ q: 'far' })));
    await flushPromises();

    // 搜索命中的分页外服务器必须加入候选并可被选择。
    expect(tunnelOptionLabels()).toContain('far-away · 10.9.9.9');
    const option = Array.from(document.querySelectorAll<HTMLElement>('[role="option"]'))
      .find((element) => element.textContent?.trim() === 'far-away · 10.9.9.9');
    option!.click();
    await flushPromises();
    expect(document.querySelector<HTMLElement>('#server-form-tunnel-server')!.textContent).toContain('far-away · 10.9.9.9');
  });
});