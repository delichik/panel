// @vitest-environment jsdom
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { toastKey } from '@/components/ui/toast';
import { useI18n } from '@/i18n';
import type { NetworkProbeTargetMatrix } from '@/types/networkProbe';
import type { ServerDto } from '@/types/servers';
import NetworkProbeFacility from './NetworkProbeFacility.vue';

enableAutoUnmount(afterEach);

const config = vi.fn();
const saveConfig = vi.fn();
const targets = vi.fn();

vi.mock('@/api/networkProbe', () => ({
  networkProbeFacilityApi: {
    config: (...args: unknown[]) => config(...args),
    saveConfig: (...args: unknown[]) => saveConfig(...args),
    targets: (...args: unknown[]) => targets(...args),
  },
}));
// 图表本体由 MetricChart/ProbeHistoryCharts 的测试覆盖；这里断言历史序列与空态传递。
vi.mock('@/components/patterns/ProbeHistoryCharts.vue', () => ({
  default: {
    name: 'ProbeHistoryCharts',
    props: ['latency', 'loss', 'latencyCurrent', 'lossCurrent'],
    template: '<div data-testid="probe-history-charts"><span data-testid="latency-points">{{ latency.points.length }}</span><span v-if="!latency.points.length">No probe history for this server yet.</span></div>',
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

function history() {
  const now = Date.now();
  return Array.from({ length: 4 }, (_, index) => ({
    time: new Date(now - (3 - index) * 60 * 60 * 1000).toISOString(),
    latencyMs: 100 + index * 10,
    packetLossPercent: index,
  }));
}

function targetNode(overrides: Record<string, unknown> = {}) {
  return {
    serverId: 'srv-1',
    name: 'edge',
    address: '10.0.0.1',
    sourceCount: 2,
    latencyMs: 30,
    packetLossPercent: 0,
    worstPacketLossPercent: 0,
    sampledAt: new Date().toISOString(),
    stale: false,
    history: history(),
    ...overrides,
  };
}

function targetMatrix(nodes: ReturnType<typeof targetNode>[]): NetworkProbeTargetMatrix {
  return { nodes, updatedAt: new Date().toISOString() };
}

const servers = [server('srv-1', 'edge', '10.0.0.1'), server('srv-2', 'core', '10.0.0.2'), server('srv-3', 'worker', '10.0.0.3')];

const defaultNodes = [
  targetNode(),
  targetNode({ serverId: 'srv-2', name: 'core', address: '10.0.0.2', sourceCount: 1, latencyMs: 145, packetLossPercent: 1.7, worstPacketLossPercent: 1.7 }),
  targetNode({ serverId: 'srv-3', name: 'worker', address: '10.0.0.3', sourceCount: 0, latencyMs: null, packetLossPercent: null, worstPacketLossPercent: null, sampledAt: null, stale: true, history: [] }),
];

async function mountFacility() {
  const wrapper = mount(NetworkProbeFacility, {
    props: { mode: 'facilityDetail', servers },
    global: { provide: { [toastKey as symbol]: { push: vi.fn(), remove: vi.fn() } } },
    attachTo: document.body,
  });
  await flushPromises();
  await flushPromises();
  await flushPromises();
  return wrapper;
}

function targetCards(wrapper: Awaited<ReturnType<typeof mountFacility>>) {
  return wrapper.findAll('[data-testid="probe-target-card"]');
}

async function selectOption(comboboxIndex: number, label: string) {
  const comboboxes = document.querySelectorAll<HTMLButtonElement>('[role="combobox"]');
  comboboxes[comboboxIndex]!.click();
  await flushPromises();
  const option = Array.from(document.querySelectorAll<HTMLButtonElement>('[role="option"]')).find((item) => item.textContent?.trim() === label);
  expect(option, `option "${label}"`).toBeTruthy();
  option!.click();
  await flushPromises();
}

beforeEach(() => {
  useI18n().setLocale('en');
  vi.clearAllMocks();
  config.mockResolvedValue({ mode: 'all', serverIds: [], intervalSeconds: 60, pingCount: 3, pingTimeoutSeconds: 2, version: 1, updatedAt: new Date().toISOString() });
  saveConfig.mockImplementation(async (input: Record<string, unknown>) => ({ ...input, version: 2, updatedAt: new Date().toISOString() }));
  targets.mockResolvedValue(targetMatrix(defaultNodes));
});

describe('network probe facility', () => {
  it('shows every server with aggregated current values and two 24h history charts, without a source/target selector', async () => {
    const wrapper = await mountFacility();

    expect(targets).toHaveBeenCalledWith(expect.objectContaining({ signal: expect.anything() }), true);
    expect(wrapper.find('table').exists()).toBe(false);
    // 结果区不再提供探测源/目标选择器。
    expect(wrapper.find('[data-testid="network-probe-results-section"] [role="combobox"]').exists()).toBe(false);

    const cards = targetCards(wrapper);
    expect(cards).toHaveLength(servers.length);
    expect(cards[0]!.text()).toContain('edge');
    expect(cards[0]!.text()).toContain('2');
    expect(cards[0]!.text()).toContain('30 ms');
    expect(cards[0]!.text()).toContain('OK');
    expect(cards[1]!.text()).toContain('core');
    expect(cards[1]!.text()).toContain('145 ms');
    expect(cards[1]!.text()).toContain('1.7 %');
    expect(cards[1]!.text()).toContain('Packet loss');

    // 每台服务器（含无结果）都保留两张 24h 折线图。
    expect(cards.every((card) => card.find('[data-testid="probe-history-charts"]').exists())).toBe(true);
    expect(cards[0]!.find('[data-testid="latency-points"]').text()).toBe('4');
    expect(cards[2]!.find('[data-testid="latency-points"]').text()).toBe('0');
  });

  it('keeps no-data server cards and their chart empty states visible', async () => {
    const wrapper = await mountFacility();

    const cards = targetCards(wrapper);
    const noDataCard = cards[2]!;
    expect(noDataCard.text()).toContain('worker');
    expect(noDataCard.text()).toContain('No data');
    expect(noDataCard.text()).toContain('—');
    const charts = noDataCard.find('[data-testid="probe-history-charts"]');
    expect(charts.exists()).toBe(true);
    expect(charts.text()).toContain('No probe history for this server yet.');
    expect(wrapper.text()).toContain('3 servers · 2 probed');
  });

  it('keeps the last successful data when a refresh fails', async () => {
    const wrapper = await mountFacility();
    expect(targetCards(wrapper)).toHaveLength(servers.length);

    targets.mockRejectedValueOnce(new Error('network down'));
    const refresh = wrapper.findAll('button').find((button) => button.text() === 'Refresh')!;
    await refresh.trigger('click');
    await flushPromises();

    // 失败不清空上次成功数据，只提示当前为旧结果。
    expect(wrapper.text()).toContain('Refresh failed. Showing the last successful results.');
    expect(targetCards(wrapper)).toHaveLength(servers.length);
    expect(targetCards(wrapper)[1]!.text()).toContain('145 ms');
  });

  it('scrolls the result list vertically inside the card without a horizontal table', async () => {
    const wrapper = await mountFacility();

    const list = wrapper.find('[data-testid="probe-target-cards"]');
    expect(list.exists()).toBe(true);
    expect(list.classes()).toContain('overflow-y-auto');
    expect(list.classes()).toContain('overflow-x-hidden');
    expect(list.element.tagName).toBe('UL');
  });

  it('splits probe scope, probe configuration, and results into independent sections', async () => {
    const wrapper = await mountFacility();

    const scope = wrapper.find('[data-testid="network-probe-scope-section"]');
    const configSection = wrapper.find('[data-testid="network-probe-config-section"]');
    const resultsSection = wrapper.find('[data-testid="network-probe-results-section"]');
    expect(scope.exists()).toBe(true);
    expect(configSection.exists()).toBe(true);
    expect(resultsSection.exists()).toBe(true);

    expect(scope.text()).toContain('Probe scope');
    expect(scope.text()).toContain('All servers');
    expect(configSection.text()).toContain('Probe configuration');
    expect(configSection.text()).toContain('Probe interval (minutes)');
    expect(resultsSection.text()).toContain('core');
    // 结果区展示全部服务器，配置区不渲染历史图表。
    expect(resultsSection.find('[data-testid="probe-history-charts"]').exists()).toBe(true);
    expect(configSection.find('[data-testid="probe-history-charts"]').exists()).toBe(false);
  });

  it('uses the compact server picker grid when the scope is selected', async () => {
    const wrapper = await mountFacility();
    await selectOption(0, 'Selected servers');

    const picker = wrapper.find('section[aria-label="Probe nodes"]');
    expect(picker.exists()).toBe(true);
    expect(picker.classes()).toContain('sm:grid-cols-2');
    expect(picker.classes()).toContain('xl:grid-cols-3');
    expect(picker.findAll('button[aria-pressed]')).toHaveLength(servers.length);
  });

  it('renders probe parameters as NumberBox spinbuttons with min/max clamping and inline validation', async () => {
    const wrapper = await mountFacility();
    const inputs = wrapper.findAll<HTMLInputElement>('input[role="spinbutton"]');
    expect(inputs).toHaveLength(3);
    // 默认配置 intervalSeconds=60 → 1 分钟。
    expect(inputs[0]!.element.value).toBe('1');
    expect(inputs[1]!.element.value).toBe('3');
    expect(inputs[2]!.element.value).toBe('2');
    expect(wrapper.text()).toContain('Range 1–60 minutes, default 1.');

    // NumberBox 在 commit 时按 min/max 夹取：61 → 60。
    inputs[0]!.element.value = '61';
    await inputs[0]!.trigger('input');
    await inputs[0]!.trigger('change');
    await flushPromises();
    expect(inputs[0]!.element.value).toBe('60');
    expect(wrapper.text()).not.toContain('Enter a whole number between 1 and 60 minutes.');

    // 清空仍触发字段校验并阻止保存（保持原有校验路径）。
    inputs[0]!.element.value = '';
    await inputs[0]!.trigger('input');
    await inputs[0]!.trigger('change');
    await flushPromises();
    expect(wrapper.text()).toContain('Enter a whole number between 1 and 60 minutes.');

    const save = wrapper.findAll('button').find((button) => button.text() === 'Save configuration')!;
    await save.trigger('click');
    await flushPromises();
    expect(saveConfig).not.toHaveBeenCalled();
  });

  it('saves the probe interval in minutes as intervalSeconds', async () => {
    const wrapper = await mountFacility();
    const interval = wrapper.find<HTMLInputElement>('[data-testid="network-probe-interval-minutes"]');
    interval.element.value = '2';
    await interval.trigger('input');
    await interval.trigger('change');
    await flushPromises();

    const save = wrapper.findAll('button').find((button) => button.text() === 'Save configuration')!;
    await save.trigger('click');
    await flushPromises();

    expect(saveConfig).toHaveBeenCalledWith({
      mode: 'all',
      serverIds: [],
      intervalSeconds: 120,
      pingCount: 3,
      pingTimeoutSeconds: 2,
    });
  });

  it('loads a legacy interval in seconds as whole minutes', async () => {
    config.mockResolvedValueOnce({ mode: 'all', serverIds: [], intervalSeconds: 90, pingCount: 3, pingTimeoutSeconds: 2, version: 1, updatedAt: new Date().toISOString() });
    const wrapper = await mountFacility();

    const interval = wrapper.find<HTMLInputElement>('[data-testid="network-probe-interval-minutes"]');
    // 90 秒按四舍五入显示为 2 分钟，且不会因为换算产生伪脏状态。
    expect(interval.element.value).toBe('2');
    const save = wrapper.findAll('button').find((button) => button.text() === 'Save configuration')!;
    expect(save.attributes('disabled')).toBeDefined();
  });
});
