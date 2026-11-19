// @vitest-environment jsdom
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ServerMetricsSeries } from '@/api/servers';
import { useI18n } from '@/i18n';
import type { ServerDto } from '@/types/servers';
import MetricsPanel from './MetricsPanel.vue';

enableAutoUnmount(afterEach);

const metricsBucketed = vi.fn();

vi.mock('@/api/servers', () => ({
  serversApi: { metricsBucketed: (...args: unknown[]) => metricsBucketed(...args) },
}));
vi.mock('@/components/patterns/DataRefreshControl.vue', () => ({
  default: { name: 'DataRefreshControl', template: '<div />' },
}));
// jsdom 无 canvas：图表本体只验证挂载契约，统计文本断言在卡片 footer。
// `__esModule` 让 defineAsyncComponent 取 default 组件而不是模块命名空间。
vi.mock('@/components/patterns/MetricChart.vue', () => ({
  __esModule: true,
  default: {
    name: 'MetricChart',
    props: ['series', 'valueKind', 'thresholds', 'label'],
    template: '<div data-testid="metric-chart" />',
  },
}));

const GIB = 1024 ** 3;
const base = '2026-11-19T10:00:00.000Z';

function server(): ServerDto {
  return {
    id: 'srv-1',
    name: 'edge-1',
    host: '10.0.0.1',
    port: 22,
    credentialId: 'cred-1',
    reachable: true,
    proxyMode: 'direct',
    tunnelServerId: '',
    tunnelServerName: '',
    socks5Address: '',
    socks5Username: '',
    proxyInvalid: false,
    proxyError: '',
  } as ServerDto;
}

function at(minute: number): string {
  return new Date(Date.parse(base) + minute * 60 * 1000).toISOString();
}

function metrics(overrides: Partial<ServerMetricsSeries> = {}): ServerMetricsSeries {
  return {
    range: '1h',
    cpu: [
      { time: at(0), usagePercent: 10 },
      { time: at(1), usagePercent: 40 },
      { time: at(2), usagePercent: 20 },
    ],
    memory: [
      { time: at(0), usedBytes: 5 * GIB, totalBytes: 10 * GIB },
      { time: at(1), usedBytes: 5 * GIB, totalBytes: 10 * GIB },
    ],
    diskCurrent: null,
    diskIo: [],
    network: [],
    load: [],
    ...overrides,
  };
}

async function mountPanel(): Promise<ReturnType<typeof mount>> {
  const wrapper = mount(MetricsPanel, { props: { server: server(), active: true }, attachTo: document.body });
  await flushPromises();
  await flushPromises();
  return wrapper;
}

function cardText(wrapper: ReturnType<typeof mount>, index: number): string {
  return wrapper.findAll('[data-chart-card]')[index]?.text() ?? '';
}

beforeEach(() => {
  useI18n().setLocale('en');
  vi.clearAllMocks();
  metricsBucketed.mockResolvedValue(metrics());
});

describe('server metrics panel statistics', () => {
  it('shows average, maximum and minimum together for the CPU chart', async () => {
    const wrapper = await mountPanel();

    expect(metricsBucketed).toHaveBeenCalledWith('srv-1', '1h', 60, expect.anything());
    expect(cardText(wrapper, 0)).toContain('Avg 23.3% · Max 40.0% · Min 10.0%');
    expect(cardText(wrapper, 0)).not.toContain('Peak');
  });

  it('keeps the average/peak summary for the non-CPU charts', async () => {
    const wrapper = await mountPanel();

    expect(cardText(wrapper, 1)).toContain('Avg 50.0% · Peak 50.0%');
  });

  it('renders placeholders instead of faking CPU extrema when the range has no CPU samples', async () => {
    metricsBucketed.mockResolvedValue(metrics({ cpu: [] }));
    const wrapper = await mountPanel();

    expect(cardText(wrapper, 0)).toContain('Avg Not available · Max Not available · Min Not available');
  });
});
