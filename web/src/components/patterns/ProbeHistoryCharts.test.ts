// @vitest-environment jsdom
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import ProbeHistoryCharts from './ProbeHistoryCharts.vue';
import { useI18n } from '@/i18n';
import type { MetricSeriesData } from '@/utils/metrics';

enableAutoUnmount(afterEach);

const emptySeries = (id: string): MetricSeriesData => ({ id, name: id, points: [] });
const series = (id: string, points: Array<{ time: string; value: number | null }>): MetricSeriesData => ({ id, name: id, points });

beforeEach(() => {
  useI18n().setLocale('en');
});

describe('ProbeHistoryCharts', () => {
  it('uses one shared ChartCard per series and keeps the empty state inside the card', async () => {
    const wrapper = mount(ProbeHistoryCharts, {
      props: { latency: emptySeries('latency'), loss: emptySeries('loss'), latencyCurrent: null, lossCurrent: null },
    });
    await flushPromises();

    const root = wrapper.get('[data-testid="probe-history-charts"]');
    const cards = root.findAll('[data-chart-card]');
    expect(cards).toHaveLength(2);

    const { t } = useI18n();
    expect(wrapper.text()).toContain(t('serversPage.probe.latencyHistory'));
    expect(wrapper.text()).toContain(t('serversPage.probe.packetLossHistory'));
    expect(wrapper.text()).toContain(t('serversPage.probe.noHistory'));

    // 空态时没有当前值，也不额外画第二层 surface-card（ChartCard 就是唯一卡片）。
    expect(root.findAll('[data-chart-value]')).toHaveLength(0);
    expect(root.findAll('.surface-card')).toHaveLength(2);
    expect(root.find('.surface-card .surface-card').exists()).toBe(false);
  });

  it('shows the current values and drops the empty placeholder when history exists', async () => {
    const wrapper = mount(ProbeHistoryCharts, {
      props: {
        latency: series('latency', [{ time: '2026-08-01T00:00:00.000Z', value: 12 }]),
        loss: series('loss', [{ time: '2026-08-01T00:00:00.000Z', value: 0 }]),
        latencyCurrent: 12,
        lossCurrent: 0,
      },
    });
    await flushPromises();

    const values = wrapper.findAll('[data-chart-value]');
    expect(values).toHaveLength(2);
    expect(values[0]!.text()).toContain('12');
    expect(values[1]!.text()).toContain('0');
    expect(wrapper.find('[data-chart-card-empty]').exists()).toBe(false);
    expect(wrapper.findAll('.surface-card')).toHaveLength(2);
  });
});
