// @vitest-environment jsdom
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { useI18n } from '@/i18n';
import type { NetworkProbeTargetMatrix } from '@/types/networkProbe';
import type { ServerDto } from '@/types/servers';
import ProbeOverviewPanel from './ProbeOverviewPanel.vue';

enableAutoUnmount(afterEach);

const targets = vi.fn();

vi.mock('@/api/networkProbe', () => ({
  networkProbeFacilityApi: { targets: (...args: unknown[]) => targets(...args) },
}));
vi.mock('@/components/patterns/DataRefreshControl.vue', () => ({
  default: { name: 'DataRefreshControl', template: '<div />' },
}));
vi.mock('@/components/patterns/ProbeHistoryCharts.vue', () => ({
  default: {
    name: 'ProbeHistoryCharts',
    props: ['latency', 'loss', 'latencyCurrent', 'lossCurrent'],
    template: '<div data-testid="probe-history-charts"><span data-testid="latency-points">{{ latency.points.length }}</span><span data-testid="loss-points">{{ loss.points.length }}</span></div>',
  },
}));

function server(id: string, name: string, host: string): ServerDto {
  return {
    id,
    name,
    host,
    ipv4: host,
    ipv6: '',
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
  } as ServerDto;
}

const servers = [server('srv-1', 'edge', '10.0.0.1'), server('srv-2', 'core', '10.0.0.2')];

function history() {
  const now = Date.now();
  return Array.from({ length: 3 }, (_, index) => ({
    time: new Date(now - (2 - index) * 60 * 60 * 1000).toISOString(),
    latencyMs: 30 + index,
    packetLossPercent: index,
  }));
}

function targetNode(overrides: Record<string, unknown> = {}) {
  return {
    serverId: 'srv-2',
    name: 'core',
    address: '10.0.0.2',
    sourceCount: 2,
    latencyMs: 42,
    packetLossPercent: 1.5,
    worstPacketLossPercent: 3,
    sampledAt: new Date().toISOString(),
    stale: false,
    history: history(),
    ...overrides,
  };
}

function targetMatrix(nodes: ReturnType<typeof targetNode>[]): NetworkProbeTargetMatrix {
  return { nodes, updatedAt: new Date().toISOString() };
}

async function mountPanel(target: ServerDto) {
  const wrapper = mount(ProbeOverviewPanel, {
    props: { server: target, active: true },
    attachTo: document.body,
  });
  await flushPromises();
  await flushPromises();
  return wrapper;
}

beforeEach(() => {
  useI18n().setLocale('en');
  vi.clearAllMocks();
  targets.mockResolvedValue(targetMatrix([
    targetNode({ serverId: 'srv-1', name: 'edge', address: '10.0.0.1', sourceCount: 0, latencyMs: null, packetLossPercent: null, history: [] }),
    targetNode(),
  ]));
});

describe('server overview probe panel', () => {
  it('shows the aggregated latency and packet loss history for the current server without a probe source selector', async () => {
    const wrapper = await mountPanel(servers[1]!);

    expect(targets).toHaveBeenCalledWith(expect.objectContaining({ signal: expect.anything() }), true);
    expect(wrapper.find('[role="combobox"]').exists()).toBe(false);
    const charts = wrapper.findComponent({ name: 'ProbeHistoryCharts' });
    expect(charts.exists()).toBe(true);
    expect(charts.find('[data-testid="latency-points"]').text()).toBe('3');
    expect(charts.find('[data-testid="loss-points"]').text()).toBe('3');
    expect(charts.props('latencyCurrent')).toBe(42);
    expect(charts.props('lossCurrent')).toBe(1.5);
    expect(wrapper.text()).toContain('Packet loss');
  });

  it('keeps both chart cards visible with empty history when the server has no probe result yet', async () => {
    targets.mockResolvedValue(targetMatrix([
      targetNode({ sourceCount: 0, latencyMs: null, packetLossPercent: null, worstPacketLossPercent: null, sampledAt: null, stale: true, history: [] }),
    ]));
    const wrapper = await mountPanel(servers[1]!);

    expect(wrapper.text()).toContain('No probe result for this server yet.');
    expect(wrapper.text()).toContain('No data');
    const charts = wrapper.find('[data-testid="probe-history-charts"]');
    expect(charts.exists()).toBe(true);
    expect(charts.find('[data-testid="latency-points"]').text()).toBe('0');
  });

  it('keeps the last successful data when a refresh fails', async () => {
    const wrapper = await mountPanel(servers[1]!);
    expect(wrapper.find('[data-testid="probe-history-charts"]').exists()).toBe(true);

    targets.mockRejectedValueOnce(new Error('network down'));
    const refresh = wrapper.findAll('button').find((button) => button.text() === 'Refresh')!;
    await refresh.trigger('click');
    await flushPromises();

    expect(wrapper.text()).toContain('Refresh failed. Showing the last successful results.');
    expect(wrapper.find('[data-testid="probe-history-charts"]').exists()).toBe(true);
  });

  it('shows an explicit error empty state with retry when the first load fails', async () => {
    targets.mockRejectedValueOnce(new Error('network down'));
    const wrapper = await mountPanel(servers[1]!);

    expect(wrapper.text()).toContain('Unable to load data.');
    expect(wrapper.find('[data-testid="probe-history-charts"]').exists()).toBe(false);

    const retry = wrapper.findAll('button').find((button) => button.text() === 'Retry')!;
    await retry.trigger('click');
    await flushPromises();

    expect(wrapper.find('[data-testid="probe-history-charts"]').exists()).toBe(true);
  });
});
