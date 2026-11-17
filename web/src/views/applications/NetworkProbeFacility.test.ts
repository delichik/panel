// @vitest-environment jsdom
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { toastKey } from '@/components/ui/toast';
import { useI18n } from '@/i18n';
import type { ServerDto } from '@/types/servers';
import NetworkProbeFacility from './NetworkProbeFacility.vue';

enableAutoUnmount(afterEach);

const config = vi.fn();
const results = vi.fn();
const saveConfig = vi.fn();
const networkProbes = vi.fn();

vi.mock('@/api/networkProbe', () => ({
  networkProbeFacilityApi: {
    config: (...args: unknown[]) => config(...args),
    results: (...args: unknown[]) => results(...args),
    saveConfig: (...args: unknown[]) => saveConfig(...args),
  },
}));
vi.mock('@/api/servers', () => ({
  serversApi: { networkProbes: (...args: unknown[]) => networkProbes(...args) },
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

function history() {
  const now = Date.now();
  return Array.from({ length: 4 }, (_, index) => ({
    time: new Date(now - (3 - index) * 60 * 60 * 1000).toISOString(),
    latencyMs: 100 + index * 10,
    packetLossPercent: index,
  }));
}

function probeResult(sourceId: string, targetId: string, overrides: Record<string, unknown> = {}) {
  return {
    sourceServerId: sourceId,
    targetServerId: targetId,
    targetAddress: `10.0.0.${targetId.slice(-1)}`,
    latencyMs: 145,
    packetLossPercent: 1.7,
    sent: 3,
    received: 3,
    sampledAt: new Date().toISOString(),
    error: '',
    stale: false,
    history: history(),
    ...overrides,
  };
}

const servers = [server('srv-1', 'edge', '10.0.0.1'), server('srv-2', 'core', '10.0.0.2'), server('srv-3', 'worker', '10.0.0.3')];

function sourceResults(sourceId: string, targetId: string, targetName: string) {
  return {
    serverId: sourceId,
    enabled: true,
    nodes: [{ serverId: targetId, name: targetName, address: `10.0.0.${targetId.slice(-1)}` }],
    results: [probeResult(sourceId, targetId)],
    updatedAt: new Date().toISOString(),
  };
}

async function mountFacility() {
  const wrapper = mount(NetworkProbeFacility, {
    props: { mode: 'facilityDetail', servers },
    global: { provide: { [toastKey as symbol]: { push: vi.fn(), remove: vi.fn() } } },
    attachTo: document.body,
  });
  await flushPromises();
  await flushPromises();
  return wrapper;
}

beforeEach(() => {
  useI18n().setLocale('en');
  vi.clearAllMocks();
  config.mockResolvedValue({ mode: 'all', serverIds: [], intervalSeconds: 60, pingCount: 3, pingTimeoutSeconds: 2, version: 1, updatedAt: new Date().toISOString() });
  results.mockResolvedValue({
    mode: 'all',
    nodes: [
      { serverId: 'srv-1', name: 'edge', address: '10.0.0.1' },
      { serverId: 'srv-2', name: 'core', address: '10.0.0.2' },
    ],
    results: [probeResult('srv-1', 'srv-2'), probeResult('srv-2', 'srv-1')],
    updatedAt: new Date().toISOString(),
  });
  networkProbes.mockImplementation(async (sourceId: string) => (
    sourceId === 'srv-2' ? sourceResults('srv-2', 'srv-1', 'edge') : sourceResults('srv-1', 'srv-2', 'core')
  ));
});

describe('network probe facility', () => {
  it('renders current values and 24h history as a vertical target list instead of a matrix table', async () => {
    const wrapper = await mountFacility();

    expect(networkProbes).toHaveBeenCalledWith('srv-1', expect.objectContaining({ signal: expect.anything() }), true);
    expect(wrapper.find('table').exists()).toBe(false);
    const rows = wrapper.findAll('[data-testid="probe-target"]');
    expect(rows).toHaveLength(1);
    expect(rows[0]!.text()).toContain('core');
    expect(rows[0]!.text()).toContain('145 ms');
    expect(rows[0]!.text()).toContain('1.7 %');
    expect(rows[0]!.text()).toContain('3 / 3');
    // 有丢包但未失败 → 降级状态文案。
    expect(rows[0]!.text()).toContain('Packet loss');
    // 延迟与丢包各一条历史条。
    expect(rows[0]!.findAll('[data-testid="history-strip"]')).toHaveLength(2);
    expect(rows[0]!.text()).toContain('Latency · last 24 hours');
    expect(rows[0]!.text()).toContain('Packet loss · last 24 hours');
  });

  it('filters targets by name or address', async () => {
    results.mockResolvedValue({
      mode: 'all',
      nodes: [
        { serverId: 'srv-1', name: 'edge', address: '10.0.0.1' },
        { serverId: 'srv-2', name: 'core', address: '10.0.0.2' },
        { serverId: 'srv-3', name: 'worker', address: '10.0.0.3' },
      ],
      results: [probeResult('srv-1', 'srv-2'), probeResult('srv-1', 'srv-3')],
      updatedAt: new Date().toISOString(),
    });
    networkProbes.mockImplementation(async (sourceId: string) => ({
      serverId: sourceId,
      enabled: true,
      nodes: [
        { serverId: 'srv-2', name: 'core', address: '10.0.0.2' },
        { serverId: 'srv-3', name: 'worker', address: '10.0.0.3' },
      ],
      results: [probeResult(sourceId, 'srv-2'), probeResult(sourceId, 'srv-3', { latencyMs: 42 })],
      updatedAt: new Date().toISOString(),
    }));
    const wrapper = await mountFacility();
    expect(wrapper.findAll('[data-testid="probe-target"]')).toHaveLength(2);

    const search = wrapper.find<HTMLInputElement>('input[type="search"]');
    search.element.value = 'worker';
    await search.trigger('input');
    await flushPromises();

    const rows = wrapper.findAll('[data-testid="probe-target"]');
    expect(rows).toHaveLength(1);
    expect(rows[0]!.text()).toContain('worker');

    search.element.value = 'no-such-target';
    await search.trigger('input');
    await flushPromises();
    expect(wrapper.findAll('[data-testid="probe-target"]')).toHaveLength(0);
    expect(wrapper.text()).toContain('No targets match the search');
  });

  it('loads the selected probe source and keeps last data when a refresh fails', async () => {
    const wrapper = await mountFacility();
    expect(wrapper.findAll('[data-testid="probe-target"]')[0]!.text()).toContain('core');

    networkProbes.mockRejectedValueOnce(new Error('network down'));
    const refresh = wrapper.findAll('button').find((button) => button.text() === 'Refresh')!;
    await refresh.trigger('click');
    await flushPromises();

    // 失败不清空上次成功数据，只提示当前为旧结果。
    expect(wrapper.text()).toContain('Refresh failed. Showing the last successful results.');
    expect(wrapper.findAll('[data-testid="probe-target"]')).toHaveLength(1);
  });

  it('shows probe parameter fields with inline range validation', async () => {
    const wrapper = await mountFacility();
    const inputs = wrapper.findAll<HTMLInputElement>('input[type="number"]');
    expect(inputs).toHaveLength(3);
    expect(inputs[0]!.element.value).toBe('60');
    expect(inputs[1]!.element.value).toBe('3');
    expect(inputs[2]!.element.value).toBe('2');

    inputs[0]!.element.value = '5';
    await inputs[0]!.trigger('input');
    await flushPromises();
    expect(wrapper.text()).toContain('Enter a whole number between 10 and 3600 seconds.');
    expect(saveConfig).not.toHaveBeenCalled();
  });
});
