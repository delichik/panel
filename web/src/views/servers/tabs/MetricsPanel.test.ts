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
      { time: at(0), usagePercent: 10, usagePercentMin: 5, usagePercentMax: 15 },
      { time: at(1), usagePercent: 40, usagePercentMin: 35, usagePercentMax: 45 },
      { time: at(2), usagePercent: 20, usagePercentMin: 15, usagePercentMax: 25 },
    ],
    memory: [
      { time: at(0), usedBytes: 4 * GIB, totalBytes: 10 * GIB },
      { time: at(1), usedBytes: 6 * GIB, totalBytes: 10 * GIB },
      { time: at(2), usedBytes: 5 * GIB, totalBytes: 10 * GIB },
    ],
    diskCurrent: null,
    diskIo: [
      { time: at(0), readBytesPerSecond: 1024, readBytesPerSecondMin: 512, readBytesPerSecondMax: 1536, writeBytesPerSecond: 512, writeBytesPerSecondMin: 256, writeBytesPerSecondMax: 768 },
      { time: at(1), readBytesPerSecond: 2048, readBytesPerSecondMin: 1024, readBytesPerSecondMax: 3072, writeBytesPerSecond: 1536, writeBytesPerSecondMin: 768, writeBytesPerSecondMax: 2048 },
    ],
    network: [
      { time: at(0), rxBytesPerSecond: 2048, rxBytesPerSecondMin: 1024, rxBytesPerSecondMax: 3072, txBytesPerSecond: 1024, txBytesPerSecondMin: 512, txBytesPerSecondMax: 2048 },
      { time: at(1), rxBytesPerSecond: 4096, rxBytesPerSecondMin: 2048, rxBytesPerSecondMax: 8192, txBytesPerSecond: 3072, txBytesPerSecondMin: 1024, txBytesPerSecondMax: 4096 },
    ],
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
  it('splits disk/network directions and shows band extrema for every chart', async () => {
    const wrapper = await mountPanel();

    expect(metricsBucketed).toHaveBeenCalledWith('srv-1', '1h', 60, expect.anything());
    expect(wrapper.findAll('[data-chart-card]')).toHaveLength(6);
    expect(cardText(wrapper, 0)).toContain('Avg 23.3% · Max 45.0% · Min 5.0%');
    expect(cardText(wrapper, 1)).toContain('Avg 50.0% · Max 60.0% · Min 40.0%');
    expect(cardText(wrapper, 2)).toContain('Avg 1.5 KB/s · Max 3.0 KB/s · Min 512 B/s');
    expect(cardText(wrapper, 3)).toContain('Avg 1.0 KB/s · Max 2.0 KB/s · Min 256 B/s');
    expect(cardText(wrapper, 4)).toContain('Avg 3.0 KB/s · Max 8.0 KB/s · Min 1.0 KB/s');
    expect(cardText(wrapper, 5)).toContain('Avg 2.0 KB/s · Max 4.0 KB/s · Min 512 B/s');
    expect(wrapper.text()).not.toContain('Peak');
  });

  it('renders placeholders instead of faking CPU extrema when the range has no CPU samples', async () => {
    metricsBucketed.mockResolvedValue(metrics({ cpu: [] }));
    const wrapper = await mountPanel();

    expect(cardText(wrapper, 0)).toContain('Avg Not available · Max Not available · Min Not available');
  });
});
