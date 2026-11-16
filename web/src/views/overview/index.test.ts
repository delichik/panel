// @vitest-environment jsdom
import { flushPromises, mount, enableAutoUnmount } from '@vue/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { createMemoryHistory, createRouter } from 'vue-router';
import { toastKey } from '@/components/ui/toast';
import { useI18n } from '@/i18n';
import type { ServerDto } from '@/types/servers';
import OverviewPage from './index.vue';

enableAutoUnmount(afterEach);

const list = vi.fn();
const metricsBucketed = vi.fn();
const networkProbes = vi.fn();
const updates = vi.fn();
const images = vi.fn();
const config = vi.fn();

vi.mock('@/api/servers', () => ({
  serversApi: {
    list: (...args: unknown[]) => list(...args),
    metricsBucketed: (...args: unknown[]) => metricsBucketed(...args),
    networkProbes: (...args: unknown[]) => networkProbes(...args),
  },
}));
vi.mock('@/api/packages', () => ({
  packagesApi: { updates: (...args: unknown[]) => updates(...args) },
}));
vi.mock('@/api/containers', () => ({
  containersApi: { images: (...args: unknown[]) => images(...args) },
}));
vi.mock('@/api/networkProbe', () => ({
  networkProbeFacilityApi: { config: (...args: unknown[]) => config(...args) },
}));

function server(overrides: Partial<ServerDto> = {}): ServerDto {
  const now = new Date().toISOString();
  return {
    id: 'srv-1',
    name: 'edge',
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
    os: { supported: true },
    lastCheckedAt: now,
    ...overrides,
  } as ServerDto;
}

function hourIso(hoursAgo: number): string {
  return new Date(Date.now() - hoursAgo * 60 * 60 * 1000).toISOString();
}

function series(cpu = 23.5) {
  const time = new Date().toISOString();
  return {
    range: '1d',
    cpu: [{ time: hourIso(1), usagePercent: cpu }],
    memory: [{ time, usedBytes: 4 * 1024 ** 3, totalBytes: 8 * 1024 ** 3 }],
    diskCurrent: { time, usedBytes: 63, totalBytes: 100 },
    diskIo: [],
    network: [{ time, rxBytesPerSecond: 1, txBytesPerSecond: 1 }],
    load: [{ time: hourIso(1), load1: 0.42, load5: 0.3, load15: 0.2 }],
    latest: {
      time,
      cpuUsagePercent: cpu,
      memoryUsedBytes: 4 * 1024 ** 3,
      memoryTotalBytes: 8 * 1024 ** 3,
      diskUsedBytes: 63,
      diskTotalBytes: 100,
      diskSampledAt: time,
      load1: 0.42,
      rxBytesPerSecond: 12 * 1024 * 1024,
      txBytesPerSecond: 3 * 1024 * 1024,
    },
  };
}

function probeResult(overrides: Record<string, unknown> = {}) {
  return {
    sourceServerId: 'srv-1',
    targetServerId: 'srv-2',
    targetAddress: '10.0.0.2',
    latencyMs: 145,
    packetLossPercent: 1.7,
    sent: 3,
    received: 3,
    sampledAt: new Date().toISOString(),
    error: '',
    stale: false,
    history: [
      { time: hourIso(2), latencyMs: 130, packetLossPercent: 0 },
      { time: hourIso(1), latencyMs: 145, packetLossPercent: 1.7 },
    ],
    ...overrides,
  };
}

async function mountPage(initial = '/overview') {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/overview', component: OverviewPage },
      { path: '/servers', component: { template: '<div />' } },
      { path: '/applications/facility-apps/network-probe/config', component: { template: '<div />' } },
    ],
  });
  await router.push(initial);
  await router.isReady();
  const wrapper = mount(OverviewPage, {
    global: {
      plugins: [router],
      provide: { [toastKey as symbol]: { push: vi.fn(), remove: vi.fn() } },
    },
    attachTo: document.body,
  });
  await flushPromises();
  await flushPromises();
  return { wrapper, router };
}

