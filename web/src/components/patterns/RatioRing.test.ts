// @vitest-environment jsdom
import { enableAutoUnmount, mount } from '@vue/test-utils';
import { afterEach, describe, expect, it } from 'vitest';
import RatioRing from './RatioRing.vue';

enableAutoUnmount(afterEach);

describe('RatioRing', () => {
  it('draws the ratio on a two-tone ring with a readable accessible name', () => {
    const wrapper = mount(RatioRing, {
      props: {
        percent: 68.4,
        valueText: '68.4%',
        label: 'Disk usage: 68.4%',
        tone: 'warning',
        size: 'md',
      },
    });

    const ring = wrapper.get('[data-testid="ratio-ring"]');
    expect(ring.attributes('role')).toBe('img');
    expect(ring.attributes('aria-label')).toBe('Disk usage: 68.4%');
    expect(ring.attributes('data-tone')).toBe('warning');
    expect(ring.attributes('data-empty')).toBeUndefined();
    expect(ring.classes()).toContain('size-20');
    expect(ring.text()).toBe('68.4%');

    const circles = ring.findAll('circle');
    expect(circles).toHaveLength(2);
    // r = 15.9155 让周长 ≈ 100，dash 值可直接读作百分比。
    expect(circles[0]!.attributes('r')).toBe('15.9155');
    expect(circles[1]!.attributes('stroke-dasharray')).toBe('68.4 100');
    expect(circles[1]!.classes()).toContain('stroke-current');
    expect(circles[1]!.classes()).toContain('text-warning');
  });

  it('renders an empty ring instead of a fake 0% sample', () => {
    const wrapper = mount(RatioRing, {
      props: { percent: null, valueText: '—', label: 'Online rate: —' },
    });

    const ring = wrapper.get('[data-testid="ratio-ring"]');
    expect(ring.attributes('data-empty')).toBe('true');
    expect(ring.text()).toBe('—');
    expect(ring.find('circle:last-child').attributes('stroke-dasharray')).toBe('0 100');
    expect(ring.find('circle:last-child').classes()).toContain('text-muted-foreground');
  });

  it('clamps out-of-range percentages', () => {
    const over = mount(RatioRing, { props: { percent: 150, valueText: '100%', label: 'Online rate: 100%' } });
    expect(over.get('circle:last-child').attributes('stroke-dasharray')).toBe('100 100');

    const under = mount(RatioRing, { props: { percent: -5, valueText: '0%', label: 'Online rate: 0%' } });
    expect(under.get('circle:last-child').attributes('stroke-dasharray')).toBe('0 100');
  });

  it('supports compact and hero sizes plus a page-owned test id', () => {
    const small = mount(RatioRing, { props: { percent: 42, valueText: '42%', label: 'CPU: 42%', size: 'sm', testId: 'capacity-cpu' } });
    expect(small.get('[data-testid="capacity-cpu"]').classes()).toContain('size-14');
    expect(small.get('[data-testid="capacity-cpu"]').text()).toBe('42%');

    const large = mount(RatioRing, { props: { percent: 92, valueText: '92%', label: 'Online rate: 92%', size: 'lg' } });
    expect(large.get('[data-testid="ratio-ring"]').classes()).toContain('size-24');
    expect(large.get('[data-testid="ratio-ring"]').find('span').classes()).toContain('text-subtitle');
  });
});
