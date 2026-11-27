// @vitest-environment jsdom
import { enableAutoUnmount, mount } from '@vue/test-utils';
import { afterEach, describe, expect, it } from 'vitest';
import DiskUsageChart from './DiskUsageChart.vue';

enableAutoUnmount(afterEach);

describe('DiskUsageChart', () => {
  it('draws the used ratio on a two-tone ring with a readable accessible name', () => {
    const wrapper = mount(DiskUsageChart, {
      props: {
        percent: 68.4,
        valueText: '68.4%',
        label: 'Disk usage: 68.4%',
        tone: 'warning',
      },
    });

    const chart = wrapper.get('[data-testid="disk-usage-chart"]');
    expect(chart.attributes('role')).toBe('img');
    expect(chart.attributes('aria-label')).toBe('Disk usage: 68.4%');
    expect(chart.attributes('data-tone')).toBe('warning');
    expect(chart.attributes('data-empty')).toBeUndefined();
    expect(chart.text()).toBe('68.4%');

    const circles = chart.findAll('circle');
    expect(circles).toHaveLength(2);
    // r = 15.9155 makes the circumference ≈ 100, so dash values read as percentages.
    expect(circles[0]!.attributes('r')).toBe('15.9155');
    expect(circles[1]!.attributes('stroke-dasharray')).toBe('68.4 100');
    expect(circles[1]!.classes()).toContain('stroke-current');
    expect(circles[1]!.classes()).toContain('text-warning');
  });

  it('renders an empty ring instead of a fake 0% sample', () => {
    const wrapper = mount(DiskUsageChart, {
      props: { percent: null, valueText: '—', label: 'Disk usage: —' },
    });

    const chart = wrapper.get('[data-testid="disk-usage-chart"]');
    expect(chart.attributes('data-empty')).toBe('true');
    expect(chart.text()).toBe('—');
    expect(chart.find('circle:last-child').attributes('stroke-dasharray')).toBe('0 100');
    expect(chart.find('circle:last-child').classes()).toContain('text-muted-foreground');
  });

  it('clamps out-of-range percentages', () => {
    const over = mount(DiskUsageChart, { props: { percent: 150, valueText: '100%', label: 'Disk usage: 100%' } });
    expect(over.get('circle:last-child').attributes('stroke-dasharray')).toBe('100 100');

    const under = mount(DiskUsageChart, { props: { percent: -5, valueText: '0%', label: 'Disk usage: 0%' } });
    expect(under.get('circle:last-child').attributes('stroke-dasharray')).toBe('0 100');
  });
});