beforeEach(() => {
  useI18n().setLocale('en');
  vi.clearAllMocks();
  list.mockResolvedValue([
    server({ id: 'srv-1', name: 'edge', host: '10.0.0.1' }),
    server({ id: 'srv-2', name: 'core', host: '10.0.0.2' }),
  ]);
  metricsBucketed.mockResolvedValue(series());
  networkProbes.mockResolvedValue({
    serverId: 'srv-1',
    enabled: true,
    nodes: [{ serverId: 'srv-2', name: 'core', address: '10.0.0.2' }],
    results: [probeResult()],
    updatedAt: new Date().toISOString(),
  });
  updates.mockResolvedValue({ lastRefreshedAt: null, refreshing: false, updates: [{}, {}, {}] });
  images.mockResolvedValue({ items: [{ id: 'img-1', updateAvailable: true }], observedAt: null, stale: false, refreshing: false });
  config.mockResolvedValue({ mode: 'all', serverIds: [], version: 1, updatedAt: new Date().toISOString() });
});

describe('overview status page', () => {
  it('renders the fixed node status, network overview, and server cards without dashboard editing', async () => {
    const { wrapper } = await mountPage();

    expect(list).toHaveBeenCalled();
    expect(config).toHaveBeenCalled();
    expect(metricsBucketed).toHaveBeenCalledWith('srv-1', '1d', 1800, expect.objectContaining({ signal: expect.anything() }));
    expect(networkProbes).toHaveBeenCalledWith('srv-1', expect.objectContaining({ signal: expect.anything() }), true);
    expect(wrapper.text()).toContain('Node status');
    expect(wrapper.text()).toContain('edge');
    expect(wrapper.text()).toContain('core');
    expect(wrapper.text()).toContain('23.5%');
    expect(wrapper.text()).toContain('145 ms');
    expect(wrapper.text()).toContain('1.7 %');
    expect(wrapper.text()).toContain('50%');
    expect(wrapper.text()).toContain('63%');
    expect(wrapper.text()).toContain('12.0 MB/s');
    expect(wrapper.text()).toContain('3.0 MB/s');
    expect(wrapper.text()).not.toContain('Edit');
    expect(wrapper.text()).not.toContain('10.0.0.1:22');
    expect(wrapper.text()).not.toContain('Network overview');
    expect(wrapper.text()).not.toContain('Last report');
    const cards = wrapper.findAll('[data-testid="server-card"]');
    expect(cards).toHaveLength(2);
    expect(cards[0]!.text()).toContain('Probe source');
    // 探测源卡片不显示延迟/丢包历史条（只有 CPU + 负载两条）。
    expect(cards[0]!.findAll('[data-testid="history-strip"]')).toHaveLength(2);
    expect(cards[1]!.findAll('[data-testid="history-strip"]')).toHaveLength(4);
    expect(wrapper.findAll('[data-testid="history-strip"]')).toHaveLength(6);
    // 状态用颜色点表达，但必须保留读屏文本。
    expect(cards[0]!.find('.sr-only').text()).toBe('Online');
  });

  it('deep-links package update badges to the packages tab', async () => {
    const { wrapper, router } = await mountPage();

    const packageBadge = wrapper.findAll('[data-testid="package-updates-badge"]')[0]!;
    expect(packageBadge.text()).toContain('3');
    await packageBadge.trigger('click');
    await flushPromises();
    expect(router.currentRoute.value.path).toBe('/servers');
    expect(router.currentRoute.value.query).toMatchObject({ server: 'srv-1', tab: 'packages' });
  });

  it('deep-links image update badges to the images subtab', async () => {
    const { wrapper, router } = await mountPage();

    const imageBadge = wrapper.findAll('[data-testid="image-updates-badge"]')[0]!;
    expect(imageBadge.text()).toContain('1');
    await imageBadge.trigger('click');
    await flushPromises();
    expect(router.currentRoute.value.path).toBe('/servers');
    expect(router.currentRoute.value.query).toMatchObject({ server: 'srv-1', tab: 'resources', subtab: 'images' });
  });

  it('keeps other cards when one server metrics request fails', async () => {
    metricsBucketed.mockImplementation(async (id: string) => {
      if (id === 'srv-2') throw new Error('metrics unavailable');
      return series(23.5);
    });
    const { wrapper } = await mountPage();

    const cards = wrapper.findAll('[data-testid="server-card"]');
    expect(cards[0]!.text()).toContain('23.5%');
    expect(cards[1]!.text()).toContain('—');
    expect(cards[1]!.text()).not.toContain('23.5%');
  });

  it('shows the add-server guidance when no servers exist', async () => {
    list.mockResolvedValue([]);
    const { wrapper } = await mountPage();

    expect(wrapper.text()).toContain('No servers connected');
    expect(wrapper.text()).toContain('Add server');
  });
});
