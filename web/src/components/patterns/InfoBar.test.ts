// @vitest-environment jsdom
import { enableAutoUnmount, mount } from '@vue/test-utils';
import { afterEach, describe, expect, it } from 'vitest';
import InfoBar from './InfoBar.vue';

enableAutoUnmount(afterEach);

describe('InfoBar', () => {
  it('renders an info bar as a status region with semantic info surfaces', () => {
    const wrapper = mount(InfoBar, {
      props: { title: 'Maintenance window', tone: 'info' },
      slots: { default: 'Next window starts at 22:00.' },
    });

    const bar = wrapper.get('[data-info-bar]');
    expect(bar.attributes('role')).toBe('status');
    expect(bar.attributes('data-tone')).toBe('info');
    expect(bar.classes()).toContain('rounded-control');
    expect(bar.classes()).toContain('border-info-border');
    expect(bar.classes()).toContain('bg-info-bg');
    expect(bar.classes()).toContain('text-info');
    expect(bar.classes()).not.toContain('shadow-flyout');
    expect(bar.text()).toContain('Maintenance window');
    expect(bar.text()).toContain('Next window starts at 22:00.');
    expect(bar.get('svg').attributes('aria-hidden')).toBe('true');
  });

  it('uses alert for warning/danger and status for success', () => {
    expect(mount(InfoBar, { props: { tone: 'warning' } }).get('[data-info-bar]').attributes('role')).toBe('alert');
    expect(mount(InfoBar, { props: { tone: 'danger' } }).get('[data-info-bar]').attributes('role')).toBe('alert');
    expect(mount(InfoBar, { props: { tone: 'success' } }).get('[data-info-bar]').attributes('role')).toBe('status');
  });

  it.each([
    ['info', 'border-info-border', 'bg-info-bg', 'text-info'],
    ['success', 'border-success-border', 'bg-success-bg', 'text-success'],
    ['warning', 'border-warning-border', 'bg-warning-bg', 'text-warning'],
    ['danger', 'border-danger-border', 'bg-danger-bg', 'text-danger'],
  ] as const)('maps the %s tone to its semantic surface tokens', (tone, borderClass, backgroundClass, textClass) => {
    const bar = mount(InfoBar, { props: { tone } }).get('[data-info-bar]');
    expect(bar.classes()).toContain(borderClass);
    expect(bar.classes()).toContain(backgroundClass);
    expect(bar.classes()).toContain(textClass);
  });

  it('renders an accessible close button only when closable', async () => {
    const plain = mount(InfoBar, { props: { title: 'Read only' } });
    expect(plain.find('button').exists()).toBe(false);

    const wrapper = mount(InfoBar, { props: { title: 'Read only', closable: true, closeLabel: 'Dismiss' } });
    const close = wrapper.get('button');
    expect(close.attributes('aria-label')).toBe('Dismiss');
    expect(close.attributes('title')).toBe('Dismiss');

    await close.trigger('click');
    expect(wrapper.emitted('close')).toHaveLength(1);
  });

  it('supports title and description slots', () => {
    const wrapper = mount(InfoBar, {
      props: { tone: 'success' },
      slots: { title: '<strong class="custom-title">Done</strong>', default: '<span class="custom-body">Saved</span>' },
    });
    expect(wrapper.get('.custom-title').text()).toBe('Done');
    expect(wrapper.get('.custom-body').text()).toBe('Saved');
  });
});
