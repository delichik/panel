// @vitest-environment jsdom
import { enableAutoUnmount, mount, type VueWrapper } from '@vue/test-utils';
import { afterEach, describe, expect, it } from 'vitest';
import ProgressBar from './ProgressBar.vue';

enableAutoUnmount(afterEach);

function fillOf(wrapper: VueWrapper) {
  return wrapper.get('[data-progress-bar] > div');
}

describe('ProgressBar', () => {
  it('exposes progressbar semantics and the clamped percentage', () => {
    const wrapper = mount(ProgressBar, { props: { value: 42, label: 'Deployment progress' } });

    const bar = wrapper.get('[data-progress-bar]');
    expect(bar.attributes('role')).toBe('progressbar');
    expect(bar.attributes('aria-label')).toBe('Deployment progress');
    expect(bar.attributes('aria-valuemin')).toBe('0');
    expect(bar.attributes('aria-valuemax')).toBe('100');
    expect(bar.attributes('aria-valuenow')).toBe('42');
    expect(bar.attributes('aria-valuetext')).toBe('42%');

    // 4px 轨道 + 4px 圆角 + 弱填充底，填充是语义 accent。
    expect(bar.classes()).toContain('h-1');
    expect(bar.classes()).toContain('rounded-control');
    expect(bar.classes()).toContain('bg-muted');
    expect(fillOf(wrapper).classes()).toContain('bg-primary');
    expect(fillOf(wrapper).attributes('style')).toContain('width: 42%');
  });

  it('clamps out-of-range and non-finite values', () => {
    const high = mount(ProgressBar, { props: { value: 140, label: 'High' } });
    expect(high.get('[data-progress-bar]').attributes('aria-valuenow')).toBe('100');
    expect(fillOf(high).attributes('style')).toContain('width: 100%');

    const low = mount(ProgressBar, { props: { value: -5, label: 'Low' } });
    expect(low.get('[data-progress-bar]').attributes('aria-valuenow')).toBe('0');
    expect(fillOf(low).attributes('style')).toContain('width: 0%');

    const invalid = mount(ProgressBar, { props: { value: Number.NaN, label: 'NaN' } });
    expect(invalid.get('[data-progress-bar]').attributes('aria-valuenow')).toBe('0');
  });

  it('maps tone to semantic fill colors and accepts a readable value text', () => {
    const danger = mount(ProgressBar, { props: { value: 80, label: 'Disk usage', tone: 'danger', valueLabel: '80% used' } });
    const bar = danger.get('[data-progress-bar]');
    expect(bar.attributes('data-tone')).toBe('danger');
    expect(bar.attributes('aria-valuetext')).toBe('80% used');
    expect(fillOf(danger).classes()).toContain('bg-danger');
    expect(fillOf(danger).classes()).toContain('rounded-control');

    expect(fillOf(mount(ProgressBar, { props: { value: 10, label: 'Ok', tone: 'success' } })).classes()).toContain('bg-success');
    expect(fillOf(mount(ProgressBar, { props: { value: 10, label: 'Warn', tone: 'warning' } })).classes()).toContain('bg-warning');
    expect(fillOf(mount(ProgressBar, { props: { value: 10, label: 'Info', tone: 'info' } })).classes()).toContain('bg-info');
  });
});
