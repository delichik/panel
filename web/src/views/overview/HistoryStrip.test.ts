// @vitest-environment jsdom
import { enableAutoUnmount, mount } from '@vue/test-utils';
import { afterEach, describe, expect, it } from 'vitest';
import HistoryStrip from './HistoryStrip.vue';
import MetricCell from './MetricCell.vue';

enableAutoUnmount(afterEach);

function segmentClasses(wrapper: ReturnType<typeof mount>): string[][] {
  return wrapper.findAll('span[aria-hidden="true"]').map((node) => node.classes());
}

describe('overview HistoryStrip warning thresholds', () => {
  it('keeps the fixed green/yellow/red zones without a threshold', () => {
    const wrapper = mount(HistoryStrip, {
      props: { kind: 'cpu', values: [10, 71, 95], label: 'CPU · 24h' },
    });
    const classes = segmentClasses(wrapper);
    expect(classes[0]).toContain('bg-success');
    expect(classes[1]).toContain('bg-warning');
    expect(classes[2]).toContain('bg-danger');
  });

  it('derives zones from a configured warning threshold', () => {
    // CPU 阈值 80 → 黄色起点 80 × 70/90 ≈ 62.2，红色起点 80。
    const wrapper = mount(HistoryStrip, {
      props: { kind: 'cpu', values: [60, 70, 80], label: 'CPU · 24h', threshold: 80 },
    });
    const classes = segmentClasses(wrapper);
    expect(classes[0]).toContain('bg-success');
    expect(classes[1]).toContain('bg-warning');
    expect(classes[2]).toContain('bg-danger');
  });

  it('passes the threshold from MetricCell to the strip', () => {
    const wrapper = mount(MetricCell, {
      props: {
        label: 'CPU',
        text: '75%',
        valueClass: 'text-warning',
        ariaLabel: 'CPU · 24h',
        kind: 'cpu',
        history: [70],
        threshold: 80,
      },
    });
    expect(wrapper.findComponent(HistoryStrip).props('threshold')).toBe(80);
  });
});
