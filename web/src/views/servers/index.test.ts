// @vitest-environment jsdom
import { flushPromises, mount, enableAutoUnmount } from '@vue/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { createMemoryHistory, createRouter } from 'vue-router';
import { toastKey } from '@/components/ui/toast';
import { useI18n } from '@/i18n';
import ServersPage from './index.vue';

enableAutoUnmount(afterEach);

const listPage = vi.fn();
const get = vi.fn();
const metrics = vi.fn();
const containers = vi.fn();

vi.mock('@/api/servers', () => ({
  serversApi: {
    listPage: (...args: unknown[]) => listPage(...args),
    list: vi.fn().mockResolvedValue([]),
    get: (...args: unknown[]) => get(...args),
    test: vi.fn(),
    delete: vi.fn(),
    metrics: (...args: unknown[]) => metrics(...args),
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

beforeEach(() => {
  useI18n().setLocale('en');
  vi.clearAllMocks();
  listPage.mockResolvedValue({ items: [server], total: 1, page: 1, pageSize: 50 });
  get.mockResolvedValue(server);
  metrics.mockResolvedValue({ cpu: [], memory: [], disk: [], network: [], load: [] });
  containers.mockResolvedValue({ items: [], observedAt: null, stale: false, refreshing: false });
});

describe('servers page table and covered detail', () => {
  it('renders the inventory as a table with row actions', async () => {
    const { wrapper } = await mountPage();

    expect(listPage).toHaveBeenCalledWith(expect.objectContaining({ page: 1, pageSize: 50 }));
    const rows = wrapper.findAll('tbody tr');
    expect(rows).toHaveLength(1);
    expect(rows[0]!.text()).toContain('edge-1');
    expect(rows[0]!.text()).toContain('10.0.0.1:22');
    // 名称列与操作列「详情」均可打开覆盖式详情；整行不可点。
    const buttons = rows[0]!.findAll('button');
    expect(buttons.find((button) => button.text() === 'edge-1')?.attributes('aria-label')).toBe('Open details for edge-1');
    expect(buttons.some((button) => button.text().includes('Details'))).toBe(true);
    expect(wrapper.find('[data-testid="server-detail"]').exists()).toBe(false);
  });

  it('covers the table with the detail view and syncs the URL', async () => {
    const { wrapper, router } = await mountPage();

    const nameButton = wrapper.findAll('tbody tr').at(0)!.findAll('button').find((button) => button.text() === 'edge-1')!;
    await nameButton.trigger('click');
    await flushPromises();

    expect(router.currentRoute.value.query.server).toBe('srv-1');
    const detail = wrapper.get('[data-testid="server-detail"]');
    expect(detail.text()).toContain('Overview');
    expect(detail.text()).toContain('Connection');
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
    expect(wrapper.findAll('[role="tab"]').some((tab) => tab.text() === 'Images')).toBe(true);
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
    expect(wrapper.findAll('[role="tab"]').find((tab) => tab.text() === 'Images')?.attributes('aria-selected')).toBe('true');
  });
});
