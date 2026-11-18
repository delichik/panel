// @vitest-environment jsdom
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
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
const applicationsList = vi.fn();

vi.mock('@/api/applications', () => ({
  applicationsApi: { list: (...args: unknown[]) => applicationsList(...args) },
}));

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
    diskIo: [{ time, readBytesPerSecond: 1536 * 1024, writeBytesPerSecond: 512 * 1024 }],
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
  applicationsList.mockResolvedValue({
    items: [
      { id: 'app-1', enabled: true, deployingInstanceCount: 0, abnormalInstanceCount: 0 },
      { id: 'app-2', enabled: false, deployingInstanceCount: 0, abnormalInstanceCount: 0 },
      { id: 'app-3', enabled: true, deployingInstanceCount: 1, abnormalInstanceCount: 0 },
      { id: 'app-4', enabled: true, deployingInstanceCount: 0, abnormalInstanceCount: 0, lastError: 'deploy failed' },
    ],
    total: 4,
  });
});

describe('overview status page', () => {
  it('renders the fixed node status, network overview, and server cards without dashboard editing', async () => {
    const { wrapper } = await mountPage();

    expect(list).toHaveBeenCalled();
    expect(config).toHaveBeenCalled();
    expect(metricsBucketed).toHaveBeenCalledWith('srv-1', '1d', 1800, expect.objectContaining({ signal: expect.anything() }));
    expect(networkProbes).toHaveBeenCalledWith('srv-1', expect.objectContaining({ signal: expect.anything() }), true);
    expect(wrapper.text()).toContain('Node status');
    expect(wrapper.text()).toContain('Applications');
    expect(wrapper.text()).toContain('4');
    expect(wrapper.get('[data-testid="application-summary"]').text()).toContain('Disabled');
    // 摘要头：在线率环 + 胶囊 KPI + 应用分布条 + 容量三环。
    const onlineRing = wrapper.get('[data-testid="online-rate-ring"]');
    expect(onlineRing.attributes('role')).toBe('img');
    expect(onlineRing.attributes('aria-label')).toBe('Online rate: 100%');
    expect(onlineRing.text()).toBe('100%');
    expect(wrapper.get('[data-testid="application-ratio-bar"]').attributes('aria-label')).toBe('Applications: 3 enabled, 1 disabled');
    expect(wrapper.get('[data-testid="capacity-summary"]').text()).toContain('Capacity');
    expect(wrapper.get('[data-testid="capacity-cpu"]').text()).toBe('24%');
    expect(wrapper.get('[data-testid="capacity-memory"]').text()).toBe('50%');
    expect(wrapper.get('[data-testid="capacity-disk"]').text()).toBe('63%');
    expect(wrapper.text()).toContain('23.5%');
    expect(wrapper.text()).toContain('edge');
    expect(wrapper.text()).toContain('core');
    expect(wrapper.text()).toContain('145 ms');
    expect(wrapper.text()).toContain('1.7 %');
    expect(wrapper.text()).toContain('50%');
    expect(wrapper.text()).toContain('63%');
    expect(wrapper.text()).toContain('12.0 MB/s');
    expect(wrapper.text()).toContain('3.0 MB/s');
    expect(wrapper.text()).toContain('Disk read');
    expect(wrapper.text()).toContain('Disk write');
    expect(wrapper.text()).toContain('1.5 MB/s');
    expect(wrapper.text()).not.toContain('Edit');
    expect(wrapper.text()).not.toContain('10.0.0.1:22');
    expect(wrapper.text()).not.toContain('Network overview');
    expect(wrapper.text()).not.toContain('Last report');
    const cards = wrapper.findAll('[data-testid="server-card"]');
    expect(cards).toHaveLength(2);
    expect(cards[0]!.text()).toContain('Probe source');
    // 探测源卡片仍显示延迟/丢包占位控件，但不发起历史数据请求、不画历史条。
    expect(cards[0]!.text()).toContain('Latency');
    expect(cards[0]!.text()).toContain('Packet loss');
    expect(cards[0]!.findAll('[data-testid="metric-placeholder"]')).toHaveLength(2);
    expect(cards[1]!.findAll('[data-testid="metric-placeholder"]')).toHaveLength(0);
    expect(cards[0]!.findAll('[data-testid="history-strip"]')).toHaveLength(1);
    expect(cards[1]!.findAll('[data-testid="history-strip"]')).toHaveLength(3);
    expect(wrapper.findAll('[data-testid="history-strip"]')).toHaveLength(4);
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

  it('keeps metric values after an auto-refresh tick', async () => {
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] });
    const { wrapper } = await mountPage();
    expect(wrapper.findAll('[data-testid="server-card"]')[0]!.text()).toContain('23.5%');

    await vi.advanceTimersByTimeAsync(10 * 60 * 1000);
    await flushPromises();
    await flushPromises();

    const cards = wrapper.findAll('[data-testid="server-card"]');
    expect(cards).toHaveLength(2);
    expect(cards[0]!.text()).toContain('23.5%');
    expect(cards[1]!.text()).toContain('23.5%');
    vi.useRealTimers();
  });

  it('keeps metric values when the probe source has no results', async () => {
    networkProbes.mockResolvedValue({
      serverId: 'srv-1',
      enabled: true,
      nodes: [{ serverId: 'srv-2', name: 'core', address: '10.0.0.2' }],
      results: [],
      updatedAt: null,
    });
    const { wrapper } = await mountPage();

    const cards = wrapper.findAll('[data-testid="server-card"]');
    expect(cards).toHaveLength(2);
    expect(cards[0]!.text()).toContain('23.5%');
    expect(cards[1]!.text()).toContain('23.5%');
    expect(cards[0]!.text()).toContain('50%');
    expect(cards[1]!.text()).toContain('63%');
  });

  it('keeps previous metric values when an auto-refresh metrics request fails', async () => {
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] });
    const { wrapper } = await mountPage();
    expect(wrapper.findAll('[data-testid="server-card"]')[0]!.text()).toContain('23.5%');

    metricsBucketed.mockRejectedValue(new Error('metrics unavailable'));
    await vi.advanceTimersByTimeAsync(10 * 60 * 1000);
    await flushPromises();
    await flushPromises();

    const cards = wrapper.findAll('[data-testid="server-card"]');
    expect(cards).toHaveLength(2);
    expect(cards[0]!.text()).toContain('23.5%');
    expect(cards[1]!.text()).toContain('23.5%');
    vi.useRealTimers();
  });

  it('keeps metric values when probe data is unavailable', async () => {
    networkProbes.mockRejectedValue(new Error('probe unavailable'));
    const { wrapper } = await mountPage();

    const cards = wrapper.findAll('[data-testid="server-card"]');
    expect(cards).toHaveLength(2);
    expect(cards[0]!.text()).toContain('23.5%');
    expect(cards[1]!.text()).toContain('23.5%');
    expect(cards[1]!.text()).toContain('63%');
  });

  it('shows the add-server guidance when no servers exist', async () => {
    list.mockResolvedValue([]);
    const { wrapper } = await mountPage();

    expect(wrapper.text()).toContain('No servers connected');
    expect(wrapper.text()).toContain('Add');
  });
});

describe('overview mobile layout contract', () => {
  const pageSource = readFileSync(resolve(__dirname, './index.vue'), 'utf8');
  const cardSource = readFileSync(resolve(__dirname, './ServerCard.vue'), 'utf8');

  it('stacks the open summary header and keeps server cards single-column below sm', () => {
    // 摘要头在 wide 以下单列；wide 两列（容量整行）、wider 三列。
    expect(pageSource).toContain('wide:grid-cols-2 wider:grid-cols-3');
    expect(pageSource).toContain('flex min-w-0 flex-wrap items-center gap-4');
    expect(cardSource).toContain('max-sm:grid-cols-1');
    expect(cardSource).toContain('gap-x-4 gap-y-2 max-sm:grid-cols-1');
  });

  it('lets the probe selector and refresh action wrap full width on phones', () => {
    expect(pageSource).toContain('min-w-0 flex-1 sm:w-44 sm:flex-none');
    expect(pageSource).toContain('max-sm:w-full');
  });
});